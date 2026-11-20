#include "controller.h"
#include <QClipboard>
#include <QFile>
#include <QJsonArray>
#include <QJsonDocument>
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
private slots:
  void migrationPreservesProfilesAndDeletion() {
    QTemporaryDir dir;
    QString path = dir.filePath("hosts.json");
    QVariantMap existing{{"id","keep-wallet-id"},{"host","LAB-AMI.EXAMPLE.NET"},
                         {"name","My server"},{"username","custom"},{"remember",true},
                         {"legacyTLS",false}};
    auto legacy = QJsonDocument(QJsonArray{QJsonObject::fromVariantMap(existing)}).toJson();
    QFile f(path); QVERIFY(f.open(QIODevice::WriteOnly)); f.write(legacy); f.close();
    FakeCredentials wallet;
    wallet.values["keep-wallet-id"] = "fixture-password";
    QVariantList saved;
    {
      Controller c(nullptr, &wallet, path);
      QCOMPARE(c.hosts().size(), 3);
      QCOMPARE(c.hosts()[0].toMap(), existing);
      QCOMPARE(read(path + ".pre-profiles"), legacy);
      QCOMPARE(wallet.reads + wallet.writes + wallet.removes, 0);
      auto lab-avocent = c.hosts()[1].toMap();
      QCOMPARE(lab-avocent["name"].toString(), "Lab-Avocent");
      QVERIFY(c.removeHost(lab-avocent["id"].toString()));
      saved = c.hosts();
    }
    Controller reloaded(nullptr, &wallet, path);
    QCOMPARE(reloaded.hosts(), saved);
    QCOMPARE(reloaded.hosts().size(), 2);
    QVERIFY(wallet.values.contains("keep-wallet-id"));
    QVERIFY(!read(path).contains("fixture-password"));
    QVERIFY(!(QFile::permissions(path) & (QFile::ReadGroup | QFile::ReadOther)));
  }
  void walletSaveEditRetrieveAndRemove() {
    QTemporaryDir dir; FakeCredentials wallet;
    QString path = dir.filePath("hosts.json");
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
    Controller c(nullptr, &wallet, dir.filePath("hosts.json"));
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
    Controller c(nullptr, &wallet, dir.filePath("hosts.json"));
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
  void failedStageAndActiveProfile() {
    QTemporaryDir dir; FakeCredentials wallet;
    Controller c(nullptr, &wallet, dir.filePath("hosts.json"));
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
};
QTEST_MAIN(ControllerTest)
#include "controller_test.moc"
