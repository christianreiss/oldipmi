#include "controller.h"
#include <QApplication>
#include <QClipboard>
#include <QFileDialog>
#include <QFile>
#include <QJsonArray>
#include <QJsonDocument>
#include <QJsonObject>
#include <QScopeGuard>
#include <QSignalSpy>
#include <QTemporaryDir>
#include <QtTest>

class FakeCredentials : public Credentials {
public:
  QMap<QString, QString> values;
  bool available = true;
  int reads = 0, writes = 0, removes = 0;
  bool read(const QString &id, QString &password) override {
    ++reads;
    if (!available || !values.contains(id)) return false;
    password = values[id];
    return true;
  }
  bool write(const QString &id, const QString &password) override {
    ++writes;
    if (!available) return false;
    values[id] = password;
    return true;
  }
  bool remove(const QString &id) override {
    ++removes;
    if (!available) return false;
    values.remove(id);
    return true;
  }
};
class ControllerTest : public QObject {
  Q_OBJECT
  static QByteArray read(const QString &path) {
    QFile f(path);
    if (!f.open(QIODevice::ReadOnly)) return {};
    return f.readAll();
  }
  // A saved profile with "remember" set and no wallet entry yet.
  static QVariantMap savedProfile() {
    return {{"id", "fixture-id"}, {"name", "Fixture"}, {"host", "bmc.example.invalid"},
            {"username", "admin"}, {"family", "ami"}, {"remember", true}};
  }
  static QString writeProfiles(const QTemporaryDir &dir, const QVariantList &profiles) {
    QString path = dir.filePath("hosts.json");
    QFile f(path);
    if (f.open(QIODevice::WriteOnly))
      f.write(QJsonDocument(QJsonObject{{"version", 1},
                                        {"profiles", QJsonArray::fromVariantList(profiles)}}).toJson());
    return path;
  }
private slots:
  void nothingIsBuiltIn() {
    QTemporaryDir dir; FakeCredentials wallet;
    QString path = dir.filePath("hosts.json");
    {
      Controller c(nullptr, &wallet, path);
      QVERIFY(c.hosts().isEmpty());
    }
    QVERIFY(!QFile::exists(path)); // nothing to save yet
    // Without defaultsVersion, 0.3.0 and older added presets here; now the file stays as it is.
    const QByteArray empty = R"({"version":1,"profiles":[]})";
    QFile f(path); QVERIFY(f.open(QIODevice::WriteOnly)); f.write(empty); f.close();
    Controller c(nullptr, &wallet, path);
    QVERIFY(c.hosts().isEmpty());
    QCOMPARE(read(path), empty);
  }
  void migrationPreservesProfilesAndDeletion() {
    QTemporaryDir dir;
    QString path = dir.filePath("hosts.json");
    QVariantMap existing{{"id","keep-wallet-id"},{"host","BMC1.EXAMPLE.INVALID"},
                         {"name","My server"},{"username","custom"},{"remember",true},
                         {"legacyTLS",false}};
    QVariantMap second{{"id","second-id"},{"host","bmc2.example.invalid"},{"name","Second"},
                       {"username","admin"},{"remember",true}};
    auto legacy = QJsonDocument(QJsonArray{QJsonObject::fromVariantMap(existing),
                                           QJsonObject::fromVariantMap(second)}).toJson();
    QFile f(path); QVERIFY(f.open(QIODevice::WriteOnly)); f.write(legacy); f.close();
    FakeCredentials wallet;
    wallet.values["keep-wallet-id"] = "fixture-password";
    QVariantList saved;
    {
      Controller c(nullptr, &wallet, path);
      QCOMPARE(c.hosts(), (QVariantList{existing, second})); // nothing added
      QCOMPARE(read(path + ".pre-profiles"), legacy);
      auto converted = QJsonDocument::fromJson(read(path)).object();
      QCOMPARE(converted["version"].toInt(), 1);
      QCOMPARE(converted["profiles"].toArray().toVariantList(), c.hosts());
      QCOMPARE(wallet.reads + wallet.writes + wallet.removes, 0);
      QVERIFY(c.removeHost("second-id"));
      saved = c.hosts();
    }
    Controller reloaded(nullptr, &wallet, path);
    QCOMPARE(reloaded.hosts(), saved);
    QCOMPARE(reloaded.hosts(), QVariantList{existing});
    QVERIFY(wallet.values.contains("keep-wallet-id"));
    QVERIFY(!read(path).contains("fixture-password"));
    QVERIFY(!(QFile::permissions(path) & (QFile::ReadGroup | QFile::ReadOther)));
  }
  void walletSaveEditRetrieveAndRemove() {
    QTemporaryDir dir; FakeCredentials wallet;
    QString path = writeProfiles(dir, {savedProfile()});
    Controller c(nullptr, &wallet, path);
    auto p = c.hosts()[0].toMap();
    // Unsupported family is rejected locally, before any network operation.
    p["family"] = "fixture";
    p["password"] = "must-not-persist";
    p["token"] = "must-not-persist";
    p = c.saveHost(p, "fixture-secret", true);
    QVERIFY(!p.isEmpty()); QCOMPARE(wallet.writes, 1);
    p["name"] = "Edited profile";
    p = c.saveHost(p, {}, true);
    QVERIFY(!p.isEmpty()); QCOMPARE(wallet.writes, 1);
    QCOMPARE(wallet.values.value(p["id"].toString()), "fixture-secret");
    c.connectHost(p, {});
    QCOMPARE(wallet.reads, 1);
    QTRY_COMPARE(c.state(), QString("disconnected"));
    QVERIFY2(c.message().contains("unsupported BMC family"), qPrintable(c.message()));
    QVERIFY(!read(path).contains("fixture-secret"));
    QVERIFY(!read(path).contains("must-not-persist"));
    QVERIFY(c.removeHost(p["id"].toString()));
    QVERIFY(!wallet.values.contains(p["id"].toString()));
  }
  void missingAndUnavailableWallet() {
    QTemporaryDir dir; FakeCredentials wallet;
    Controller c(nullptr, &wallet, writeProfiles(dir, {savedProfile()}));
    auto p = c.hosts()[0].toMap(); p["family"] = "fixture";
    QSignalSpy prompt(&c, &Controller::passwordRequired);
    QSignalSpy fallback(&c, &Controller::walletUnavailable);
    c.connectHost(p, {});
    QCOMPARE(prompt.count(), 1);
    QCOMPARE(c.state(), "disconnected");
    wallet.available = false;
    auto before = c.hosts();
    c.connectHost(p, "fixture-secret");
    QCOMPARE(fallback.count(), 1);
    QCOMPARE(c.hosts(), before);
    c.continueWithoutWallet();
    QTRY_COMPARE(c.state(), QString("disconnected"));
    QVERIFY(c.message().contains("unsupported BMC family"));
    QCOMPARE(c.hosts(), before);
    c.cancelCredentials();
    c.continueWithoutWallet();
    QCOMPARE(c.state(), "disconnected");
  }
  void changedAccountNeverReusesPassword() {
    QTemporaryDir dir; FakeCredentials wallet;
    Controller c(nullptr, &wallet, writeProfiles(dir, {savedProfile()}));
    auto p = c.saveHost(c.hosts()[0].toMap(), "fixture-secret", true);
    p["username"] = "different-account";
    QSignalSpy prompt(&c, &Controller::passwordRequired);
    c.connectHost(p, {});
    QCOMPARE(prompt.count(), 1); QCOMPARE(wallet.reads, 0);
    p = c.saveHost(p, {}, true);
    QVERIFY(!wallet.values.contains(p["id"].toString()));
  }
  void duplicateAndNewFieldsPersist() {
    QTemporaryDir dir; FakeCredentials wallet;
    QString path = dir.filePath("hosts.json");
    Controller c(nullptr, &wallet, path);
    auto p = c.saveHost({{"host", " bmc.example.invalid "}, {"username", "ADMIN"}, {"family", "aten"},
                         {"consoleMode", "html5"}, {"lastConnected", "2026-11-01T10:00:00Z"}},
                        "fixture-secret", true);
    QCOMPARE(p["host"].toString(), "bmc.example.invalid");
    QCOMPARE(p["name"].toString(), "bmc.example.invalid"); // empty name falls back to host
    QCOMPARE(p["consoleMode"].toString(), "html5");
    // Editing without lastConnected keeps the recorded value.
    auto edited = p; edited.remove("lastConnected"); edited["name"] = "Edited";
    edited = c.saveHost(edited, {}, true);
    QCOMPARE(edited["lastConnected"].toString(), "2026-11-01T10:00:00Z");
    auto copy = c.duplicateHost(p["id"].toString());
    QVERIFY(!copy.isEmpty());
    QVERIFY(copy["id"] != p["id"]);
    QCOMPARE(copy["name"].toString(), "Edited (copy)");
    QCOMPARE(copy["consoleMode"].toString(), "html5");
    QVERIFY(!copy.contains("lastConnected"));
    QVERIFY(!copy["remember"].toBool());             // the copy has no wallet entry
    QVERIFY(!wallet.values.contains(copy["id"].toString()));
    QCOMPARE(c.host(copy["id"].toString()), copy);
    Controller reloaded(nullptr, &wallet, path);
    QCOMPARE(reloaded.host(p["id"].toString())["consoleMode"].toString(), "html5");
  }
  // What reaches the core when a profile connects: the keys saveHost keeps,
  // exactly those of testdata/ipc/connect.json, of which internal/backend
  // checks that the core reads all but id, lastConnected and remember. A
  // stand-in core (desktop/fakecore_test.cpp) records the command.
  void connectSendsTheContractProfile() {
    QTemporaryDir dir; FakeCredentials wallet;
    QFile f(QStringLiteral(IPC_FIXTURES "/connect.json"));
    QVERIFY(f.open(QIODevice::ReadOnly));
    const QJsonObject want = QJsonDocument::fromJson(f.readAll()).object();
    QFile script(dir.filePath("script.json"));
    QVERIFY(script.open(QIODevice::WriteOnly));
    script.write(R"([{"expect": "connect"}])");
    script.close();
    const QString log = dir.filePath("commands.jsonl");
    const QByteArray binDir = qgetenv("OLDIPMI_BIN_DIR");
    auto restore = qScopeGuard([&binDir] {
      if (binDir.isNull()) qunsetenv("OLDIPMI_BIN_DIR"); else qputenv("OLDIPMI_BIN_DIR", binDir);
      qunsetenv("OLDIPMI_FAKE_SCRIPT"); qunsetenv("OLDIPMI_FAKE_LOG");
    });
    qputenv("OLDIPMI_BIN_DIR", FAKECORE_DIR);
    qputenv("OLDIPMI_FAKE_SCRIPT", script.fileName().toLocal8Bit());
    qputenv("OLDIPMI_FAKE_LOG", log.toLocal8Bit());
    auto profile = want["profile"].toObject().toVariantMap();
    profile["unknownKey"] = "dropped";
    Controller c(nullptr, &wallet, dir.filePath("hosts.json"));
    c.connectHost(profile, want["password"].toString());
    QTRY_VERIFY(QFileInfo(log).size() > 0);
    QFile commands(log);
    QVERIFY(commands.open(QIODevice::ReadOnly));
    QCOMPARE(QJsonDocument::fromJson(commands.readLine()).object(), want);
    QCOMPARE(c.host(profile["id"].toString()).keys(), want["profile"].toObject().keys());
  }
  // The signals each action sends, which QML binds to. Phase 4 splits the
  // Controller behind its API and keeps them; how often changed() fires is
  // not pinned, only that it does.
  void signalsPerAction() {
    QTemporaryDir dir; FakeCredentials wallet;
    Controller c(nullptr, &wallet, dir.filePath("hosts.json"));
    QSignalSpy changed(&c, &Controller::changed), hosts(&c, &Controller::hostsChanged),
        activated(&c, &Controller::profileActivated), prompt(&c, &Controller::passwordRequired),
        fallback(&c, &Controller::walletUnavailable), frames(&c, &Controller::frameReady);
    // hostsChanged, profileActivated, passwordRequired, walletUnavailable, frameReady
    auto counts = [&] {
      return QList<int>{int(hosts.count()), int(activated.count()), int(prompt.count()), int(fallback.count()), int(frames.count())};
    };
    auto reset = [&] {
      for (auto *spy : {&changed, &hosts, &activated, &prompt, &fallback, &frames})
        spy->clear();
    };
    auto p = c.saveHost({{"host", "bmc.example.invalid"}, {"username", "admin"}, {"family", "ami"}}, {}, false);
    QCOMPARE(counts(), (QList<int>{1, 0, 0, 0, 0}));
    QVERIFY(changed.count() > 0);
    reset();
    c.connectHost(p, {}); // no password: the prompt
    QCOMPARE(counts(), (QList<int>{0, 0, 1, 0, 0}));
    QVERIFY(changed.count() > 0);
    reset();
    wallet.available = false; // a password to remember, and no wallet
    auto remembered = p;
    remembered["remember"] = true;
    c.connectHost(remembered, "fixture-secret");
    QCOMPARE(counts(), (QList<int>{0, 0, 0, 1, 0}));
    c.cancelCredentials();
    wallet.available = true;
    auto demo = c.saveHost({{"host", "demo"}, {"family", "demo"}, {"name", "Demo"}}, {}, false);
    reset();
    // Saved again, then stamped "last connected"; the new console is shown,
    // without a picture yet.
    c.connectHost(demo, {});
    QCOMPARE(counts(), (QList<int>{2, 1, 0, 0, 1}));
    QCOMPARE(activated.first().first().toMap().value("id"), demo.value("id"));
    QVERIFY(frames.first().first().value<QImage>().isNull());
    QTRY_COMPARE_WITH_TIMEOUT(c.state(), QString("connected"), 10000);
    QTRY_VERIFY(!frames.last().first().value<QImage>().isNull());
    QVERIFY(changed.count() > 0);
    reset();
    c.disconnectHost(); // the picture goes
    QTRY_COMPARE(c.state(), QString("disconnected"));
    QCOMPARE(counts().mid(0, 4), (QList<int>{0, 0, 0, 0}));
    QVERIFY(!frames.isEmpty() && frames.last().first().value<QImage>().isNull());
    QVERIFY(changed.count() > 0);
    reset();
    QVERIFY(c.removeHost(p["id"].toString()));
    QCOMPARE(counts(), (QList<int>{1, 0, 0, 0, 0}));
  }
  void failedStageAndActiveProfile() {
    QTemporaryDir dir; FakeCredentials wallet;
    Controller c(nullptr, &wallet, writeProfiles(dir, {savedProfile()}));
    auto p = c.hosts()[0].toMap(); p["family"] = "fixture";
    c.connectHost(p, "fixture-secret");
    QCOMPARE(c.activeProfile().value("id"), p["id"]);
    QTRY_COMPARE(c.state(), QString("disconnected"));
    QCOMPARE(c.errorStage(), QString("logging_in"));
    QVERIFY(c.host(p["id"].toString()).contains("lastConnected"));
    auto failure = c.message();
    c.disconnectHost();
    QVERIFY(c.errorStage().isEmpty());
    QVERIFY(c.activeProfile().isEmpty());
    QCOMPARE(c.message(), failure); // nothing was open, so nothing was "cancelled"
  }
  void invalidFileIsPreserved() {
    QTemporaryDir dir; FakeCredentials wallet;
    QString path = dir.filePath("hosts.json");
    QFile f(path); QVERIFY(f.open(QIODevice::WriteOnly)); f.write("broken"); f.close();
    Controller c(nullptr, &wallet, path);
    QVERIFY(c.hosts().isEmpty());
    QVERIFY(c.saveHost({{"host","example.invalid"}}, {}, false).isEmpty());
    QCOMPARE(read(path), QByteArray("broken"));
  }
  void copyFramePutsTheShownScreenOnTheClipboard() {
    QTemporaryDir dir; FakeCredentials wallet;
    Controller c(nullptr, &wallet, dir.filePath("hosts.json"));
    auto *clipboard = QGuiApplication::clipboard();
    clipboard->setText("kept");
    QVERIFY(!c.copyFrame().isValid()); // nothing shown: the clipboard keeps what it had
    QCOMPARE(clipboard->text(), QString("kept"));
    c.preview(); // an 800x600 synthetic framebuffer
    QVERIFY(c.hasFrame());
    QCOMPARE(c.copyFrame(), QSize(800, 600));
    const QImage copied = clipboard->image();
    QCOMPARE(copied.size(), QSize(800, 600));
    QVERIFY(!copied.hasAlphaChannel());
    QCOMPARE(copied.pixelColor(400, 300), QColor("#223548"));
  }
  // The ISO goes to the console that was in front when the file dialog
  // opened. The dialog runs its own event loop, in which an agent may bring
  // another console to the front (C-09).
  void isoGoesToTheConsoleItWasChosenFor() {
    QTemporaryDir dir; FakeCredentials wallet;
    const QString iso = dir.filePath("install.iso");
    QFile isoFile(iso);
    QVERIFY(isoFile.open(QIODevice::WriteOnly));
    isoFile.close();
    const QByteArray binDir = qgetenv("OLDIPMI_BIN_DIR");
    auto restore = qScopeGuard([&binDir] {
      if (binDir.isNull()) qunsetenv("OLDIPMI_BIN_DIR"); else qputenv("OLDIPMI_BIN_DIR", binDir);
      qunsetenv("OLDIPMI_FAKE_SCRIPT"); qunsetenv("OLDIPMI_FAKE_LOG");
    });
    qputenv("OLDIPMI_BIN_DIR", FAKECORE_DIR);
    QFile script(dir.filePath("script.json"));
    QVERIFY(script.open(QIODevice::WriteOnly));
    script.write(R"([{"expect": "connect"}, {"send": {"version": 1, "type": "state", "state": "connected"}}, {"expect": "mount"}])");
    script.close();
    qputenv("OLDIPMI_FAKE_SCRIPT", script.fileName().toLocal8Bit());
    Controller c(nullptr, &wallet, dir.filePath("hosts.json"));
    for (const char *id : {"a", "b"}) { // one stand-in core each, with its own log
      qputenv("OLDIPMI_FAKE_LOG", dir.filePath(QString(id) + ".jsonl").toLocal8Bit());
      c.connectHost({{"id", id}, {"name", id}, {"host", QString(id) + ".example.net"}, {"family", "demo"}}, "");
      QTRY_VERIFY(c.session(id) && c.session(id)->connected());
    }
    auto received = [&dir](const char *id) {
      QFile f(dir.filePath(QString(id) + ".jsonl"));
      return f.open(QIODevice::ReadOnly) ? f.readAll() : QByteArray();
    };
    c.show("a");
    QCoreApplication::setAttribute(Qt::AA_DontUseNativeDialogs);
    QTimer agent;
    connect(&agent, &QTimer::timeout, &c, [&] {
      auto *dialog = qobject_cast<QFileDialog *>(QApplication::activeModalWidget());
      if (!dialog)
        return;
      agent.stop();
      c.show("b"); // what an agent's connect does
      dialog->selectFile(iso);
      static_cast<QDialog *>(dialog)->accept(); // QFileDialog::accept() is protected
    });
    agent.start(20);
    c.mountISO();
    QCOMPARE(c.shownId(), QString("b"));
    QTRY_VERIFY(received("a").contains(R"("op":"mount")"));
    QTest::qWait(100);
    QVERIFY(!received("b").contains(R"("op":"mount")"));
  }
};
QTEST_MAIN(ControllerTest)
#include "controller_test.moc"
