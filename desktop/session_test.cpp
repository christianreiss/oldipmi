// The desktop's side of the IPC with its core, and how a Session reacts to what
// arrives: events and replies, broken framing, a core that ends. testdata/ipc
// holds the messages each side sends (internal/backend pins the core's side). A
// stand-in core, desktop/fakecore_test.cpp, plays a script for each case.
#include "session.h"
#include <QFile>
#include <QJsonArray>
#include <QJsonDocument>
#include <QJsonObject>
#include <QSignalSpy>
#include <QTemporaryDir>
#include <QTest>

class SessionTest : public QObject {
  Q_OBJECT
  QTemporaryDir dir;
  int runs = 0;
  QString log;
  QStringList seen; // state, message and mounted at every changed()

  static QJsonDocument fixture(const QString &name) {
    QFile f(QStringLiteral(IPC_FIXTURES "/") + name);
    if (!f.open(QIODevice::ReadOnly))
      qFatal("cannot read %s", qPrintable(f.fileName()));
    return QJsonDocument::fromJson(f.readAll());
  }
  static QJsonObject event(int i) { return fixture("events.json").array()[i].toObject(); }
  static QString snapshot(const Session *s) {
    return s->state + " | " + s->message + (s->mounted ? " | mounted" : "");
  }
  // start runs a Session against the stand-in core, which plays steps.
  Session *start(const QJsonArray &steps, const QVariantMap &profile = {{"host", "bmc1.example.net"}},
                 const QString &binDir = FAKECORE_DIR) {
    ++runs;
    const QString script = dir.filePath(QString("script-%1.json").arg(runs));
    log = dir.filePath(QString("commands-%1.jsonl").arg(runs));
    QFile f(script);
    if (!f.open(QIODevice::WriteOnly) || f.write(QJsonDocument(steps).toJson()) < 0)
      qFatal("cannot write %s", qPrintable(script));
    f.close();
    qputenv("OLDIPMI_BIN_DIR", binDir.toLocal8Bit());
    qputenv("OLDIPMI_FAKE_SCRIPT", script.toLocal8Bit());
    qputenv("OLDIPMI_FAKE_LOG", log.toLocal8Bit());
    seen.clear();
    auto *s = new Session("key", profile, this);
    connect(s, &Session::changed, this, [this, s] { seen << snapshot(s); });
    s->begin("pw-1");
    return s;
  }
  // The commands the core received, in order.
  QList<QJsonObject> commands() const {
    QList<QJsonObject> out;
    QFile f(log);
    if (f.open(QIODevice::ReadOnly))
      for (const auto &line : f.readAll().split('\n'))
        if (!line.isEmpty())
          out << QJsonDocument::fromJson(line).object();
    return out;
  }
  static QJsonObject expect(const char *op) { return {{"expect", op}}; }
  static QJsonObject send(const QJsonObject &e) { return {{"send", e}}; }
  static QJsonObject reply(const QJsonObject &e) { return {{"reply", e}}; }

private slots:
  void initTestCase() { QVERIFY(dir.isValid()); }

  // The connect command carries the profile as the desktop keeps it
  // (Controller::saveHost) and the password.
  void connectCommandMatchesTheContract() {
    const QJsonObject want = fixture("connect.json").object();
    auto *s = start({expect("connect")}, want["profile"].toObject().toVariantMap());
    QTRY_COMPARE(commands().size(), 1);
    QCOMPARE(commands()[0], want);
    delete s;
    QTRY_COMPARE(commands().size(), 2);
    QCOMPARE(commands()[1], (QJsonObject{{"version", 1}, {"id", 2}, {"op", "disconnect"}}));
  }

  // Every further command: what Session builds (key, pointer, release,
  // disconnect) and the version and id each one gets.
  void commandsMatchTheContract() {
    auto *s = start({expect("connect"), send(event(3)), expect("power")});
    QTRY_VERIFY(s->connected());
    s->key(4, true);
    s->pointer(640, 360, 1, -1);
    s->releaseInput();
    s->command({{"op", "mount"}, {"path", "/srv/iso/install.iso"}});
    s->command({{"op", "eject"}});
    s->command({{"op", "power"}, {"action", "soft"}});
    QTRY_COMPARE(commands().size(), 7);
    delete s;
    QTRY_COMPARE(commands().size(), 8);
    const QJsonArray want = fixture("commands.json").array();
    for (int i = 0; i < want.size(); ++i)
      QCOMPARE(commands()[i + 1], want[i].toObject());
  }

  // events.json in order: the stages and their messages, the console info and
  // resolution, replies that settle their callback, a media event that settles
  // none, an error reply, and the console's end. Every changed() is listed, so
  // splitting Session (Phase 4) shows any signal it adds or drops.
  void eventsDriveTheState() {
    auto *s = start({expect("connect"), send(event(0)), send(event(1)), send(event(2)), send(event(3)),
                     expect("release"), QJsonObject{{"frame", QJsonArray{64, 48}}},
                     expect("key"), reply(event(4)),
                     expect("mount"), reply(event(5)), send(event(6)),
                     expect("power"), reply(event(7)), send(event(8))});
    QSignalSpy frames(s, &Session::frameReady);
    QTRY_VERIFY(s->connected());
    QVariantMap info = event(2)["info"].toObject().toVariantMap();
    QCOMPARE(s->consoleInfo, info);
    s->releaseInput();
    QTRY_VERIFY(!s->lastFrame.isNull());
    QCOMPARE(s->lastFrame.size(), QSize(64, 48));
    QCOMPARE(frames.count(), 1);
    info["resolution"] = "64 × 48";
    QCOMPARE(s->consoleInfo, info);
    QStringList results;
    auto record = [&results](bool ok, QString m) { results << (QString::number(ok) + " " + m).trimmed(); };
    s->command({{"op", "key"}, {"input", QJsonObject{{"usage", 4}, {"down", true}}}}, record);
    QTRY_COMPARE(results.size(), 1);
    s->command({{"op", "mount"}, {"path", "/srv/iso/install.iso"}}, record);
    QTRY_VERIFY(results.size() == 2 && !s->mounted);
    s->command({{"op", "power"}, {"action", "soft"}}, record);
    QTRY_COMPARE(s->state, QString("disconnected"));
    QCOMPARE(results, (QStringList{"1", "1", "0 power control requires account credentials"}));
    QCOMPARE(s->errorStage, QString());
    QCOMPARE(seen, (QStringList{
                       "logging_in | Opening console on bmc1.example.net…",
                       "logging_in | Logging in…",
                       "discovering | Discovering console…",
                       "connecting | Connecting to console…",
                       "connected | Connected — click the console to control it.",
                       "connected | Connected — click the console to control it.", // the resolution
                       "connected | Connected — click the console to control it.", // the first frame
                       "connected | Connected — click the console to control it.", // the key's reply
                       "connected | Connected — click the console to control it. | mounted",
                       "connected | Connected — click the console to control it.", // the media event
                       "connected | power control requires account credentials",
                       "disconnected | Console connection failed: EOF",
                   }));
    delete s;
  }

  // A stage that fails names itself: the error arrives before a picture.
  void errorBeforeThePictureNamesTheStage() {
    auto *s = start({expect("connect"), send(event(0)), send(event(1)),
                     send({{"version", 1}, {"type", "error"}, {"state", "disconnected"}, {"message", "Console discovery failed: no JNLP"}})});
    QTRY_COMPARE(s->state, QString("disconnected"));
    QCOMPARE(s->errorStage, QString("discovering"));
    QCOMPARE(s->message, QString("Console discovery failed: no JNLP"));
    delete s;
  }

  // Broken framing from the core ends the session with its own message.
  void brokenFraming_data() {
    QTest::addColumn<QJsonObject>("step");
    QTest::addColumn<QString>("message");
    QTest::newRow("empty control message") << QJsonObject{{"raw", "00000000"}} << "Invalid backend control message.";
    QTest::newRow("control message over 1 MiB") << QJsonObject{{"raw", "00100001"}} << "Invalid backend control message.";
    QTest::newRow("another IPC version")
        << send({{"version", 2}, {"type", "state"}, {"state", "connected"}}) << "Incompatible backend version.";
    QTest::newRow("picture without a size") << QJsonObject{{"videoRaw", "000000080000000000000000"}}
                                            << "Invalid framebuffer from backend.";
    QTest::newRow("picture of the wrong length") << QJsonObject{{"videoRaw", "000000100000000200000002"}}
                                                 << "Invalid framebuffer from backend.";
  }
  void brokenFraming() {
    QFETCH(QJsonObject, step);
    QFETCH(QString, message);
    auto *s = start({expect("connect"), step});
    QTRY_COMPARE(s->state, QString("disconnected"));
    QCOMPARE(s->message, message);
    delete s;
  }

  // A core that ends settles the waiting callbacks; while setting up or
  // connected, the message says the connection ended. What it wrote to stderr
  // is not shown (C-08, Phase 1). A Go panic ends the core with exit code 2.
  void coreThatEndsSettlesCallbacks() {
    auto *s = start({expect("connect"), send(event(3)), expect("power"),
                     QJsonObject{{"stderr", "panic: test\n"}}, QJsonObject{{"exit", 2}}});
    QTRY_VERIFY(s->connected());
    QStringList results;
    s->command({{"op", "power"}, {"action", "soft"}}, [&results](bool ok, QString m) { results << QString::number(ok) + " " + m; });
    QTRY_COMPARE(s->state, QString("disconnected"));
    QCOMPARE(results, QStringList{"0 Connection ended."});
    QCOMPARE(s->message, QString("Connection ended. Reconnect to obtain a fresh session."));
    QVERIFY(s->lastFrame.isNull());
    delete s;
  }

  // A core that cannot start and one killed by a signal get the same message
  // today, though only the first is about the bundle (C-08, Phase 1).
  void coreThatDoesNotRun_data() {
    QTest::addColumn<QString>("binDir");
    QTest::addColumn<QJsonArray>("steps");
    QTest::newRow("missing") << dir.filePath("no-core") << QJsonArray{};
    QTest::newRow("killed") << QString(FAKECORE_DIR) << QJsonArray{expect("connect"), send(event(3)), QJsonObject{{"signal", 9}}};
  }
  void coreThatDoesNotRun() {
    QFETCH(QString, binDir);
    QFETCH(QJsonArray, steps);
    auto *s = start(steps, {{"host", "bmc1.example.net"}}, binDir);
    QTRY_COMPARE(s->state, QString("disconnected"));
    QTRY_COMPARE(s->message, QString("The native backend could not run. Check the application bundle."));
    QVERIFY(s->lastFrame.isNull());
    delete s;
  }

  // Closing settles them too, and later commands fail at once.
  void stopSettlesCallbacks() {
    auto *s = start({expect("connect"), send(event(3)), expect("power")});
    QTRY_VERIFY(s->connected());
    QStringList results;
    auto record = [&results](bool ok, QString m) { results << QString::number(ok) + " " + m; };
    s->command({{"op", "power"}, {"action", "soft"}}, record);
    QTRY_COMPARE(commands().size(), 2);
    s->stop();
    QCOMPARE(results, QStringList{"0 Connection closed."});
    s->command({{"op", "eject"}}, record);
    QCOMPARE(results, (QStringList{"0 Connection closed.", "0 Not connected."}));
    delete s;
  }
};

QTEST_GUILESS_MAIN(SessionTest)
#include "session_test.moc"
