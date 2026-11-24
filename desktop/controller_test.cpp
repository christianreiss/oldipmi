#include "controller.h"
#include "filedialogs.h"
#include <QApplication>
#include <QClipboard>
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
  bool failRemove = false;
  int reads = 0, writes = 0, removes = 0;
  Entry snapshot(const QString &id, QString &password) override {
    if (!available)
      return Entry::Unavailable;
    if (!values.contains(id))
      return Entry::Missing;
    password = values.value(id);
    return Entry::Present;
  }
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
    if (!available || failRemove) return false;
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
  // Only the wallet's failures offer to connect without saving: an address of
  // only spaces says so, and nothing waits for an answer then (C-05).
  void onlyTheWalletOffersToConnectWithoutSaving() {
    QTemporaryDir dir; FakeCredentials wallet;
    Controller c(nullptr, &wallet, writeProfiles(dir, {savedProfile()}));
    QSignalSpy fallback(&c, &Controller::walletUnavailable);
    auto p = c.hosts()[0].toMap();
    auto blank = p;
    blank["host"] = "  ";
    c.connectHost(blank, "fixture-secret");
    QCOMPARE(c.message(), QString("Enter a BMC address before saving."));
    QCOMPARE(fallback.count(), 0);
    QVERIFY(c.pendingProfile().isEmpty());
    wallet.available = false;
    c.connectHost(p, "fixture-secret"); // the wallet cannot store the password
    QCOMPARE(fallback.count(), 1);
    QCOMPARE(c.pendingProfile().value("id"), p.value("id"));
    c.cancelCredentials();
    p["remember"] = false; // nor remove the one it has
    c.connectHost(p, "fixture-secret");
    QCOMPARE(fallback.count(), 2);
    c.cancelCredentials();
    QVERIFY(c.sessions().isEmpty());
  }
  // A profiles file that cannot be read or written: its message, no offer to
  // connect without saving, and no console.
  void unsavableProfileSaysWhy() {
    QTemporaryDir dir; FakeCredentials wallet;
    const QVariantMap p{{"host", "bmc.example.invalid"}, {"username", "admin"},
                        {"family", "ami"}, {"remember", true}};
    QFile broken(dir.filePath("hosts.json"));
    QVERIFY(broken.open(QIODevice::WriteOnly)); broken.write("broken"); broken.close();
    QFile blocker(dir.filePath("blocker")); // a file where the folder should be
    QVERIFY(blocker.open(QIODevice::WriteOnly)); blocker.close();
    const QList<std::pair<QString, QString>> cases{
        {broken.fileName(), "Cannot modify an unreadable profiles file."},
        {dir.filePath("blocker/hosts.json"), "Could not save host profiles."}};
    for (const auto &[path, message] : cases) {
      Controller c(nullptr, &wallet, path);
      QSignalSpy fallback(&c, &Controller::walletUnavailable);
      c.connectHost(p, "fixture-secret");
      QCOMPARE(c.message(), message);
      QCOMPARE(fallback.count(), 0);
      QVERIFY(c.pendingProfile().isEmpty());
      QVERIFY(c.sessions().isEmpty());
    }
  }
  void profileWriteFailureRestoresWallet() {
    QTemporaryDir dir;
    FakeCredentials wallet;
    const auto path = writeProfiles(dir, {savedProfile()});
    Controller c(nullptr, &wallet, path);
    const auto profile = c.host("fixture-id");
    const auto id = profile.value("id").toString();
    wallet.values[id] = "previous";
    const auto backup = dir.filePath("backup.json");
    QVERIFY(QFile::rename(path, backup));
    QVERIFY(QDir().mkdir(path)); // an atomic file commit cannot replace a directory
    const auto unchanged = c.hosts();
    QSignalSpy changes(&c, &Controller::hostsChanged);
    auto failure = ProfileStore::Failure::Wallet;
    QVERIFY(c.saveProfile(profile, "replacement", true, &failure).isEmpty());
    QCOMPARE(failure, ProfileStore::Failure::Profile);
    QCOMPARE(wallet.values.value(id), QString("previous"));
    QVERIFY(!c.removeProfile(id, &failure));
    QCOMPARE(wallet.values.value(id), QString("previous"));
    QCOMPARE(c.hosts(), unchanged);
    QCOMPARE(changes.count(), 0);

    auto fresh = profile;
    fresh.remove("id");
    wallet.failRemove = true;
    QVERIFY(c.saveProfile(fresh, "fixture", true, &failure).isEmpty());
    QCOMPARE(failure, ProfileStore::Failure::Partial);
    QVERIFY(c.message().contains("could not be restored"));
    QCOMPARE(c.hosts(), unchanged);
  }
  void oversizedProfileWriteKeepsLoadableFile() {
    QTemporaryDir dir;
    FakeCredentials wallet;
    const auto path = writeProfiles(dir, {savedProfile()});
    Controller c(nullptr, &wallet, path);
    const auto before = read(path);
    auto profile = c.host("fixture-id");
    profile["name"] = QString(1024 * 1024, 'x');
    QVERIFY(c.saveHost(profile, {}, false).isEmpty());
    QVERIFY(c.message().contains("too large"));
    QCOMPARE(read(path), before);
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
  // The class of a failure goes with the session's message; a notice written
  // after it, which message() then shows, has none.
  void errorCodeGoesWithTheMessageShown() {
    QTemporaryDir dir; FakeCredentials wallet;
    Controller c(nullptr, &wallet, writeProfiles(dir, {savedProfile()}));
    c.connectHost({{"id", "u"}, {"name", "u"}, {"host", "demo-unreachable"}, {"family", "demo"}}, "");
    QTRY_COMPARE(c.state(), QString("disconnected"));
    QCOMPARE(c.errorStage(), QString("logging_in"));
    QCOMPARE(c.errorCode(), QString("unreachable"));
    QVERIFY(c.saveHost({{"name", "No address"}}, {}, false).isEmpty());
    QCOMPARE(c.message(), QString("Enter a BMC address before saving."));
    QCOMPARE(c.errorCode(), QString());
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
  // Closing every console at once (Quit, Disconnect all) names each open
  // console, also those of connections that are not in the list: one opened
  // without saving after KWallet failed, and one deleted while it runs (C-04).
  // The saved ones come in the list's order, the others after them by name.
  void openConsolesIncludeUnsavedConnections() {
    QTemporaryDir dir; FakeCredentials wallet;
    const QByteArray binDir = qgetenv("OLDIPMI_BIN_DIR");
    auto restore = qScopeGuard([&binDir] {
      if (binDir.isNull()) qunsetenv("OLDIPMI_BIN_DIR"); else qputenv("OLDIPMI_BIN_DIR", binDir);
      qunsetenv("OLDIPMI_FAKE_SCRIPT");
    });
    qputenv("OLDIPMI_BIN_DIR", FAKECORE_DIR);
    QFile script(dir.filePath("script.json"));
    QVERIFY(script.open(QIODevice::WriteOnly));
    script.write(R"([{"expect": "connect"}, {"send": {"version": 1, "type": "state", "state": "connected"}}])");
    script.close();
    qputenv("OLDIPMI_FAKE_SCRIPT", script.fileName().toLocal8Bit());
    Controller c(nullptr, &wallet, dir.filePath("hosts.json"));
    auto profile = [](const QString &name) {
      return QVariantMap{{"name", name}, {"host", name.toLower() + ".example.net"}, {"username", "admin"},
                         {"family", "ami"}, {"remember", true}};
    };
    const auto zulu = c.saveHost(profile("Zulu"), "fixture-secret", true);
    const auto deleted = c.saveHost(profile("Deleted"), "fixture-secret", true);
    const auto alpha = c.saveHost(profile("Alpha"), "fixture-secret", true);
    for (const auto &p : {zulu, deleted, alpha})
      c.connectHost(p, "fixture-secret");
    QVERIFY(c.removeHost(deleted.value("id").toString())); // its console runs on
    wallet.available = false; // a new connection, opened without saving it
    c.connectHost(profile("Unsaved"), "fixture-secret");
    c.continueWithoutWallet();
    QCOMPARE(c.hosts().size(), 2);
    QTRY_COMPARE(c.openCount(), 4);
    QStringList named;
    const auto sessions = c.sessions();
    for (const auto &v : c.openConsoles()) {
      const auto o = v.toMap();
      named << o["name"].toString() + " " + o["host"].toString();
      QVERIFY(c.isOpen(o["id"].toString()));
      QCOMPARE(o["startedAt"], sessions[o["id"].toString()].toMap()["startedAt"]);
    }
    QCOMPARE(named, (QStringList{"Zulu zulu.example.net", "Alpha alpha.example.net",
                                 "Deleted deleted.example.net", "Unsaved unsaved.example.net"}));
  }
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
    struct Chooser : FileDialogs {
      std::function<void()> meanwhile; // what happens while the user chooses
      QString iso;
      QString isoImage() override {
        meanwhile();
        return iso;
      }
    } dialogs;
    Controller c(nullptr, &wallet, dir.filePath("hosts.json"), &dialogs);
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
    dialogs.iso = iso;
    dialogs.meanwhile = [&c] { c.show("b"); }; // what an agent's connect does
    c.mountISO();
    QCOMPARE(c.shownId(), QString("b"));
    QTRY_VERIFY(received("a").contains(R"("op":"mount")"));
    QTest::qWait(100);
    QVERIFY(!received("b").contains(R"("op":"mount")"));
  }
  // With Quit and Disconnect all, the cores log out of their BMCs at the same
  // time, and quitting waits for all of them.
  void closingConsolesLogsThemOutTogether_data() {
    QTest::addColumn<bool>("quit");
    QTest::newRow("quit") << true;
    QTest::newRow("disconnect all") << false;
  }
  void closingConsolesLogsThemOutTogether() {
    QFETCH(bool, quit);
    QTemporaryDir dir; FakeCredentials wallet;
    const QByteArray binDir = qgetenv("OLDIPMI_BIN_DIR");
    auto restore = qScopeGuard([&binDir] {
      if (binDir.isNull()) qunsetenv("OLDIPMI_BIN_DIR"); else qputenv("OLDIPMI_BIN_DIR", binDir);
      qunsetenv("OLDIPMI_FAKE_SCRIPT"); qunsetenv("OLDIPMI_FAKE_LOG");
    });
    qputenv("OLDIPMI_BIN_DIR", FAKECORE_DIR);
    QFile script(dir.filePath("script.json"));
    QVERIFY(script.open(QIODevice::WriteOnly));
    // Each stand-in core takes a second to log out after the disconnect.
    script.write(R"([{"expect": "connect"}, {"send": {"version": 1, "type": "state", "state": "connected"}}, {"logout": 1000}])");
    script.close();
    qputenv("OLDIPMI_FAKE_SCRIPT", script.fileName().toLocal8Bit());
    const QList<const char *> ids{"a", "b", "c"};
    QElapsedTimer closing;
    {
      Controller c(nullptr, &wallet, dir.filePath("hosts.json"));
      for (const char *id : ids) { // one stand-in core each, with its own log
        qputenv("OLDIPMI_FAKE_LOG", dir.filePath(QString(id) + ".jsonl").toLocal8Bit());
        c.connectHost({{"id", id}, {"name", id}, {"host", QString(id) + ".example.net"}, {"family", "demo"}}, "");
        QTRY_VERIFY(c.session(id) && c.session(id)->connected());
      }
      closing.start();
      if (!quit)
        c.disconnectAll();
    } // quitting deletes the controller
    // One after the other, the three logouts took three seconds.
    QVERIFY2(closing.elapsed() < 2000, qPrintable(QString("closing took %1 ms").arg(closing.elapsed())));
    for (const char *id : ids) {
      QFile log(dir.filePath(QString(id) + ".jsonl"));
      QVERIFY(log.open(QIODevice::ReadOnly));
      QVERIFY2(log.readAll().contains(R"("loggedOut":true)"), id);
    }
  }
  // Closing a console does not wait for its core to log out of the BMC: that
  // goes on in the background, and quitting waits for it (C-08).
  void closingAConsoleDoesNotWait() {
    QTemporaryDir dir; FakeCredentials wallet;
    const QByteArray binDir = qgetenv("OLDIPMI_BIN_DIR");
    auto restore = qScopeGuard([&binDir] {
      if (binDir.isNull()) qunsetenv("OLDIPMI_BIN_DIR"); else qputenv("OLDIPMI_BIN_DIR", binDir);
      qunsetenv("OLDIPMI_FAKE_SCRIPT"); qunsetenv("OLDIPMI_FAKE_LOG");
    });
    qputenv("OLDIPMI_BIN_DIR", FAKECORE_DIR);
    QFile script(dir.filePath("script.json"));
    QVERIFY(script.open(QIODevice::WriteOnly));
    script.write(R"([{"expect": "connect"}, {"send": {"version": 1, "type": "state", "state": "connected"}}, {"logout": 1000}])");
    script.close();
    qputenv("OLDIPMI_FAKE_SCRIPT", script.fileName().toLocal8Bit());
    const QString log = dir.filePath("a.jsonl");
    qputenv("OLDIPMI_FAKE_LOG", log.toLocal8Bit());
    auto loggedOut = [&log] {
      QFile f(log);
      return f.open(QIODevice::ReadOnly) && f.readAll().contains(R"("loggedOut":true)");
    };
    {
      Controller c(nullptr, &wallet, dir.filePath("hosts.json"));
      c.connectHost({{"id", "a"}, {"name", "a"}, {"host", "a.example.net"}, {"family", "demo"}}, "");
      QTRY_VERIFY(c.session("a") && c.session("a")->connected());
      QElapsedTimer closing;
      closing.start();
      c.disconnectProfile("a");
      QVERIFY2(closing.elapsed() < 500, qPrintable(QString("closing took %1 ms").arg(closing.elapsed())));
      QVERIFY(!c.session("a"));
      QVERIFY(!loggedOut()); // the core takes a second for it
    } // quitting deletes the controller
    QVERIFY(loggedOut());
  }
};
QTEST_MAIN(ControllerTest)
#include "controller_test.moc"
