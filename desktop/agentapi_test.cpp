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

class AgentApiTest : public QObject {
  Q_OBJECT
  struct R { bool ok = false; QJsonValue value; bool called = false; };
  static R run(AgentApi &api, const QString &op, QJsonObject args = {}) {
    R r;
    api.dispatch(op, args, [&r](bool ok, QJsonValue v) { r = {ok, v, true}; });
    return r;
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
  void connectErrors() {
    QTemporaryDir dir; NoCredentials w;
    Controller c(nullptr, &w, dir.filePath("hosts.json"));
    AgentApi api(&c, dir.filePath("s"));
    QVERIFY(!run(api, "connect", {{"profile", "does-not-exist"}}).ok);
    auto r = run(api, "connect", {{"profile", "lab-ami"}}); // case-insensitive, no saved password
    QVERIFY(!r.ok);
    QVERIFY(r.value.toString().contains("password", Qt::CaseInsensitive));
    QCOMPARE(c.state(), QString("disconnected"));
  }
  void socketIsOwnerOnlyAndAnswers() {
    QTemporaryDir dir; NoCredentials w;
    Controller c(nullptr, &w, dir.filePath("hosts.json"));
    QString path = dir.filePath("run/agent.sock");
    AgentApi api(&c, path);
    QVERIFY(api.start());
    QVERIFY(api.listening());
    QVERIFY(!(QFile::permissions(path) & (QFile::ReadGroup | QFile::WriteGroup | QFile::ReadOther | QFile::WriteOther)));
    QVERIFY(!(QFile::permissions(QFileInfo(path).absolutePath()) & (QFile::ReadGroup | QFile::ReadOther | QFile::ExeGroup | QFile::ExeOther)));
    QLocalSocket s;
    s.connectToServer(path);
    QTRY_COMPARE(s.state(), QLocalSocket::ConnectedState); // server shares this event loop
    s.write("{\"id\":7,\"op\":\"status\",\"args\":{}}\n");
    QTRY_VERIFY(s.canReadLine());
    auto o = QJsonDocument::fromJson(s.readLine()).object();
    QCOMPARE(o["id"].toInt(), 7);
    QVERIFY(o["ok"].toBool());
    QCOMPARE(o["result"].toObject()["state"].toString(), QString("disconnected"));
    // A second instance must not steal a live socket.
    AgentApi second(&c, path);
    QVERIFY(!second.start());
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
