#include "singleinstance.h"
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
