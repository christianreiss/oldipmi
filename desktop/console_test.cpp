#include "console.h"
#include <QQuickWindow>
#include <QShortcut>
#include <QSignalSpy>
#include <QtTest>

// Takes every key, like a text field: nothing propagates to its parent.
class KeyField : public QQuickItem {
public:
  using QQuickItem::QQuickItem;

protected:
  void keyPressEvent(QKeyEvent *e) override { e->accept(); }
  void keyReleaseEvent(QKeyEvent *e) override { e->accept(); }
};

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
  void wheel(QPointF at, QPoint angle = {0, 120}, Qt::MouseButtons buttons = Qt::NoButton) {
    QWheelEvent e(at, window->mapToGlobal(at), {}, angle, buttons, Qt::NoModifier, Qt::NoScrollPhase, false);
    QCoreApplication::sendEvent(window, &e);
  }
  // What the server takes as an action: a button down or a wheel turn.
  static int actions(const QSignalSpy &pointer) {
    int n = 0;
    for (const auto &args : pointer)
      n += args.at(2).toInt() != 0 || args.at(3).toInt() != 0;
    return n;
  }
  // A 150x150 picture, shown 300x300 in the middle of 400x300: margins left and right.
  void squareFrame() {
    QImage square(150, 150, QImage::Format_RGBA8888);
    square.fill(Qt::darkBlue);
    view->setFrame(square);
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
  // The margin around the picture is not the remote screen: a press or a wheel
  // turn there is not sent, while a drag from the picture goes on to its edge.
  // Moving over the margin still moves the remote pointer along the edge.
  void marginIsNotSentToTheServer() {
    squareFrame();
    click({200, 150});
    QVERIFY(view->captured());
    QSignalSpy pointer(view, &Console::pointerInput);
    click({20, 150});
    click({390, 150});
    wheel({20, 150});
    QCOMPARE(actions(pointer), 0);
    wheel({200, 150}); // on the picture it is sent
    QCOMPARE(actions(pointer), 1);
    QTest::mouseMove(window, {100, 150});
    pointer.clear();
    QTest::mousePress(window, Qt::LeftButton, {}, {100, 150});
    QTest::mouseMove(window, {20, 150});
    QTest::mouseRelease(window, Qt::LeftButton, {}, {20, 150});
    QVERIFY(pointer.count() >= 2);
    QCOMPARE(pointer.first().at(2).toInt(), 1); // pressed on the picture
    QCOMPARE(pointer.last().at(0).toInt(), 0);  // released at the picture's left edge
    QCOMPARE(pointer.last().at(2).toInt(), 0);
  }
  // A wheel turn while the button of a press that was not sent is still held is
  // not sent either (the server never saw that press, nor will it see the
  // release), and a sideways scroll has no wheel step to send.
  void wheelFollowsItsGesture() {
    squareFrame();
    click({200, 150});
    QVERIFY(view->captured());
    QSignalSpy pointer(view, &Console::pointerInput);
    QTest::mousePress(window, Qt::LeftButton, {}, {20, 150}); // on the margin: not sent
    QTest::mouseMove(window, {200, 150});                     // onto the picture, button held
    wheel({200, 150}, {0, 120}, Qt::LeftButton);
    QTest::mouseRelease(window, Qt::LeftButton, {}, {200, 150});
    QCOMPARE(actions(pointer), 0);
    wheel({200, 150}, {120, 0});
    QCOMPARE(actions(pointer), 0);
    wheel({200, 150}, {0, -120});
    QCOMPARE(actions(pointer), 1);
    QCOMPARE(pointer.last().at(2).toInt(), 0);  // no button
    QCOMPARE(pointer.last().at(3).toInt(), -1); // one step down
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
  void heldKeyBlocksTakingControl() {
    // Right Ctrl pressed while another key is down is a chord too, so typing does
    // not move to the server unnoticed.
    QSignalSpy keys(view, &Console::keyInput);
    view->capture();
    view->release();
    key(QEvent::KeyPress, Qt::Key_A, 30 + 8);
    key(QEvent::KeyPress, Qt::Key_Control, 97 + 8);
    key(QEvent::KeyRelease, Qt::Key_Control, 97 + 8);
    QVERIFY(!view->captured());
    key(QEvent::KeyRelease, Qt::Key_A, 30 + 8);
    key(QEvent::KeyPress, Qt::Key_Control, 97 + 8); // every key up again: a tap
    key(QEvent::KeyRelease, Qt::Key_Control, 97 + 8);
    QVERIFY(view->captured());
    QCOMPARE(keys.count(), 0); // nothing of it reached the server, the tap neither
  }
  void keyReleasedElsewhereDoesNotBlock() {
    // A key released while another item had the focus does not block the tap.
    auto *field = new KeyField(window->contentItem());
    view->capture();
    view->release();
    key(QEvent::KeyPress, Qt::Key_A, 30 + 8);
    field->forceActiveFocus();
    key(QEvent::KeyRelease, Qt::Key_A, 30 + 8); // the field takes it
    field->setFocus(false);
    QCOMPARE(window->activeFocusItem(), window->contentItem());
    key(QEvent::KeyPress, Qt::Key_Control, 97 + 8);
    key(QEvent::KeyRelease, Qt::Key_Control, 97 + 8);
    QVERIFY(view->captured());
  }
  void keyHeldAcrossFocusChangeStillBlocks() {
    // The focus going to a field and back does not turn a held chord into a tap.
    auto *field = new KeyField(window->contentItem());
    view->capture();
    view->release();
    QSignalSpy keys(view, &Console::keyInput);
    key(QEvent::KeyPress, Qt::Key_A, 30 + 8);
    field->forceActiveFocus();
    field->setFocus(false);
    QCOMPARE(window->activeFocusItem(), window->contentItem());
    key(QEvent::KeyPress, Qt::Key_Control, 97 + 8);
    key(QEvent::KeyRelease, Qt::Key_Control, 97 + 8);
    QVERIFY(!view->captured());
    key(QEvent::KeyRelease, Qt::Key_A, 30 + 8);
    key(QEvent::KeyPress, Qt::Key_Control, 97 + 8);
    key(QEvent::KeyRelease, Qt::Key_Control, 97 + 8);
    QVERIFY(view->captured());
    QCOMPARE(keys.count(), 0); // nothing reached the server
  }
  void windowFocusLossForgetsHeldKeys() {
    // Released in another window, a key cannot block the tap for good.
    view->capture();
    view->release();
    key(QEvent::KeyPress, Qt::Key_A, 30 + 8);
    QFocusEvent out(QEvent::FocusOut, Qt::ActiveWindowFocusReason);
    QCoreApplication::sendEvent(window, &out);
    QFocusEvent in(QEvent::FocusIn, Qt::ActiveWindowFocusReason);
    QCoreApplication::sendEvent(window, &in);
    QCOMPARE(window->activeFocusItem(), window->contentItem());
    key(QEvent::KeyPress, Qt::Key_Control, 97 + 8);
    key(QEvent::KeyRelease, Qt::Key_Control, 97 + 8);
    QVERIFY(view->captured());
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
