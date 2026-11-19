#include "console.h"
#include <QQuickWindow>
#include <QShortcut>
#include <QSignalSpy>
#include <QtTest>

// Input capture rules of the console view (see DESIGN.md).
class ConsoleTest : public QObject {
  Q_OBJECT
  QQuickWindow *window = nullptr;
  Console *view = nullptr;

  void key(QEvent::Type type, int qtKey, quint32 scan) {
    QKeyEvent e(type, qtKey, Qt::NoModifier, scan, 0, 0);
    QCoreApplication::sendEvent(window, &e);
  }
  void click(QPoint at) {
    QTest::mousePress(window, Qt::LeftButton, {}, at);
    QTest::mouseRelease(window, Qt::LeftButton, {}, at);
  }
private slots:
  void init() {
    window = new QQuickWindow;
    window->resize(400, 300);
    view = new Console(window->contentItem());
    view->setSize({400, 300});
    QImage frame(200, 150, QImage::Format_RGBA8888);
    frame.fill(Qt::darkBlue);
    view->setFrame(frame);
    window->show();
    QVERIFY(QTest::qWaitForWindowExposed(window));
  }
  void cleanup() { delete window; window = nullptr; view = nullptr; }

  void firstClickOnlyCaptures() {
    QSignalSpy pointer(view, &Console::pointerInput);
    QVERIFY(!view->captured());
    click({200, 150});
    QVERIFY(view->captured());
    QCOMPARE(pointer.count(), 0);           // the capturing click is not forwarded
    click({200, 150});
    QVERIFY(pointer.count() >= 2);          // press and release once captured
    auto press = pointer.first();
    QCOMPARE(press.at(0).toInt(), 100);     // fit mode maps window → framebuffer pixels
    QCOMPARE(press.at(1).toInt(), 75);
    QCOMPARE(press.at(2).toInt(), 1);
  }
  void shortcutsGoToTheServerWhileCaptured() {
    view->capture();
    QKeyEvent override(QEvent::ShortcutOverride, Qt::Key_N, Qt::ControlModifier);
    QCoreApplication::sendEvent(view, &override);
    QVERIFY(override.isAccepted());
    view->release();
    QVERIFY(!view->captured());
    QKeyEvent again(QEvent::ShortcutOverride, Qt::Key_N, Qt::ControlModifier);
    again.ignore();
    QCoreApplication::sendEvent(view, &again);
    QVERIFY(!again.isAccepted());
  }
  void windowShortcutsDoNotFireWhileCaptured() {
    // Real delivery path: QGuiApplication shortcut map → window → focus item.
    QShortcut shortcut(QKeySequence("Ctrl+N"), window);
    QShortcut fullscreen(QKeySequence(Qt::Key_F11), window);
    QSignalSpy fired(&shortcut, &QShortcut::activated);
    QSignalSpy fullscreenFired(&fullscreen, &QShortcut::activated);
    QSignalSpy keys(view, &Console::keyInput);
    view->capture();
    QVERIFY(view->hasActiveFocus());
    QTest::keyClick(window, Qt::Key_N, Qt::ControlModifier);
    QTest::keyClick(window, Qt::Key_F11);
    QCOMPARE(fired.count(), 0);
    QCOMPARE(fullscreenFired.count(), 0);
    view->release();
    QTest::keyClick(window, Qt::Key_N, Qt::ControlModifier);
    QCOMPARE(fired.count(), 1);
  }
  void loneRightCtrlReleases() {
    QSignalSpy keys(view, &Console::keyInput);
    view->capture();
    // evdev 97 (+8 for XKB) is Right Ctrl → HID 228; the tap is still forwarded.
    key(QEvent::KeyPress, Qt::Key_Control, 97 + 8);
    key(QEvent::KeyRelease, Qt::Key_Control, 97 + 8);
    QCOMPARE(keys.count(), 2);
    QCOMPARE(keys.at(0).at(0).toInt(), 228);
    QVERIFY(!view->captured());
  }
  void rightCtrlChordKeepsCapture() {
    view->capture();
    key(QEvent::KeyPress, Qt::Key_Control, 97 + 8);
    key(QEvent::KeyPress, Qt::Key_C, 46 + 8);
    key(QEvent::KeyRelease, Qt::Key_C, 46 + 8);
    key(QEvent::KeyRelease, Qt::Key_Control, 97 + 8);
    QVERIFY(view->captured());
  }
  void releaseKeyCanBeDisabled() {
    view->setProperty("rightCtrlReleases", false);
    view->capture();
    key(QEvent::KeyPress, Qt::Key_Control, 97 + 8);
    key(QEvent::KeyRelease, Qt::Key_Control, 97 + 8);
    QVERIFY(view->captured());
    view->release();
    key(QEvent::KeyPress, Qt::Key_Control, 97 + 8);
    key(QEvent::KeyRelease, Qt::Key_Control, 97 + 8);
    QVERIFY(!view->captured());              // and does not take control either
  }
  void loneRightCtrlTakesControlAgain() {
    QSignalSpy keys(view, &Console::keyInput);
    view->capture();
    key(QEvent::KeyPress, Qt::Key_Control, 97 + 8);
    key(QEvent::KeyRelease, Qt::Key_Control, 97 + 8);
    QVERIFY(!view->captured());
    // The focus is back with the window: the next tap gives keyboard and mouse
    // to the server again, without a mouse (full screen has no status strip).
    key(QEvent::KeyPress, Qt::Key_Control, 97 + 8);
    key(QEvent::KeyRelease, Qt::Key_Control, 97 + 8);
    QVERIFY(view->captured());
    QCOMPARE(keys.count(), 2);                // the capturing tap is not forwarded
  }
  void rightCtrlChordDoesNotTakeControl() {
    view->capture();
    view->release();
    key(QEvent::KeyPress, Qt::Key_Control, 97 + 8);
    key(QEvent::KeyPress, Qt::Key_C, 46 + 8);
    key(QEvent::KeyRelease, Qt::Key_C, 46 + 8);
    key(QEvent::KeyRelease, Qt::Key_Control, 97 + 8);
    QVERIFY(!view->captured());
    key(QEvent::KeyPress, Qt::Key_Control, 29 + 8); // Left Ctrl is an ordinary key
    key(QEvent::KeyRelease, Qt::Key_Control, 29 + 8);
    QVERIFY(!view->captured());
  }
  void rightCtrlLeavesOtherFocusAlone() {
    // A field or list with the focus keeps the key: no capture from there.
    auto *field = new QQuickItem(window->contentItem());
    view->capture();
    view->release();
    field->forceActiveFocus();
    QVERIFY(field->hasActiveFocus());
    key(QEvent::KeyPress, Qt::Key_Control, 97 + 8);
    key(QEvent::KeyRelease, Qt::Key_Control, 97 + 8);
    QVERIFY(!view->captured());
    QVERIFY(field->hasActiveFocus());
  }
  void losingTheFrameReleasesInput() {
    QSignalSpy released(view, &Console::releaseInput);
    view->capture();
    view->setFrame(QImage());
    QVERIFY(!view->captured());
    QVERIFY(released.count() >= 1);
  }
};
QTEST_MAIN(ConsoleTest)
#include "console_test.moc"
