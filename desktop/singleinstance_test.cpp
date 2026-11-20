#include "runtimedir.h"
#include "singleinstance.h"
#include <QDir>
#include <QFile>
#include <sys/stat.h>
#include <unistd.h>
#include <QSignalSpy>
#include <QTemporaryDir>
#include <QTest>

class SingleInstanceTest : public QObject {
  Q_OBJECT
private slots:
  void secondStartRaisesFirst() {
    QTemporaryDir dir;
    QVERIFY(dir.isValid());
    auto first = std::make_unique<SingleInstance>(dir.path());
    QSignalSpy spy(first.get(), &SingleInstance::activationRequested);
    QVERIFY(first->acquire());
    SingleInstance second(dir.path());
    QVERIFY(!second.acquire("tok-1\x01\n\x7f"));
    QVERIFY(spy.wait(2000));
    QCOMPARE(spy.takeFirst().at(0).toByteArray(), QByteArray("tok-1"));
    // Once the first instance ends, the next start owns the session.
    first.reset();
    SingleInstance third(dir.path());
    QVERIFY(third.acquire());
  }
  // --probe-ui only asks: it does not bring the running instance forward.
  void askingDoesNotRaiseFirst() {
    QTemporaryDir dir;
    QVERIFY(dir.isValid());
    SingleInstance first(dir.path());
    QSignalSpy spy(&first, &SingleInstance::activationRequested);
    QVERIFY(first.acquire());
    SingleInstance second(dir.path());
    QVERIFY(!second.acquire({}, false));
    QVERIFY(!spy.wait(300));
  }
  // The runtime directory is made private, and one that is not a directory of
  // this user is refused, as one another user made first would be (B-12).
  void runtimeDirIsPrivate() {
    QTemporaryDir xdg, tmp;
    QVERIFY(xdg.isValid() && tmp.isValid());
    auto mode = [](const QString &path) {
      struct stat st;
      return lstat(QFile::encodeName(path).constData(), &st) == 0 ? int(st.st_mode & 0777) : -1;
    };
    qputenv("XDG_RUNTIME_DIR", xdg.path().toLocal8Bit());
    QCOMPARE(runtimeDir(), xdg.path() + "/oldipmi");
    QCOMPARE(mode(xdg.path() + "/oldipmi"), 0700);
    QVERIFY(QFile::setPermissions(xdg.path() + "/oldipmi", QFileDevice::Permissions(0x7755)));
    QCOMPARE(runtimeDir(), xdg.path() + "/oldipmi");
    QCOMPARE(mode(xdg.path() + "/oldipmi"), 0700);

    // Without XDG_RUNTIME_DIR: both levels in the temp directory.
    qunsetenv("XDG_RUNTIME_DIR");
    const QByteArray oldTmp = qgetenv("TMPDIR");
    qputenv("TMPDIR", tmp.path().toLocal8Bit());
    const QString own = tmp.path() + "/oldipmi-" + QString::number(getuid());
    QVERIFY(QDir().mkdir(own));
    QVERIFY(QFile::setPermissions(own, QFileDevice::Permissions(0x7777)));
    QCOMPARE(runtimeDirPath(), own + "/oldipmi");
    QCOMPARE(runtimeDir(), own + "/oldipmi");
    QCOMPARE(mode(own), 0700);
    QCOMPARE(mode(own + "/oldipmi"), 0700);

    // A link, even to a directory of this user, and a file are refused.
    QVERIFY(QDir(own).removeRecursively());
    QVERIFY(QDir().mkdir(tmp.path() + "/elsewhere"));
    QVERIFY(QFile::link(tmp.path() + "/elsewhere", own));
    QString why;
    QVERIFY(runtimeDir(&why).isEmpty());
    QVERIFY2(why.contains("is not a directory of this user"), qPrintable(why));
    QVERIFY(QFile::remove(own));
    QFile file(own);
    QVERIFY(file.open(QIODevice::WriteOnly));
    file.close();
    QVERIFY(runtimeDir().isEmpty());
    qputenv("TMPDIR", oldTmp);
  }
  // Without a safe runtime directory there is no lock: the app starts
  // without the check rather than put a lock and a socket there (B-12).
  void unsafeRuntimeDirHoldsNoLock() {
    QTemporaryDir xdg, dir;
    QVERIFY(QDir().mkdir(dir.filePath("elsewhere")));
    QVERIFY(QFile::link(dir.filePath("elsewhere"), xdg.filePath("oldipmi")));
    const QByteArray old = qgetenv("XDG_RUNTIME_DIR");
    qputenv("XDG_RUNTIME_DIR", xdg.path().toLocal8Bit());
    {
      SingleInstance s;
      QVERIFY(s.acquire());
    }
    qputenv("XDG_RUNTIME_DIR", old);
    QVERIFY(QDir(dir.filePath("elsewhere")).isEmpty());
  }
  void staleLockIsReclaimed() {
    QTemporaryDir dir;
    QVERIFY(dir.isValid());
    QFile f(dir.path() + "/instance.lock");
    QVERIFY(f.open(QIODevice::WriteOnly));
    f.write("999999999\noldipmi\n\n"); // no such process
    f.close();
    SingleInstance s(dir.path());
    QVERIFY(s.acquire());
  }
};
QTEST_MAIN(SingleInstanceTest)
#include "singleinstance_test.moc"
