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
#include <QTemporaryFile>
#include <QTest>
#include <unistd.h>

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
  // Collects what this process and the cores it starts write to stderr
  // while it lives.
  struct StderrCapture {
    QTemporaryFile file;
    int saved = -1;
    StderrCapture() {
      if (!file.open())
        qFatal("cannot open a temporary file");
      fflush(stderr);
      saved = dup(2);
      dup2(file.handle(), 2);
    }
    ~StderrCapture() { restore(); }
    void restore() {
      if (saved < 0)
        return;
      fflush(stderr);
      dup2(saved, 2);
      close(saved);
      saved = -1;
    }
    QByteArray text() {
      restore();
      file.seek(0);
      return file.readAll();
    }
  };
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
    s->command({{"op", "key"}, {"input", QJsonObject{{"usage", 4}, {"down", true}}}}, this, record);
    QTRY_COMPARE(results.size(), 1);
    s->command({{"op", "mount"}, {"path", "/srv/iso/install.iso"}}, this, record);
    QTRY_VERIFY(results.size() == 2 && !s->mounted);
    s->command({{"op", "power"}, {"action", "soft"}}, this, record);
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

  // A core that ends after an ISO went away says that the connection ended:
  // the ISO's notice is not why the session is gone.
  void coreThatEndsReplacesTheIsoNotice() {
    auto *s = start({expect("connect"), send(event(3)), send(event(9)), QJsonObject{{"exit", 2}}});
    QTRY_COMPARE(s->state, QString("disconnected"));
    QCOMPARE(s->message, QString("Connection ended. Reconnect to obtain a fresh session."));
    delete s;
  }

  // The class of the error that ends the console stays with its message, for
  // the error page's hint (events.json 10), also once the core has exited.
  void errorKeepsItsClass() {
    auto *s = start({expect("connect"), send(event(10)), QJsonObject{{"exit", 2}}});
    QTRY_COMPARE(seen.size(), 3); // the start, the error and the exit
    QCOMPARE(s->message, event(10)["message"].toString());
    QCOMPARE(s->errorCode, QString("unreachable"));
    delete s;
  }
  // Another message drops it: a class is that of the message it came with.
  void laterMessageDropsTheClass() {
    auto *s = start({expect("connect"), send(event(10)), send(event(0)), QJsonObject{{"exit", 2}}});
    QTRY_COMPARE(s->message, QString("Connection ended. Reconnect to obtain a fresh session."));
    QCOMPARE(s->errorCode, QString());
    delete s;
  }

  // An ISO that goes away without an eject says why (events.json 9), until an
  // ISO is mounted again; a media event without a reason (6) leaves the
  // message alone (eventsDriveTheState).
  void mediaEventSaysWhy() {
    auto *s = start({expect("connect"), send(event(3)), expect("mount"), reply(event(5)), send(event(9)),
                     expect("mount"), reply(event(5))});
    QTRY_VERIFY(s->connected());
    s->command({{"op", "mount"}, {"path", "/srv/iso/install.iso"}});
    QTRY_COMPARE(s->message, QString("The ISO is no longer mounted: the BMC ended the virtual media"));
    QVERIFY(!s->mounted);
    s->command({{"op", "mount"}, {"path", "/srv/iso/install.iso"}});
    QTRY_VERIFY(s->mounted);
    QCOMPARE(seen, (QStringList{
                       "logging_in | Opening console on bmc1.example.net…",
                       "connected | Connected — click the console to control it.",
                       "connected | Connected — click the console to control it. | mounted",
                       "connected | The ISO is no longer mounted: the BMC ended the virtual media",
                       "connected | Connected — click the console to control it. | mounted",
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

  // A command's error is the command's: before the first picture it does not
  // mark the console as failed while connecting, which the connection list
  // kept showing after a normal end (C-06).
  void commandErrorIsNotAStage() {
    auto *s = start({expect("connect"), send(event(3)), expect("power"), reply(event(7)), QJsonObject{{"exit", 0}}});
    QTRY_VERIFY(s->connected());
    s->command({{"op", "power"}, {"action", "soft"}});
    QTRY_COMPARE(s->state, QString("disconnected"));
    QCOMPARE(s->message, QString("power control requires account credentials"));
    QCOMPARE(s->errorStage, QString());
    delete s;
  }

  // sequences.json: whole dialogues, which internal/backend checks the core
  // sends exactly so. Played to a Session, each ends as it should: the core
  // receives exactly the fixture's commands, ids included, every reply settles
  // its callback, the last message stays, and a stage that fails names itself.
  void dialoguesPlayToASession_data() {
    QTest::addColumn<QString>("name");
    QTest::addColumn<QString>("errorStage");
    QTest::addColumn<QStringList>("results");
    QTest::newRow("console in use") << "consoleInUse" << QString()
                                    << QStringList{"1", "1", "0 power control requires account credentials"};
    QTest::newRow("login fails") << "loginFails" << QString(ipc::StageLoggingIn) << QStringList();
    QTest::newRow("discovery fails") << "discoveryFails" << QString(ipc::StageDiscovering) << QStringList();
    QTest::newRow("connection fails") << "connectionFails" << QString(ipc::StageConnecting) << QStringList();
  }
  void dialoguesPlayToASession() {
    QFETCH(QString, name);
    QFETCH(QString, errorStage);
    QFETCH(QStringList, results);
    const QJsonArray dialogue = fixture("sequences.json")[name].toArray();
    QVERIFY(!dialogue.isEmpty());
    QJsonArray steps;
    QList<QJsonObject> desktop;
    QJsonObject last;
    for (const auto &v : dialogue) {
      const QJsonObject step = v.toObject();
      if (step.contains("desktop")) {
        desktop << step["desktop"].toObject();
        steps << QJsonObject{{"expect", desktop.last()["op"]}};
        continue;
      }
      last = step["core"].toObject();
      steps << send(last);
      if (last["state"] == ipc::StateConnected) // a picture, on the video socket
        steps << QJsonObject{{"frame", QJsonArray{64, 48}}};
    }
    auto *s = start(steps, desktop.value(0)["profile"].toObject().toVariantMap());
    QStringList got;
    auto record = [&got](bool ok, QString m) { got << (QString::number(ok) + " " + m).trimmed(); };
    for (int i = 1; i < desktop.size(); ++i) {
      if (i == 1) {
        QTRY_VERIFY(s->connected());
        QTRY_VERIFY(!s->lastFrame.isNull());
      }
      QJsonObject c = desktop[i];
      c.remove("version"); // Session numbers its commands itself
      c.remove("id");
      s->command(c, this, record);
      QTRY_COMPARE(got.size(), i);
    }
    QTRY_COMPARE(s->state, QString(ipc::StateDisconnected));
    QTRY_COMPARE(commands().size(), desktop.size());
    QCOMPARE(commands(), desktop);
    QCOMPARE(got, results);
    QCOMPARE(s->message, last["message"].toString());
    QCOMPARE(s->errorStage, errorStage);
    QVERIFY(!s->mounted);
    delete s;
  }

  // Broken framing from the core ends the session with its own message.
  void brokenFraming_data() {
    QTest::addColumn<QJsonObject>("step");
    QTest::addColumn<QString>("message");
    QTest::newRow("empty control message") << QJsonObject{{"raw", "00000000"}} << "Invalid backend control message.";
    QTest::newRow("control message over 1 MiB") << QJsonObject{{"raw", "00100001"}} << "Invalid backend control message.";
    // "not JSON" and "[1]", each with its length.
    QTest::newRow("control message that is not JSON") << QJsonObject{{"raw", "000000086e6f74204a534f4e"}}
                                                      << "Invalid backend control message.";
    QTest::newRow("control message that is no object") << QJsonObject{{"raw", "000000035b315d"}}
                                                       << "Invalid backend control message.";
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

  // A core that ends settles the waiting callbacks, here of a power command
  // or of a mount that is still waiting for the BMC; while setting up or
  // connected, the message says the connection ended. What it wrote to stderr
  // goes to the app's stderr, the terminal or the journal; it was kept unread
  // before (C-08). A Go panic ends the core with exit code 2.
  void coreThatEndsSettlesCallbacks_data() {
    QTest::addColumn<QJsonObject>("cmd");
    QTest::newRow("power") << QJsonObject{{"op", "power"}, {"action", "soft"}};
    QTest::newRow("mount") << QJsonObject{{"op", "mount"}, {"path", "/srv/iso/install.iso"}};
  }
  void coreThatEndsSettlesCallbacks() {
    QFETCH(QJsonObject, cmd);
    StderrCapture err;
    auto *s = start({expect("connect"), send(event(3)), QJsonObject{{"expect", cmd["op"]}},
                     QJsonObject{{"stderr", "panic: test\n"}}, QJsonObject{{"exit", 2}}});
    QTRY_VERIFY(s->connected());
    QStringList results;
    s->command(cmd, this, [&results](bool ok, QString m) { results << QString::number(ok) + " " + m; });
    QTRY_COMPARE(s->state, QString("disconnected"));
    QCOMPARE(results, QStringList{"0 Connection ended."});
    QCOMPARE(s->message, QString("Connection ended. Reconnect to obtain a fresh session."));
    QVERIFY(s->lastFrame.isNull());
    QVERIFY(!s->mounted);
    delete s;
    QVERIFY(err.text().contains("panic: test\n"));
  }

  // A core that cannot start and one that crashes each get their own
  // message; only the first is about the bundle (C-08).
  void coreThatDoesNotRun_data() {
    QTest::addColumn<QString>("binDir");
    QTest::addColumn<QJsonArray>("steps");
    QTest::addColumn<QString>("message");
    QTest::newRow("missing") << dir.filePath("no-core") << QJsonArray{}
                             << "The native backend could not run. Check the application bundle.";
    QTest::newRow("killed") << QString(FAKECORE_DIR) << QJsonArray{expect("connect"), send(event(3)), QJsonObject{{"signal", 9}}}
                            << "The native backend crashed. Reconnect to obtain a fresh session.";
  }
  void coreThatDoesNotRun() {
    QFETCH(QString, binDir);
    QFETCH(QJsonArray, steps);
    QFETCH(QString, message);
    auto *s = start(steps, {{"host", "bmc1.example.net"}}, binDir);
    QTRY_COMPARE(s->state, QString("disconnected"));
    QTRY_COMPARE(s->message, message);
    QVERIFY(s->lastFrame.isNull());
    delete s;
  }

  // Closing settles them too, and later commands fail at once.
  void stopSettlesCallbacks() {
    auto *s = start({expect("connect"), send(event(3)), expect("power")});
    QTRY_VERIFY(s->connected());
    QStringList results;
    auto record = [&results](bool ok, QString m) { results << QString::number(ok) + " " + m; };
    s->command({{"op", "power"}, {"action", "soft"}}, this, record);
    QTRY_COMPARE(commands().size(), 2);
    s->stop();
    QCOMPARE(results, QStringList{"0 Connection closed."});
    s->command({{"op", "eject"}}, this, record);
    QCOMPARE(results, (QStringList{"0 Connection closed.", "0 Not connected."}));
    delete s;
  }

  // A callback runs only while its context exists: once the object that asked
  // is gone, its reply is dropped (C-07).
  void callbackNeedsItsContext() {
    auto *s = start({expect("connect"), send(event(3)), expect("power"), reply(event(7)), expect("key"), reply(event(4))});
    QTRY_VERIFY(s->connected());
    QStringList results;
    auto record = [&results](const QString &op) {
      return [&results, op](bool ok, QString m) { results << (op + " " + QString::number(ok) + " " + m).trimmed(); };
    };
    auto *asker = new QObject;
    s->command({{"op", "power"}, {"action", "soft"}}, asker, record("power"));
    delete asker;
    s->command({{"op", "key"}, {"input", QJsonObject{{"usage", 4}, {"down", false}}}}, this, record("key"));
    QTRY_COMPARE(results, QStringList{"key 1"}); // the core answers in order: power's reply came first
    delete s;
  }

  // A reply settles the command with its id, in whatever order the replies
  // come: the core may answer a key before a slower mount (A1-05).
  void repliesSettleTheirCommandsInAnyOrder() {
    auto *s = start({expect("connect"), send(event(3)), expect("mount"), expect("key"),
                     reply({{"version", 1}, {"type", "error"}, {"message", "key refused"}}),
                     send({{"version", 1}, {"id", 2}, {"type", "ok"}, {"mounted", true}})});
    QTRY_VERIFY(s->connected());
    QStringList results;
    auto record = [&results](const QString &op) {
      return [&results, op](bool ok, QString m) { results << (op + " " + QString::number(ok) + " " + m).trimmed(); };
    };
    s->command({{"op", "mount"}, {"path", "/srv/iso/install.iso"}}, this, record("mount"));
    s->command({{"op", "key"}, {"input", QJsonObject{{"usage", 4}, {"down", true}}}}, this, record("key"));
    QTRY_COMPARE(results, (QStringList{"key 0 key refused", "mount 1"}));
    QVERIFY(s->mounted);
    const auto sent = commands();
    QCOMPARE(sent.size(), 3);
    QCOMPARE(sent[1]["op"].toString(), QString("mount"));
    QCOMPARE(sent[1]["id"].toInt(), 2);
    delete s;
  }

  // A session that is deleted, as at quit or when its console closes, runs no
  // callback from its destructor, where what the callback uses may be going
  // away too. Each gets its answer from the event loop, while its context exists.
  void deletedSessionAnswersLater() {
    auto *s = start({expect("connect"), send(event(3)), expect("power"), expect("eject")});
    QTRY_VERIFY(s->connected());
    QStringList results;
    auto *asker = new QObject;
    s->command({{"op", "power"}, {"action", "soft"}}, this, [&results](bool ok, QString m) { results << QString::number(ok) + " " + m; });
    s->command({{"op", "eject"}}, asker, [&results](bool, QString) { results << "asker gone, still called"; });
    QTRY_COMPARE(commands().size(), 3);
    delete s;
    QVERIFY(results.isEmpty());
    delete asker;
    QTRY_COMPARE(results, QStringList{"0 Connection closed."});
    QTest::qWait(50);
    QCOMPARE(results, QStringList{"0 Connection closed."});
  }
};

QTEST_GUILESS_MAIN(SessionTest)
#include "session_test.moc"
