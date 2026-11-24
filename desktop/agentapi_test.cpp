#include "agentapi.h"
#include "agentops.h"
#include <QBuffer>
#include <QImage>
#include <QJsonArray>
#include <QJsonDocument>
#include <QJsonObject>
#include <QLocalSocket>
#include <QProcess>
#include <QScopeGuard>
#include <QSignalSpy>
#include <QTemporaryDir>
#include <QtTest>

class NoCredentials : public Credentials {
public:
  Entry snapshot(const QString &, QString &) override { return Entry::Missing; }
  bool read(const QString &, QString &) override { return false; }
  bool write(const QString &, const QString &) override { return true; }
  bool remove(const QString &) override { return true; }
};

// A wallet that is slow to answer, as KWallet is while it asks to be unlocked:
// read() runs an event loop, after calling `during`.
class SlowWallet : public Credentials {
public:
  Entry snapshot(const QString &, QString &) override { return Entry::Missing; }
  std::function<void()> during;
  int reads = 0;
  bool read(const QString &, QString &) override {
    ++reads;
    if (during)
      during();
    QEventLoop loop;
    QTimer::singleShot(200, &loop, &QEventLoop::quit);
    loop.exec();
    return false;
  }
  bool write(const QString &, const QString &) override { return true; }
  bool remove(const QString &) override { return true; }
};
// Profiles that never reach a real BMC: the in-process demo console, a demo whose
// login fails, and a non-demo profile on a reserved .invalid name without a password.
static QString writeProfiles(const QTemporaryDir &dir) {
  QString path = dir.filePath("hosts.json");
  QFile f(path);
  if (!f.open(QIODevice::WriteOnly))
    return {};
  f.write(R"({"version":1,"defaultsVersion":1,"profiles":[
    {"id":"demo-1","name":"Demo","host":"demo","family":"demo","username":"","remember":false},
    {"id":"demo-2","name":"Demo Fail","host":"demo-fail","family":"demo","username":"","remember":false},
    {"id":"fake-1","name":"Fake","host":"bmc.invalid","family":"ami","username":"x","remember":false}]})");
  return path;
}

// The first difference between a reply and the recorded one, or nothing. Keys
// listed in vary must be there with a value of the same type.
static QString differs(const QJsonValue &got, const QJsonValue &want, const QJsonArray &vary = {}) {
  if (got.isObject() && want.isObject()) {
    const QJsonObject g = got.toObject(), w = want.toObject();
    if (g.keys() != w.keys())
      return "keys " + g.keys().join(",") + ", recorded " + w.keys().join(",");
    for (const auto &k : w.keys()) {
      if (vary.contains(k) ? g[k].type() != w[k].type() : g[k] != w[k])
        return k + ": " + QJsonDocument(QJsonArray{g[k]}).toJson(QJsonDocument::Compact) +
               ", recorded " + QJsonDocument(QJsonArray{w[k]}).toJson(QJsonDocument::Compact);
    }
    return {};
  }
  return got == want ? QString() : "got " + QJsonDocument(QJsonArray{got}).toJson(QJsonDocument::Compact);
}

class AgentApiTest : public QObject {
  Q_OBJECT
  struct R { bool ok = false; QJsonValue value; QString code; bool called = false; };
  static R run(AgentApi &api, const QString &op, QJsonObject args = {}) {
    R r;
    api.dispatch(op, args, [&r](bool ok, QJsonValue v, QString code) { r = {ok, v, code, true}; });
    return r;
  }
  // For operations that answer later (power, mount, eject).
  static std::shared_ptr<R> runAsync(AgentApi &api, const QString &op, QJsonObject args = {}) {
    auto r = std::make_shared<R>();
    api.dispatch(op, args, [r](bool ok, QJsonValue v, QString code) { *r = {ok, v, code, true}; });
    return r;
  }
  // The activity list as "client: text", newest first.
  static QStringList trail(const AgentApi &api) {
    QStringList out;
    for (const auto &v : api.activity()) {
      const auto e = v.toMap();
      out << e["client"].toString() + ": " + e["text"].toString();
    }
    return out;
  }
  static QJsonObject request(QLocalSocket &s, const QByteArray &line) {
    s.write(line + "\n");
    if (!QTest::qWaitFor([&] { return s.canReadLine(); }, 5000))
      return {};
    return QJsonDocument::fromJson(s.readLine()).object();
  }
  // Points the sessions started until the guard ends at the stand-in core
  // (desktop/fakecore_test.cpp), which plays these steps and logs each command
  // to commands.jsonl in dir.
  static auto useFakeCore(const QTemporaryDir &dir, const QByteArray &steps) {
    QFile f(dir.filePath("script.json"));
    if (!f.open(QIODevice::WriteOnly) || f.write(steps) < 0)
      qFatal("cannot write %s", qPrintable(f.fileName()));
    const QByteArray binDir = qgetenv("OLDIPMI_BIN_DIR");
    qputenv("OLDIPMI_BIN_DIR", FAKECORE_DIR);
    qputenv("OLDIPMI_FAKE_SCRIPT", f.fileName().toLocal8Bit());
    qputenv("OLDIPMI_FAKE_LOG", dir.filePath("commands.jsonl").toLocal8Bit());
    return qScopeGuard([binDir] {
      if (binDir.isNull())
        qunsetenv("OLDIPMI_BIN_DIR");
      else
        qputenv("OLDIPMI_BIN_DIR", binDir);
      qunsetenv("OLDIPMI_FAKE_SCRIPT");
      qunsetenv("OLDIPMI_FAKE_LOG");
    });
  }
  static QByteArray received(const QTemporaryDir &dir) {
    QFile f(dir.filePath("commands.jsonl"));
    return f.open(QIODevice::ReadOnly) ? f.readAll() : QByteArray();
  }
private slots:
  // testdata/agent/exchanges.json holds requests as oldipmi-mcp sends them and
  // the replies AgentApi gave, replayed here against the demo console;
  // internal/mcp checks that its tools read those replies. UPDATE_GOLDEN=1
  // records the replies anew.
  void repliesMatchTheRecordedExchanges() {
    QFile f(QStringLiteral(AGENT_FIXTURES "/exchanges.json"));
    QVERIFY(f.open(QIODevice::ReadOnly));
    QJsonObject fixture = QJsonDocument::fromJson(f.readAll()).object();
    f.close();
    QTemporaryDir dir; NoCredentials w;
    const QString hosts = dir.filePath("hosts.json"), iso = dir.filePath("fixture.iso");
    QFile h(hosts), i(iso);
    QVERIFY(h.open(QIODevice::WriteOnly) && i.open(QIODevice::WriteOnly));
    h.write(QJsonDocument(QJsonObject{{"version", 1}, {"profiles", fixture["profiles"]}}).toJson());
    i.write(QByteArray(8 * 2048, 0));
    h.close();
    i.close();
    Controller c(nullptr, &w, hosts);
    AgentApi api(&c, dir.filePath("s"));
    const bool record = qEnvironmentVariableIsSet("UPDATE_GOLDEN");
    QJsonArray steps = fixture["exchanges"].toArray();
    for (int n = 0; n < steps.size(); ++n) {
      QJsonObject e = steps[n].toObject();
      if (e.contains("wait")) {
        Session *s = nullptr;
        if (e["wait"] == "connected")
          QTRY_VERIFY_WITH_TIMEOUT((s = c.session(e["profile"].toString())) && s->connected() && !s->lastFrame.isNull(), 10000);
        else
          QTRY_VERIFY_WITH_TIMEOUT((s = c.session(e["profile"].toString())) && s->state == Session::State::Disconnected, 10000);
        continue;
      }
      QJsonObject args = e["args"].toObject();
      if (args["path"] == "$ISO")
        args["path"] = iso;
      auto r = runAsync(api, e["op"].toString(), args);
      QTRY_VERIFY_WITH_TIMEOUT(r->called, 10000);
      if (record) {
        e.remove("result");
        e.remove("error");
        e.remove("code");
        e["ok"] = r->ok;
        e[r->ok ? "result" : "error"] = r->value;
        if (!r->ok)
          e["code"] = r->code;
        steps[n] = e;
        continue;
      }
      const QString at = QString("exchange %1, %2: ").arg(n).arg(e["op"].toString());
      QVERIFY2(r->ok == e["ok"].toBool(), qPrintable(at + "ok is " + (r->ok ? "true" : "false")));
      QVERIFY2(r->code == e["code"].toString(), qPrintable(at + "code is " + r->code));
      const QString diff = differs(r->value, e[r->ok ? "result" : "error"], e["vary"].toArray());
      QVERIFY2(diff.isEmpty(), qPrintable(at + diff));
    }
    if (record) {
      fixture["exchanges"] = steps;
      QVERIFY(f.open(QIODevice::WriteOnly | QIODevice::Truncate));
      f.write(QJsonDocument(fixture).toJson());
      qInfo("recorded %s", qPrintable(f.fileName()));
    }
  }
  // The reply to hello names the agent API and every op of names.json; the
  // settings warn while an agent's oldipmi-mcp is of another version (B-01).
  void helloTellsTheContractAndNotesOtherVersions() {
    QCoreApplication::setApplicationVersion("0.5.0");
    auto restore = qScopeGuard([] { QCoreApplication::setApplicationVersion({}); });
    QTemporaryDir dir; NoCredentials w;
    Controller c(nullptr, &w, dir.filePath("hosts.json"));
    AgentApi api(&c, dir.filePath("agent.sock"));
    QVERIFY(api.start());
    QLocalSocket s;
    s.connectToServer(api.socketPath());
    QTRY_COMPARE(s.state(), QLocalSocket::ConnectedState);
    auto o = request(s, "{\"id\":1,\"op\":\"hello\",\"args\":{\"name\":\"a\",\"version\":\"1\",\"mcpVersion\":\"0.5.0\"}}");
    QVERIFY(o["ok"].toBool());
    const QJsonObject r = o["result"].toObject();
    QCOMPARE(r["api"].toInt(), agentops::Api);
    QCOMPARE(r["version"].toString(), QString("0.5.0"));
    QFile names(QStringLiteral(AGENT_FIXTURES "/names.json"));
    QVERIFY(names.open(QIODevice::ReadOnly));
    QStringList want, got = r["ops"].toVariant().toStringList();
    for (const auto &v : QJsonDocument::fromJson(names.readAll()).object()["ops"].toObject())
      want << v.toString();
    want.sort();
    got.sort();
    QCOMPARE(got, want);
    QCOMPARE(api.versionNote(), QString());
    // Another version, or none, as from an oldipmi-mcp before the version check.
    QVERIFY(request(s, "{\"id\":2,\"op\":\"hello\",\"args\":{\"name\":\"a\",\"mcpVersion\":\"0.4.0\"}}")["ok"].toBool());
    QVERIFY2(api.versionNote().startsWith("An agent uses oldipmi-mcp 0.4.0, not the one of OldIPMI 0.5.0:"), qPrintable(api.versionNote()));
    QVERIFY(request(s, "{\"id\":3,\"op\":\"hello\",\"args\":{\"name\":\"a\"}}")["ok"].toBool());
    QVERIFY2(api.versionNote().startsWith("An agent uses an oldipmi-mcp from an older OldIPMI, not the one of OldIPMI 0.5.0:"),
             qPrintable(api.versionNote()));
    // The warning goes with the agent.
    s.disconnectFromServer();
    QTRY_COMPARE(api.versionNote(), QString());
  }
  // The whole chain an agent uses: MCP requests on the stdin of a real
  // oldipmi-mcp, which finds this AgentApi's socket through XDG_RUNTIME_DIR
  // (a directory of its own), and the demo console behind it.
  void mcpServerEndToEnd() {
    QTemporaryDir dir, runtime; MemoryCredentials w;
    const auto path = writeProfiles(dir);
    Controller c(nullptr, &w, path);
    AgentApi api(&c, runtime.filePath("oldipmi/agent.sock"));
    QVERIFY(api.start());
    QProcess mcp;
    auto env = QProcessEnvironment::systemEnvironment();
    env.remove("OLDIPMI_AGENT_SOCKET");
    env.insert("XDG_RUNTIME_DIR", runtime.path());
    mcp.setProcessEnvironment(env);
    mcp.setProcessChannelMode(QProcess::ForwardedErrorChannel);
    mcp.start(QStringLiteral(MCP_BINARY), {});
    QVERIFY(mcp.waitForStarted());
    auto stop = qScopeGuard([&mcp] {
      if (mcp.state() != QProcess::NotRunning) {
        mcp.kill();
        mcp.waitForFinished(1000);
      }
    });
    QByteArray out;
    int next = 0;
    auto rpc = [&](const QString &method, const QJsonObject &params) {
      const int id = ++next;
      QJsonObject req{{"jsonrpc", "2.0"}, {"id", id}, {"method", method}, {"params", params}};
      mcp.write(QJsonDocument(req).toJson(QJsonDocument::Compact) + "\n");
      QJsonObject reply;
      (void)QTest::qWaitFor([&] {
        out += mcp.readAllStandardOutput();
        for (int nl; reply.isEmpty() && (nl = out.indexOf('\n')) >= 0; out.remove(0, nl + 1))
          if (auto o = QJsonDocument::fromJson(out.left(nl)).object(); o["id"].toInt() == id)
            reply = o;
        return !reply.isEmpty();
      }, 30000);
      return reply["result"].toObject();
    };
    auto tool = [&](const QString &name, const QJsonObject &args) {
      auto r = rpc("tools/call", {{"name", name}, {"arguments", args}});
      QString text;
      for (const auto &v : r["content"].toArray())
        text += v.toObject()["text"].toString();
      return std::make_pair(r, text);
    };
    auto init = rpc("initialize", {{"protocolVersion", "2025-06-18"}, {"capabilities", QJsonObject{}},
                                   {"clientInfo", QJsonObject{{"name", "e2e-agent"}, {"version", "1.0"}}}});
    QCOMPARE(init["serverInfo"].toObject()["name"].toString(), QString("oldipmi"));
    QVERIFY(init["instructions"].toString().contains(QStringLiteral(MCP_BINARY)));
    // The hello reaches the app, which then knows the agent by name.
    QTRY_COMPARE(api.clientName(), QString("e2e-agent 1.0"));
    QVERIFY(tool("list_profiles", {}).second.contains("\"name\": \"Demo Fail\""));
    auto [connected, said] = tool("connect", {{"profile", "Demo"}});
    QCOMPARE(said, QString("Connected to Demo (OldIPMI demo (synthetic, no network)). Framebuffer 1024x768. Take a screenshot next."));
    QCOMPARE(c.state(), QString("connected"));
    auto [shot, mapping] = tool("screenshot", {{"max_size", 64}});
    QImage png;
    QVERIFY(png.loadFromData(QByteArray::fromBase64(shot["content"].toArray()[0].toObject()["data"].toString().toLatin1()), "PNG"));
    QCOMPARE(png.size(), QSize(64, 48));
    QVERIFY(mapping.startsWith("Framebuffer 1024x768; image 64x48."));
    auto before = c.shown()->changeSeq;
    QCOMPARE(tool("key", {{"name", "Enter"}}).second, QString("Pressed Enter."));
    QTRY_VERIFY(c.shown()->changeSeq > before); // the demo screen redraws for the key
    QCOMPARE(tool("disconnect", {}).second, QString("Disconnected from Demo."));
    QTRY_COMPARE(c.state(), QString("disconnected"));
    // Profile tools walk through the same stdio, socket and desktop code.
    const auto decode = [](const QString &text) { return QJsonDocument::fromJson(text.toUtf8()).object(); };
    const auto profile = [&](const QString &text) { return decode(text).value("profile").toObject(); };
    QVERIFY(tool("create_profile", {{"host", "demo"}, {"family", "demo"}}).first["isError"].toBool());
    api.setProfileWritesAllowed(true);
    const auto demo = profile(tool("create_profile", {{"name", "MCP Demo"}, {"host", "demo"},
                                                      {"family", "demo"}}).second);
    const auto demoId = demo["id"].toString();
    QVERIFY(!demoId.isEmpty());
    QVERIFY(!tool("connect", {{"profile", demoId}}).first["isError"].toBool());
    QCOMPARE(c.state(), QString("connected"));
    QVERIFY(!tool("disconnect", {{"profile", demoId}}).first["isError"].toBool());
    QTRY_VERIFY(!c.isOpen(demoId));
    QCOMPARE(decode(tool("delete_profile", {{"id", demoId}}).second)["id"].toString(), demoId);

    const QString secret = "fixture-mcp-secret-not-in-output";
    const auto made = tool("create_profile", {{"host", "https://fixture.invalid/login"}, {"family", "ami"},
                                              {"username", "admin"}, {"password", secret}});
    QVERIFY(!made.second.contains(secret));
    const auto created = profile(made.second);
    const auto id = created["id"].toString();
    QVERIFY(!id.isEmpty());
    QCOMPARE(created["host"].toString(), QString("fixture.invalid"));
    QVERIFY(created["passwordSaved"].toBool());
    QString password;
    QVERIFY(w.read(id, password));
    QCOMPARE(password, secret);
    const auto edited = profile(tool("update_profile", {{"id", id}, {"name", "Edited"}}).second);
    QCOMPARE(edited["name"].toString(), QString("Edited"));
    QVERIFY(edited["passwordSaved"].toBool());
    QVERIFY(w.read(id, password));
    QCOMPARE(password, secret);
    {
      Controller reloaded(nullptr, &w, path);
      QCOMPARE(reloaded.host(id).value("name").toString(), QString("Edited"));
      QVERIFY(reloaded.host(id).value("remember").toBool());
    }

    const auto changed = profile(tool("update_profile", {{"id", id}, {"username", "other"}}).second);
    QCOMPARE(changed["username"].toString(), QString("other"));
    QVERIFY(!changed["passwordSaved"].toBool());
    QVERIFY(!w.read(id, password));
    QVERIFY(profile(tool("update_profile", {{"id", id}, {"password", secret}}).second)["passwordSaved"].toBool());
    const auto cleared = profile(tool("update_profile", {{"id", id}, {"clear_password", true}}).second);
    QCOMPARE(cleared["passwordSaved"], QJsonValue(false));
    QVERIFY(!w.read(id, password));
    QVERIFY(profile(tool("update_profile", {{"id", id}, {"password", secret}}).second)["passwordSaved"].toBool());
    QVERIFY(tool("list_profiles", {}).second.contains(id));
    QVERIFY(!tool("list_profiles", {}).second.contains(secret));
    QFile stored(path);
    QVERIFY(stored.open(QIODevice::ReadOnly));
    QVERIFY(!stored.readAll().contains(secret.toUtf8()));
    stored.close();
    QCOMPARE(decode(tool("delete_profile", {{"id", id}}).second)["id"].toString(), id);
    QVERIFY(!w.read(id, password));
    QVERIFY(!tool("list_profiles", {}).second.contains(id));
    QVERIFY(!trail(api).join('\n').contains(secret));

    // At the end of its input the server ends, and the app forgets the agent.
    mcp.closeWriteChannel();
    QVERIFY(mcp.waitForFinished(5000));
    QCOMPARE(mcp.exitStatus(), QProcess::NormalExit);
    QCOMPARE(mcp.exitCode(), 0);
    QTRY_COMPARE(api.clientCount(), 0);
  }
  void profileWritesValidateStateAndPermissions() {
    QTemporaryDir dir;
    MemoryCredentials wallet;
    Controller c(nullptr, &wallet, writeProfiles(dir));
    AgentApi api(&c, dir.filePath("s"));
    QVERIFY(api.start());

    QLocalSocket socket;
    socket.connectToServer(api.socketPath());
    QVERIFY(socket.waitForConnected(2000));
    const auto remove = R"({"id":1,"op":"delete_profile","args":{"id":"demo-1"}})";
    QCOMPARE(request(socket, remove)["code"].toString(), QString("forbidden"));
    api.setProfileWritesAllowed(true);
    QCOMPARE(run(api, "delete_profile", {{"id", "Demo"}}).code, QString("no_profile"));
    QCOMPARE(run(api, "update_profile", {{"id", "demo-1"}, {"legacyTLS", true}}).code, QString("bad_args"));
    QCOMPARE(run(api, "update_profile", {{"id", "demo-1"}, {"password", "discard"}}).code, QString("bad_args"));
    QCOMPARE(run(api, "update_profile", {{"id", "demo-1"}, {"name", QJsonValue::Null}}).code, QString("bad_args"));
    QVERIFY(run(api, "connect", {{"profile", "demo-1"}}).ok);
    QVERIFY(c.isOpen("demo-1"));
    QVERIFY(run(api, "connect", {{"profile", "fake-1"}}).ok);
    QCOMPARE(run(api, "delete_profile", {{"id", "demo-1"}}).code, QString("busy"));
    QCOMPARE(run(api, "update_profile", {{"id", "fake-1"}, {"name", "New"}}).code, QString("busy"));
    QVERIFY(run(api, "disconnect", {{"all", true}}).ok);
    QTRY_VERIFY(!c.isOpen("demo-1"));

    auto advanced = c.host("fake-1");
    advanced["legacyTLS"] = true;
    QVERIFY(!c.saveHost(advanced, {}, false).isEmpty());
    QCOMPARE(run(api, "update_profile", {{"id", "fake-1"}, {"host", "elsewhere.invalid"}}).code,
             QString("bad_args"));
    QVERIFY(run(api, "update_profile", {{"id", "fake-1"}, {"name", "New"}}).ok);
    QVERIFY(c.host("fake-1").value("legacyTLS").toBool());
    api.setProfileWritesAllowed(false);
    QCOMPARE(request(socket, remove)["code"].toString(), QString("forbidden"));
    api.setProfileWritesAllowed(true);
    QVERIFY(request(socket, remove)["ok"].toBool());
    QVERIFY(c.host("demo-1").isEmpty());
  }

  // A client that stops reading is dropped once 64 MiB of replies wait for it.
  void clientThatStopsReadingIsDropped() {
    QTemporaryDir dir; NoCredentials w;
    Controller c(nullptr, &w, dir.filePath("hosts.json"));
    AgentApi api(&c, dir.filePath("s"));
    QVERIFY(api.start());
    QLocalSocket s;
    s.connectToServer(api.socketPath());
    QTRY_COMPARE(api.clientCount(), 1);
    // Each unparsable line gets a 52-byte answer, which this client never reads.
    const QByteArray lines = QByteArray("x\n").repeated(1 << 16);
    for (int i = 0; i < 24 && s.state() == QLocalSocket::ConnectedState; ++i) {
      s.write(lines);
      s.waitForBytesWritten(1000);
      QCoreApplication::processEvents();
    }
    QTRY_COMPARE_WITH_TIMEOUT(api.clientCount(), 0, 30000);
  }
  void profilesNeverExposeSecrets() {
    QTemporaryDir dir; NoCredentials w;
    Controller c(nullptr, &w, writeProfiles(dir));
    AgentApi api(&c, dir.filePath("s"));
    auto r = run(api, "list_profiles");
    QVERIFY(r.ok);
    auto list = r.value.toObject()["profiles"].toArray();
    QCOMPARE(list.size(), 3);
    for (auto v : list)
      for (auto k : v.toObject().keys())
        QVERIFY2(!k.contains("pass", Qt::CaseInsensitive) || k == "passwordSaved", qPrintable(k));
  }
  void screenshotAndStatus() {
    QTemporaryDir dir; NoCredentials w;
    Controller c(nullptr, &w, dir.filePath("hosts.json"));
    AgentApi api(&c, dir.filePath("s"));
    QVERIFY(!run(api, "screenshot").ok);
    auto st = run(api, "status").value.toObject();
    QCOMPARE(st["state"].toString(), QString("disconnected"));
    QCOMPARE(st["passwordRequired"].toBool(), false);
    c.preview();
    auto r = run(api, "screenshot");
    QVERIFY(r.ok);
    auto o = r.value.toObject();
    QCOMPARE(o["fullWidth"].toInt(), 800);
    QImage png; QVERIFY(png.loadFromData(QByteArray::fromBase64(o["png"].toString().toLatin1()), "PNG"));
    QCOMPARE(png.size(), QSize(800, 600));
    r = run(api, "screenshot", {{"x", 10}, {"y", 20}, {"width", 100}, {"height", 50}, {"scale", 0.5}});
    QVERIFY(r.ok);
    QCOMPARE(r.value.toObject()["width"].toInt(), 50);
    QCOMPARE(r.value.toObject()["height"].toInt(), 25);
    r = run(api, "screenshot", {{"maxSize", 400}}); // longest side capped, aspect kept
    QCOMPARE(r.value.toObject()["width"].toInt(), 400);
    QCOMPARE(r.value.toObject()["height"].toInt(), 300);
    QVERIFY(!run(api, "screenshot", {{"x", 790}, {"width", 100}}).ok);
    QVERIFY(!run(api, "screenshot", {{"scale", 2.0}}).ok);
    QVERIFY(run(api, "status").value.toObject()["changeSeq"].toDouble() >= 1);
  }
  void inputAndPowerNeedConnection() {
    QTemporaryDir dir; NoCredentials w;
    Controller c(nullptr, &w, dir.filePath("hosts.json"));
    AgentApi api(&c, dir.filePath("s"));
    for (auto op : {"key", "pointer", "power", "mount", "eject"}) {
      auto r = run(api, op, {{"usage", 4}, {"action", "on"}});
      QVERIFY2(r.called && !r.ok, op);
      QVERIFY(r.value.toString().contains("Not connected"));
      QCOMPARE(r.code, QString(agentops::ErrNotConnected));
    }
    QVERIFY(!run(api, "nope").ok);
    QCOMPARE(run(api, "nope").code, QString(agentops::ErrUnknownOp));
  }
  void connectErrorsAndPasswordPrompt() {
    QTemporaryDir dir; NoCredentials w;
    Controller c(nullptr, &w, writeProfiles(dir));
    AgentApi api(&c, dir.filePath("s"));
    auto r = run(api, "connect", {{"profile", "does-not-exist"}});
    QVERIFY(!r.ok);
    QVERIFY(r.value.toString().contains("Available: Demo, Demo Fail, Fake"));
    QCOMPARE(r.code, QString(agentops::ErrNoProfile));
    QSignalSpy prompts(&c, &Controller::passwordRequired);
    r = run(api, "connect", {{"profile", "fake"}}); // case-insensitive, no saved password
    QVERIFY(r.ok);
    QVERIFY(r.value.toObject()["passwordRequired"].toBool());
    QCOMPARE(c.state(), QString("disconnected"));
    auto st = run(api, "status").value.toObject();
    QVERIFY(st["passwordRequired"].toBool());
    QCOMPARE(st["passwordProfile"].toString(), QString("Fake"));
    r = run(api, "connect", {{"profile", "bmc.invalid"}}); // by host; asks again (prompt may have been dismissed)
    QVERIFY(r.ok && r.value.toObject()["passwordRequired"].toBool());
    QCOMPARE(prompts.count(), 2);
    // Another console waits until the user has answered the prompt.
    r = run(api, "connect", {{"profile", "Demo"}});
    QVERIFY(!r.ok);
    QCOMPARE(r.code, QString(agentops::ErrBusy));
    QVERIFY(run(api, "disconnect").ok);
    QVERIFY(!run(api, "status").value.toObject()["passwordRequired"].toBool());
  }
  // A name or host that several profiles share names none of them: whatever
  // the operation, it fails as ambiguous and lists them with their ids, and no
  // password prompt opens (B-07). An id, or the host of one profile, still
  // names it.
  void anAmbiguousProfileNamesNone() {
    QTemporaryDir dir; NoCredentials w;
    QFile f(dir.filePath("hosts.json"));
    QVERIFY(f.open(QIODevice::WriteOnly));
    f.write(R"({"version":1,"defaultsVersion":1,"profiles":[
      {"id":"twin-a","name":"Twin","host":"a.invalid","family":"ami","username":"x","remember":false},
      {"id":"twin-b","name":"twin","host":"b.invalid","family":"ami","username":"x","remember":false},
      {"id":"copy","name":"Copy","host":"A.invalid","family":"ami","username":"x","remember":false}]})");
    f.close();
    Controller c(nullptr, &w, f.fileName());
    AgentApi api(&c, dir.filePath("s"));
    QSignalSpy prompts(&c, &Controller::passwordRequired);
    const QList<std::pair<QString, QString>> cases{
        {"TWIN", "'Twin' (a.invalid, id twin-a), 'twin' (b.invalid, id twin-b)"},
        {"a.invalid", "'Twin' (a.invalid, id twin-a), 'Copy' (A.invalid, id copy)"}};
    for (const auto &[key, which] : cases) {
      for (const char *op : {"connect", "status", "screenshot", "power", "disconnect"}) {
        auto r = run(api, op, {{"profile", key}, {"action", "off"}});
        QVERIFY2(!r.ok, op);
        QCOMPARE(r.code, QString(agentops::ErrAmbiguous));
        QCOMPARE(r.value.toString(), "Several profiles match '" + key + "': " + which + ". Use the id of the one you mean.");
      }
    }
    QCOMPARE(prompts.count(), 0);
    QVERIFY(c.pendingProfile().isEmpty());
    QVERIFY(run(api, "connect", {{"profile", "twin-a"}}).value["passwordRequired"].toBool());
    QCOMPARE(c.pendingProfile().value("id").toString(), QString("twin-a"));
    auto st = run(api, "status", {{"profile", "b.invalid"}});
    QVERIFY(st.ok);
    QVERIFY(!st.value["passwordRequired"].toBool()); // the prompt is twin-a's
  }
  // The prompt an agent's connect opened belongs to its profile, by id:
  // another console, or another profile of the same name, neither reports nor
  // dismisses it (B-07).
  void thePasswordPromptBelongsToItsProfile() {
    QTemporaryDir dir; NoCredentials w;
    const QString hosts = dir.filePath("hosts.json");
    QFile f(hosts);
    QVERIFY(f.open(QIODevice::WriteOnly));
    f.write(R"({"version":1,"defaultsVersion":1,"profiles":[
      {"id":"demo-1","name":"Demo","host":"demo","family":"demo","username":"","remember":false},
      {"id":"twin-a","name":"Twin","host":"a.invalid","family":"ami","username":"x","remember":false},
      {"id":"twin-b","name":"Twin","host":"b.invalid","family":"ami","username":"x","remember":false}]})");
    f.close();
    Controller c(nullptr, &w, hosts);
    AgentApi api(&c, dir.filePath("s"));
    QVERIFY(run(api, "connect", {{"profile", "Demo"}}).ok);
    QTRY_VERIFY_WITH_TIMEOUT(c.session("demo-1") && c.session("demo-1")->connected(), 10000);
    QVERIFY(run(api, "connect", {{"profile", "twin-a"}}).value["passwordRequired"].toBool());
    auto asks = [&](const QJsonObject &args) { return run(api, "status", args).value["passwordRequired"].toBool(); };
    QVERIFY(asks({{"profile", "twin-a"}}));
    QVERIFY(!asks({{"profile", "twin-b"}}));
    auto st = run(api, "status").value.toObject(); // about the only open console, Demo
    QVERIFY(!st["passwordRequired"].toBool());
    QCOMPARE(st["passwordProfile"].toString(), QString("Twin"));
    // Neither the other profile of the same name nor the open console takes the prompt along.
    QVERIFY(!run(api, "disconnect", {{"profile", "twin-b"}}).value["promptDismissed"].toBool());
    QVERIFY(!run(api, "disconnect").value["promptDismissed"].toBool()); // closes Demo
    QCOMPARE(c.pendingProfile().value("id").toString(), QString("twin-a"));
    QVERIFY(asks({{"profile", "twin-a"}}));
    QVERIFY(asks({})); // no console is open: any prompt counts
    // With no console open, disconnect dismisses the prompt, in the window too.
    QVERIFY(run(api, "disconnect").value["promptDismissed"].toBool());
    QVERIFY(c.pendingProfile().isEmpty());
    QVERIFY(!asks({}));
    // A prompt the user opens in the window for another profile is not the agent's.
    QVERIFY(run(api, "connect", {{"profile", "twin-a"}}).value["passwordRequired"].toBool());
    QVariantMap twinB;
    for (const auto &v : c.hosts())
      if (v.toMap().value("id") == "twin-b")
        twinB = v.toMap();
    c.connectHost(twinB, "");
    QCOMPARE(c.pendingProfile().value("id").toString(), QString("twin-b"));
    st = run(api, "status").value.toObject();
    QVERIFY(!st["passwordRequired"].toBool());
    QVERIFY(!st.contains("passwordProfile"));
  }
  void twoConsolesAtOnce() {
    QTemporaryDir dir; NoCredentials w;
    QFile pf(dir.filePath("hosts.json"));
    QVERIFY(pf.open(QIODevice::WriteOnly));
    pf.write(R"({"version":1,"defaultsVersion":1,"profiles":[
      {"id":"demo-1","name":"Demo","host":"demo","family":"demo","username":"","remember":false},
      {"id":"demo-3","name":"Wide","host":"demo-1920x1080","family":"demo","username":"","remember":false}]})");
    pf.close();
    Controller c(nullptr, &w, pf.fileName());
    AgentApi api(&c, dir.filePath("s"));
    QVERIFY(run(api, "connect", {{"profile", "Demo"}}).ok);
    QVERIFY(run(api, "connect", {{"profile", "Wide"}}).ok);
    QCOMPARE(c.openCount(), 2);
    QTRY_VERIFY_WITH_TIMEOUT(c.session("demo-1")->connected() && c.session("demo-3")->connected(), 10000);
    QTRY_VERIFY(!c.session("demo-1")->lastFrame.isNull() && !c.session("demo-3")->lastFrame.isNull());
    QCOMPARE(c.session("demo-1")->lastFrame.size(), QSize(1024, 768));
    QCOMPARE(c.session("demo-3")->lastFrame.size(), QSize(1920, 1080));
    // The newest console is in front; the other one keeps running behind it.
    QCOMPARE(c.shownId(), QString("demo-3"));
    // With several open, an operation must name its target.
    auto r = run(api, "screenshot");
    QVERIFY(!r.ok);
    QVERIFY(r.value.toString().contains("Several consoles"));
    QCOMPARE(r.code, QString(agentops::ErrAmbiguous));
    QVERIFY(!run(api, "status").ok);
    QVERIFY(!run(api, "key", {{"usage", 4}, {"down", true}}).ok);
    // Each profile answers for itself.
    QCOMPARE(run(api, "screenshot", {{"profile", "Demo"}}).value.toObject()["fullWidth"].toInt(), 1024);
    QCOMPARE(run(api, "screenshot", {{"profile", "wide"}}).value.toObject()["fullWidth"].toInt(), 1920);
    auto st = run(api, "status", {{"profile", "Demo"}}).value.toObject();
    QCOMPARE(st["profile"].toObject()["name"].toString(), QString("Demo"));
    QCOMPARE(st["sessions"].toArray().size(), 2);
    auto before = c.session("demo-1")->changeSeq;
    QVERIFY(run(api, "key", {{"profile", "Demo"}, {"usage", 4}, {"down", true}}).ok);
    QVERIFY(run(api, "key", {{"profile", "Demo"}, {"usage", 4}, {"down", false}}).ok);
    QTRY_VERIFY(c.session("demo-1")->changeSeq > before);
    // Closing one leaves the other open and makes the default unambiguous again.
    r = run(api, "disconnect", {{"profile", "Demo"}});
    QVERIFY(r.ok);
    QCOMPARE(r.value.toObject()["stillOpen"].toArray().size(), 1);
    QVERIFY(!c.session("demo-1"));
    QVERIFY(c.session("demo-3")->connected());
    QVERIFY(run(api, "screenshot").ok);
    QVERIFY(run(api, "connect", {{"profile", "Demo"}}).ok);
    QCOMPARE(c.openCount(), 2);
    r = run(api, "disconnect", {{"all", true}});
    QVERIFY(r.ok);
    QCOMPARE(r.value.toObject()["closed"].toArray().size(), 2);
    QCOMPARE(c.openCount(), 0);
    QCOMPARE(run(api, "status").value.toObject()["state"].toString(), QString("disconnected"));
  }
  void demoConsoleEndToEnd() {
    QTemporaryDir dir; NoCredentials w;
    Controller c(nullptr, &w, writeProfiles(dir));
    AgentApi api(&c, dir.filePath("s"));
    auto r = run(api, "connect", {{"profile", "Demo"}});
    QVERIFY2(r.ok, qPrintable(r.value.toString()));
    QTRY_COMPARE_WITH_TIMEOUT(c.state(), QString("connected"), 10000);
    QTRY_VERIFY(!c.shown()->lastFrame.isNull());
    QCOMPARE(c.shown()->lastFrame.size(), QSize(1024, 768));
    auto st = run(api, "status").value.toObject();
    QCOMPARE(st["profile"].toObject()["name"].toString(), QString("Demo"));
    QVERIFY(st["consoleInfo"].toObject()["protocol"].toString().contains("demo"));
    // Same profile again: no error, nothing restarted.
    r = run(api, "connect", {{"profile", "demo-1"}});
    QVERIFY(r.ok && r.value.toObject()["already"].toBool());
    // Keys and pointer reach the demo session, which redraws.
    auto before = c.shown()->changeSeq;
    QVERIFY(run(api, "key", {{"usage", 4}, {"down", true}}).ok);
    QVERIFY(run(api, "key", {{"usage", 4}, {"down", false}}).ok);
    QTRY_VERIFY(c.shown()->changeSeq > before);
    QVERIFY(!run(api, "key", {{"usage", 300}, {"down", true}}).ok);
    QVERIFY(!run(api, "pointer", {{"x", 1024}, {"y", 0}}).ok);
    before = c.shown()->changeSeq;
    QVERIFY(run(api, "pointer", {{"x", 300}, {"y", 200}, {"buttons", 1}}).ok);
    QVERIFY(run(api, "pointer", {{"x", 300}, {"y", 200}, {"buttons", 0}}).ok);
    QTRY_VERIFY(c.shown()->changeSeq > before);
    auto shot = run(api, "screenshot").value.toObject();
    QCOMPARE(shot["width"].toInt(), 1024);
    // Power and media are accepted without effect.
    auto pr = runAsync(api, "power", {{"action", "reset"}});
    QTRY_VERIFY(pr->called);
    QVERIFY(pr->ok);
    QTemporaryFile iso(dir.filePath("XXXXXX.iso"));
    QVERIFY(iso.open());
    pr = runAsync(api, "mount", {{"path", iso.fileName()}});
    QTRY_VERIFY(pr->called);
    QVERIFY2(pr->ok, qPrintable(pr->value.toString()));
    QTRY_VERIFY(c.mounted());
    pr = runAsync(api, "eject");
    QTRY_VERIFY(pr->called);
    QVERIFY(pr->ok);
    auto d = run(api, "disconnect");
    QVERIFY(d.ok);
    QCOMPARE(d.value["previous"].toString(), QString("connected"));
    QVERIFY(!d.value["profile"].toObject()["name"].toString().isEmpty());
    QCOMPARE(c.state(), QString("disconnected"));
    QVERIFY(run(api, "status").value["consoleInfo"].toObject().isEmpty()); // no stale session info
    d = run(api, "disconnect");
    QCOMPARE(d.value["previous"].toString(), QString("disconnected"));
    QCOMPARE(c.message(), QString("Disconnected.")); // a second disconnect changes nothing
    // A failing login reports its stage.
    QVERIFY(run(api, "connect", {{"profile", "Demo Fail"}}).ok);
    QTRY_COMPARE_WITH_TIMEOUT(c.state(), QString("disconnected"), 10000);
    QVERIFY2(c.message().contains("rejected"), qPrintable(c.message()));
    // Activity never names keys.
    for (auto v : api.activity())
      QVERIFY(!v.toMap()["text"].toString().contains("usage"));
  }
  void socketIsOwnerOnlyAndAnswers() {
    QTemporaryDir dir; NoCredentials w;
    Controller c(nullptr, &w, dir.filePath("hosts.json"));
    QString path = dir.filePath("run/agent.sock");
    AgentApi api(&c, path);
    QVERIFY(!api.enabled());
    QVERIFY(api.start());
    QVERIFY(api.listening() && api.enabled());
    QVERIFY(!(QFile::permissions(path) & (QFile::ReadGroup | QFile::WriteGroup | QFile::ReadOther | QFile::WriteOther)));
    QVERIFY(!(QFile::permissions(QFileInfo(path).absolutePath()) & (QFile::ReadGroup | QFile::ReadOther | QFile::ExeGroup | QFile::ExeOther)));
    QLocalSocket s;
    s.connectToServer(path);
    QTRY_COMPARE(s.state(), QLocalSocket::ConnectedState); // server shares this event loop
    auto o = request(s, "{\"id\":7,\"op\":\"status\",\"args\":{}}");
    QCOMPARE(o["id"].toInt(), 7);
    QVERIFY(o["ok"].toBool());
    QCOMPARE(o["result"].toObject()["state"].toString(), QString("disconnected"));
    // A second instance must not steal a live socket, and says why.
    AgentApi second(&c, path);
    QVERIFY(!second.start());
    QVERIFY(second.errorString().contains("Another OldIPMI instance"));
    // Once the first instance lets go, the second takes the socket over.
    api.stop();
    QTRY_VERIFY_WITH_TIMEOUT(second.listening(), 8000);
    QVERIFY(second.errorString().isEmpty());
  }
  void socketRobustness() {
    QTemporaryDir dir; NoCredentials w;
    Controller c(nullptr, &w, dir.filePath("hosts.json"));
    QString path = dir.filePath("agent.sock");
    AgentApi api(&c, path);
    QSignalSpy clients(&api, &AgentApi::clientsChanged);
    QVERIFY(api.start());
    QLocalSocket a, b;
    a.connectToServer(path);
    b.connectToServer(path);
    QTRY_COMPARE(api.clientCount(), 2);
    QVERIFY(clients.count() >= 2);
    QCOMPARE(api.clientName(), QString("unidentified client"));
    // hello names the client for the UI.
    auto o = request(a, R"({"id":1,"op":"hello","args":{"name":"claude-code","version":"2.1.0","protocol":"2025-11-25"}})");
    QVERIFY(o["ok"].toBool());
    QVERIFY(api.clientName().contains("claude-code 2.1.0"));
    QCOMPARE(api.clientCount(), 2);
    // Partial lines are buffered until the newline arrives.
    a.write(R"({"id":2,"op":"sta)");
    a.flush();
    QTest::qWait(50);
    QVERIFY(!a.canReadLine());
    o = request(a, R"(tus","args":{}})");
    QCOMPARE(o["id"].toInt(), 2);
    // Two requests in one write are answered in order; clients are independent.
    a.write("{\"id\":3,\"op\":\"status\"}\n{\"id\":4,\"op\":\"nope\"}\n");
    QTRY_VERIFY(a.bytesAvailable() > 0 && QByteArray(a.peek(1 << 16)).count('\n') >= 2);
    QCOMPARE(QJsonDocument::fromJson(a.readLine()).object()["id"].toInt(), 3);
    o = QJsonDocument::fromJson(a.readLine()).object();
    QCOMPARE(o["id"].toInt(), 4);
    QVERIFY(!o["ok"].toBool());
    QCOMPARE(o["code"].toString(), QString(agentops::ErrUnknownOp));
    QCOMPARE(request(b, R"({"id":9,"op":"status"})")["id"].toInt(), 9);
    // Invalid JSON is answered, not ignored.
    o = request(b, "{oops");
    QVERIFY(!o["ok"].toBool());
    QVERIFY(o["error"].toString().contains("Invalid JSON"));
    QCOMPARE(o["code"].toString(), QString(agentops::ErrBadRequest));
    // An oversized line closes only that client.
    b.write(QByteArray((1 << 20) + 1024, 'x'));
    QTRY_COMPARE_WITH_TIMEOUT(b.state(), QLocalSocket::UnconnectedState, 5000);
    QTRY_COMPARE(api.clientCount(), 1);
    QCOMPARE(request(a, R"({"id":5,"op":"status"})")["id"].toInt(), 5);
    // stop() closes clients and removes the socket; start() serves again.
    QSignalSpy listening(&api, &AgentApi::listeningChanged);
    api.setProperty("enabled", false);
    QVERIFY(!api.listening() && !api.enabled());
    QVERIFY(!QFile::exists(path));
    QTRY_COMPARE(a.state(), QLocalSocket::UnconnectedState);
    QCOMPARE(api.clientCount(), 0);
    QCOMPARE(api.clientName(), QString());
    QVERIFY(listening.count() >= 1);
    QVERIFY(api.start());
    QLocalSocket again;
    again.connectToServer(path);
    QTRY_COMPARE(again.state(), QLocalSocket::ConnectedState);
    QCOMPARE(request(again, R"({"id":6,"op":"status"})")["id"].toInt(), 6);
  }
  void heldInputIsReleasedWhenTheClientLeaves() {
    QTemporaryDir dir; NoCredentials w;
    Controller c(nullptr, &w, writeProfiles(dir));
    QString path = dir.filePath("agent.sock");
    AgentApi api(&c, path);
    QVERIFY(api.start());
    auto *s = new QLocalSocket;
    s->connectToServer(path);
    QTRY_COMPARE(s->state(), QLocalSocket::ConnectedState);
    QVERIFY(request(*s, R"({"id":1,"op":"connect","args":{"profile":"Demo"}})")["ok"].toBool());
    QTRY_COMPARE_WITH_TIMEOUT(c.state(), QString("connected"), 10000);
    QTRY_VERIFY(!c.shown()->lastFrame.isNull());
    QVERIFY(request(*s, R"({"id":2,"op":"key","args":{"usage":225,"down":true}})")["ok"].toBool());
    QVERIFY(request(*s, R"({"id":3,"op":"pointer","args":{"x":10,"y":10,"buttons":1}})")["ok"].toBool());
    QTest::qWait(300);
    auto before = c.shown()->changeSeq;
    delete s; // the agent process goes away while holding Shift and the left button
    QTRY_COMPARE(api.clientCount(), 0);
    QTRY_VERIFY(c.shown()->changeSeq > before); // the demo screen redraws for the key-up/button-up
    QCOMPARE(trail(api).mid(0, 2), (QStringList{"unidentified client: Agent disconnected",
                                                "unidentified client: Released 2 held key(s)/button(s) of this agent"}));
    c.disconnectHost();
  }
  void activityIsBoundedNewestFirstAndCoalesced() {
    QTemporaryDir dir; NoCredentials w;
    Controller c(nullptr, &w, dir.filePath("hosts.json"));
    AgentApi api(&c, dir.filePath("agent.sock"));
    QSignalSpy changed(&api, &AgentApi::activityChanged);
    for (int i = 0; i < 5; ++i)
      run(api, "list_profiles");
    QCOMPARE(api.activity().size(), 1); // repeated calls coalesce
    for (int i = 0; i < 15; ++i) {
      api.start();
      api.stop();
    }
    QCOMPARE(api.activity().size(), AgentApi::MaxActivity);
    auto first = api.activity().first().toMap();
    QCOMPARE(first["text"].toString(), QString("Agent control stopped"));
    QVERIFY(first["time"].toDateTime().isValid());
    QVERIFY(changed.count() > 20);
    api.clearActivity();
    QVERIFY(api.activity().isEmpty());
  }
  // What the operations leave for the user: the entries of the activity list
  // in Settings → AI agents, and the notes in the status bar. A request that
  // fails, and one that changes nothing, leaves none.
  void everyOperationLeavesItsTrail() {
    QTemporaryDir dir; NoCredentials w;
    QStringList notes; // outlives the controller, whose changed() fills it
    Controller c(nullptr, &w, writeProfiles(dir));
    connect(&c, &Controller::changed, &c, [&] {
      if (c.message().startsWith("Agent: ") && (notes.isEmpty() || notes.last() != c.message()))
        notes << c.message();
    });
    QFile iso(dir.filePath("fixture.iso"));
    QVERIFY(iso.open(QIODevice::WriteOnly) && iso.write(QByteArray(8 * 2048, 0)) > 0);
    iso.close();
    const QString path = dir.filePath("agent.sock");
    AgentApi api(&c, path);
    QVERIFY(api.start());
    api.clearActivity(); // "Agent control started"
    auto *s = new QLocalSocket;
    s->connectToServer(path);
    QTRY_COMPARE(s->state(), QLocalSocket::ConnectedState);
    auto ok = [&](const QByteArray &line) { return request(*s, line)["ok"].toBool(); };
    QVERIFY(ok(R"({"id":1,"op":"hello","args":{"name":"fixture-agent","version":"1.0"}})"));
    QVERIFY(ok(R"({"id":2,"op":"list_profiles","args":{}})"));
    QVERIFY(ok(R"({"id":3,"op":"connect","args":{"profile":"Demo"}})"));
    QTRY_VERIFY_WITH_TIMEOUT(c.session("demo-1") && c.session("demo-1")->connected(), 10000);
    QTRY_VERIFY(!c.session("demo-1")->lastFrame.isNull());
    QVERIFY(ok(R"({"id":4,"op":"connect","args":{"profile":"Demo"}})")); // already open
    QVERIFY(ok(R"({"id":5,"op":"status","args":{}})"));
    QVERIFY(ok(R"({"id":6,"op":"screenshot","args":{}})"));
    QVERIFY(ok(R"({"id":7,"op":"key","args":{"usage":4,"down":true}})"));
    QVERIFY(ok(R"({"id":8,"op":"key","args":{"usage":4,"down":false}})"));
    QVERIFY(!ok(R"({"id":9,"op":"key","args":{"usage":300,"down":true}})"));
    QVERIFY(ok(R"({"id":10,"op":"pointer","args":{"x":10,"y":10,"buttons":0}})"));
    QVERIFY(!ok(R"({"id":11,"op":"nope","args":{}})"));
    QVERIFY(ok(R"({"id":12,"op":"release","args":{}})"));
    QVERIFY(ok(R"({"id":13,"op":"power","args":{"action":"reset"}})"));
    QJsonObject mount{{"id", 14}, {"op", "mount"}, {"args", QJsonObject{{"path", iso.fileName()}}}};
    QVERIFY(ok(QJsonDocument(mount).toJson(QJsonDocument::Compact)));
    QVERIFY(ok(R"({"id":15,"op":"eject","args":{}})"));
    QVERIFY(ok(R"({"id":16,"op":"disconnect","args":{}})"));
    QCOMPARE(trail(api), (QStringList{
                             "fixture-agent 1.0: Disconnect",
                             "fixture-agent 1.0: Eject ISO",
                             "fixture-agent 1.0: Mount ISO fixture.iso",
                             "fixture-agent 1.0: Power reset accepted",
                             "fixture-agent 1.0: Power reset requested",
                             "fixture-agent 1.0: Released all keys and buttons",
                             "fixture-agent 1.0: Pointer input ×1, last at 10,10",
                             "fixture-agent 1.0: Keyboard input: 1 key presses",
                             "fixture-agent 1.0: Screenshot ×1",
                             "fixture-agent 1.0: Open console 'Demo'",
                             "fixture-agent 1.0: Listed profiles ×1",
                             "fixture-agent 1.0: Agent identified itself",
                             "unidentified client: Agent connected",
                         }));
    api.clearActivity();
    QVERIFY(ok(R"({"id":17,"op":"connect","args":{"profile":"Demo"}})"));
    QTRY_VERIFY_WITH_TIMEOUT(c.session("demo-1") && c.session("demo-1")->connected(), 10000);
    QVERIFY(ok(R"({"id":18,"op":"disconnect","args":{"all":true}})"));
    QVERIFY(request(*s, R"({"id":19,"op":"connect","args":{"profile":"Fake"}})")["result"].toObject()["passwordRequired"].toBool());
    QVERIFY(ok(R"({"id":20,"op":"disconnect","args":{}})")); // dismisses the prompt
    delete s;
    QTRY_COMPARE(api.clientCount(), 0);
    QCOMPARE(trail(api), (QStringList{
                             "fixture-agent 1.0: Agent disconnected",
                             "fixture-agent 1.0: Disconnect",
                             "fixture-agent 1.0: Waiting for the user to enter the password for 'Fake'",
                             "fixture-agent 1.0: Open console 'Fake'",
                             "fixture-agent 1.0: Disconnect all consoles",
                             "fixture-agent 1.0: Open console 'Demo'",
                         }));
    QCOMPARE(notes, (QStringList{"Agent: opening Demo", "Agent: power reset", "Agent: power reset accepted",
                                 "Agent: mount fixture.iso", "Agent: eject ISO", "Agent: disconnect",
                                 "Agent: opening Demo", "Agent: disconnect all", "Agent: opening Fake",
                                 "Agent: disconnect"}));
  }
  // A command the console refuses fails with the console's message, as failed.
  void aRefusedCommandFails() {
    QTemporaryDir dir;
    auto fake = useFakeCore(dir, R"([{"expect": "connect"},
      {"send": {"version": 1, "type": "state", "state": "connected"}}, {"expect": "power"},
      {"reply": {"version": 1, "type": "error", "message": "power control requires account credentials"}}])");
    NoCredentials w;
    Controller c(nullptr, &w, writeProfiles(dir));
    AgentApi api(&c, dir.filePath("s"));
    QVERIFY(run(api, "connect", {{"profile", "Demo"}}).ok);
    QTRY_VERIFY(c.session("demo-1") && c.session("demo-1")->connected());
    auto power = runAsync(api, "power", {{"action", "soft"}});
    QTRY_VERIFY(power->called);
    QVERIFY(!power->ok);
    QCOMPARE(power->value.toString(), QString("power control requires account credentials"));
    QCOMPARE(power->code, QString(agentops::ErrFailed));
    QCOMPARE(trail(api).mid(0, 2), (QStringList{"OldIPMI: Power soft failed", "OldIPMI: Power soft requested"}));
    QCOMPARE(c.message(), QString("Agent: power soft failed: power control requires account credentials"));
  }
  // Quitting while a power command waits for its answer: the agent API goes
  // first, then the controller, whose sessions settle their callbacks. None may
  // reach the agent API any more (C-07, B-10; the ASan build sees the use after
  // free).
  void quitWithAPowerCommandPending() {
    QTemporaryDir dir;
    auto fake = useFakeCore(dir, R"([{"expect": "connect"},
      {"send": {"version": 1, "type": "state", "state": "connected"}}, {"expect": "power"}])");
    NoCredentials w;
    auto *c = new Controller(nullptr, &w, writeProfiles(dir));
    auto *api = new AgentApi(c, dir.filePath("s"));
    QVERIFY(run(*api, "connect", {{"profile", "Demo"}}).ok);
    QTRY_VERIFY(c->session("demo-1") && c->session("demo-1")->connected());
    auto power = runAsync(*api, "power", {{"action", "soft"}});
    QTRY_VERIFY(received(dir).contains(R"("op":"power")"));
    delete api;
    delete c;
    QVERIFY(!power->called);
  }
  // A client that leaves while its request waits in a nested event loop, here
  // the wallet's: the request ends without touching the client, which is gone
  // (B-06; the ASan build sees the use after free).
  void clientLeavesWhileTheWalletIsOpen() {
    QTemporaryDir dir;
    const QString hosts = dir.filePath("hosts.json");
    QFile h(hosts);
    QVERIFY(h.open(QIODevice::WriteOnly));
    h.write(R"({"version":1,"defaultsVersion":1,"profiles":[
      {"id":"kept-1","name":"Kept","host":"bmc.invalid","family":"ami","username":"x","remember":true}]})");
    h.close();
    SlowWallet wallet;
    Controller c(nullptr, &wallet, hosts);
    AgentApi api(&c, dir.filePath("s"));
    QVERIFY(api.start());
    QLocalSocket client;
    client.connectToServer(api.socketPath());
    QVERIFY(client.waitForConnected(1000));
    QTRY_COMPARE(api.clientCount(), 1);
    wallet.during = [&client] { client.abort(); };
    client.write(R"({"id":1,"op":"connect","args":{"profile":"Kept"}})" "\n");
    client.flush();
    QTRY_COMPARE(wallet.reads, 1);
    QTRY_COMPARE(api.clientCount(), 0);
    QVERIFY(c.pendingProfile().value("id") == "kept-1"); // the prompt is open, for the user
    c.cancelCredentials();
  }
  // The agent socket's default directory must be this user's own; one that is
  // not, here a link, is refused, as one another user made first would be (B-12).
  void socketDirectoryMustBeOurs() {
    QTemporaryDir xdg, dir;
    QVERIFY(QDir().mkdir(dir.filePath("elsewhere")));
    QVERIFY(QFile::link(dir.filePath("elsewhere"), xdg.filePath("oldipmi")));
    const QByteArray oldXdg = qgetenv("XDG_RUNTIME_DIR"), oldSocket = qgetenv("OLDIPMI_AGENT_SOCKET");
    auto restore = qScopeGuard([&] {
      qputenv("XDG_RUNTIME_DIR", oldXdg);
      if (oldSocket.isNull()) qunsetenv("OLDIPMI_AGENT_SOCKET"); else qputenv("OLDIPMI_AGENT_SOCKET", oldSocket);
    });
    qputenv("XDG_RUNTIME_DIR", xdg.path().toLocal8Bit());
    qunsetenv("OLDIPMI_AGENT_SOCKET");
    NoCredentials w;
    Controller c(nullptr, &w, dir.filePath("hosts.json"));
    AgentApi api(&c);
    QCOMPARE(api.socketPath(), xdg.path() + "/oldipmi/agent.sock");
    QVERIFY(!api.start());
    QVERIFY2(api.errorString().contains("is not a directory of this user"), qPrintable(api.errorString()));
    QVERIFY(QDir(dir.filePath("elsewhere")).isEmpty());
    api.stop();
  }
  void instructionsNameTheBinary() {
    QTemporaryDir dir; NoCredentials w;
    Controller c(nullptr, &w, dir.filePath("hosts.json"));
    AgentApi api(&c, dir.filePath("s"));
    auto t = api.mcpInstructions();
    QVERIFY(t.contains("claude mcp add oldipmi -- " + api.mcpPath()));
    QVERIFY(!t.contains("{{"));
  }
};
QTEST_MAIN(AgentApiTest)
#include "agentapi_test.moc"
