#include "agentapi.h"
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
  bool read(const QString &, QString &) override { return false; }
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
  struct R { bool ok = false; QJsonValue value; bool called = false; };
  static R run(AgentApi &api, const QString &op, QJsonObject args = {}) {
    R r;
    api.dispatch(op, args, [&r](bool ok, QJsonValue v) { r = {ok, v, true}; });
    return r;
  }
  // For operations that answer later (power, mount, eject).
  static std::shared_ptr<R> runAsync(AgentApi &api, const QString &op, QJsonObject args = {}) {
    auto r = std::make_shared<R>();
    api.dispatch(op, args, [r](bool ok, QJsonValue v) { *r = {ok, v, true}; });
    return r;
  }
  static QJsonObject request(QLocalSocket &s, const QByteArray &line) {
    s.write(line + "\n");
    if (!QTest::qWaitFor([&] { return s.canReadLine(); }, 5000))
      return {};
    return QJsonDocument::fromJson(s.readLine()).object();
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
          QTRY_VERIFY_WITH_TIMEOUT((s = c.session(e["profile"].toString())) && s->state == "disconnected", 10000);
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
        e["ok"] = r->ok;
        e[r->ok ? "result" : "error"] = r->value;
        steps[n] = e;
        continue;
      }
      const QString at = QString("exchange %1, %2: ").arg(n).arg(e["op"].toString());
      QVERIFY2(r->ok == e["ok"].toBool(), qPrintable(at + "ok is " + (r->ok ? "true" : "false")));
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
  // The whole chain an agent uses: MCP requests on the stdin of a real
  // oldipmi-mcp, which finds this AgentApi's socket through XDG_RUNTIME_DIR
  // (a directory of its own), and the demo console behind it.
  void mcpServerEndToEnd() {
    QTemporaryDir dir, runtime; NoCredentials w;
    Controller c(nullptr, &w, writeProfiles(dir));
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
    QTRY_COMPARE(api.clients().value(0).toMap().value("name").toString(), QString("e2e-agent"));
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
    // At the end of its input the server ends, and the app forgets the agent.
    mcp.closeWriteChannel();
    QVERIFY(mcp.waitForFinished(5000));
    QCOMPARE(mcp.exitStatus(), QProcess::NormalExit);
    QCOMPARE(mcp.exitCode(), 0);
    QTRY_COMPARE(api.clientCount(), 0);
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
    }
    QVERIFY(!run(api, "nope").ok);
  }
  void connectErrorsAndPasswordPrompt() {
    QTemporaryDir dir; NoCredentials w;
    Controller c(nullptr, &w, writeProfiles(dir));
    AgentApi api(&c, dir.filePath("s"));
    auto r = run(api, "connect", {{"profile", "does-not-exist"}});
    QVERIFY(!r.ok);
    QVERIFY(r.value.toString().contains("Available: Demo, Demo Fail, Fake"));
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
    QVERIFY(run(api, "disconnect").ok);
    QVERIFY(!run(api, "status").value.toObject()["passwordRequired"].toBool());
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
    QCOMPARE(api.clients().size(), 2);
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
    QCOMPARE(request(b, R"({"id":9,"op":"status"})")["id"].toInt(), 9);
    // Invalid JSON is answered, not ignored.
    o = request(b, "{oops");
    QVERIFY(!o["ok"].toBool());
    QVERIFY(o["error"].toString().contains("Invalid JSON"));
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
    bool released = false;
    for (auto v : api.activity())
      released = released || v.toMap()["text"].toString().contains("Released 2 held");
    QVERIFY(released);
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
