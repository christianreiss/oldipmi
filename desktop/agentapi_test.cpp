#include "agentapi.h"
#include <QBuffer>
#include <QImage>
#include <QJsonArray>
#include <QJsonDocument>
#include <QLocalSocket>
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
  void profilesNeverExposeSecrets() {
    QTemporaryDir dir; NoCredentials w;
    Controller c(nullptr, &w, dir.filePath("hosts.json"));
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
    r = run(api, "connect", {{"profile", "bmc.invalid"}}); // by host: the open prompt is reused
    QVERIFY(r.ok && r.value.toObject()["passwordRequired"].toBool());
    QCOMPARE(prompts.count(), 1);
    QVERIFY(run(api, "disconnect").ok);
    QVERIFY(!run(api, "status").value.toObject()["passwordRequired"].toBool());
  }
  void demoConsoleEndToEnd() {
    QTemporaryDir dir; NoCredentials w;
    Controller c(nullptr, &w, writeProfiles(dir));
    AgentApi api(&c, dir.filePath("s"));
    auto r = run(api, "connect", {{"profile", "Demo"}});
    QVERIFY2(r.ok, qPrintable(r.value.toString()));
    QTRY_COMPARE_WITH_TIMEOUT(c.state(), QString("connected"), 10000);
    QTRY_VERIFY(!c.lastFrame().isNull());
    QCOMPARE(c.lastFrame().size(), QSize(1024, 768));
    auto st = run(api, "status").value.toObject();
    QCOMPARE(st["profile"].toObject()["name"].toString(), QString("Demo"));
    QVERIFY(st["consoleInfo"].toObject()["protocol"].toString().contains("demo"));
    // Same profile again: no error, nothing restarted. Another profile: busy.
    r = run(api, "connect", {{"profile", "demo-1"}});
    QVERIFY(r.ok && r.value.toObject()["already"].toBool());
    QVERIFY(run(api, "connect", {{"profile", "Demo Fail"}}).value.toString().contains("busy"));
    // Keys and pointer reach the demo session, which redraws.
    auto before = c.changeSeq();
    QVERIFY(run(api, "key", {{"usage", 4}, {"down", true}}).ok);
    QVERIFY(run(api, "key", {{"usage", 4}, {"down", false}}).ok);
    QTRY_VERIFY(c.changeSeq() > before);
    QVERIFY(!run(api, "key", {{"usage", 300}, {"down", true}}).ok);
    QVERIFY(!run(api, "pointer", {{"x", 1024}, {"y", 0}}).ok);
    before = c.changeSeq();
    QVERIFY(run(api, "pointer", {{"x", 300}, {"y", 200}, {"buttons", 1}}).ok);
    QVERIFY(run(api, "pointer", {{"x", 300}, {"y", 200}, {"buttons", 0}}).ok);
    QTRY_VERIFY(c.changeSeq() > before);
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
    QVERIFY(run(api, "disconnect").ok);
    QCOMPARE(c.state(), QString("disconnected"));
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
    QTRY_VERIFY(!c.lastFrame().isNull());
    QVERIFY(request(*s, R"({"id":2,"op":"key","args":{"usage":225,"down":true}})")["ok"].toBool());
    QVERIFY(request(*s, R"({"id":3,"op":"pointer","args":{"x":10,"y":10,"buttons":1}})")["ok"].toBool());
    QTest::qWait(300);
    auto before = c.changeSeq();
    delete s; // the agent process goes away while holding Shift and the left button
    QTRY_COMPARE(api.clientCount(), 0);
    QTRY_VERIFY(c.changeSeq() > before); // the demo screen redraws for the key-up/button-up
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
