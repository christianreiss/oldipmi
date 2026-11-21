// The key table (keymap.h): the HID usage the BMC gets for a key, by its XKB
// keycode (evdev code + 8), else by its Qt::Key.
#include "keymap.h"
#include <QtTest>

class KeymapTest : public QObject {
  Q_OBJECT
  // An XKB keycode, from the evdev code (linux/input-event-codes.h).
  static quint32 xkb(int evdev) { return quint32(evdev + 8); }
private slots:
  void byKeycode_data() {
    QTest::addColumn<int>("evdev");
    QTest::addColumn<int>("usage");
    QTest::newRow("Escape") << 1 << 41;
    QTest::newRow("1") << 2 << 30;
    QTest::newRow("0") << 11 << 39;
    QTest::newRow("Backspace") << 14 << 42;
    QTest::newRow("Tab") << 15 << 43;
    QTest::newRow("Q") << 16 << 20;
    QTest::newRow("Enter") << 28 << 40;
    QTest::newRow("A") << 30 << 4;
    QTest::newRow("Z") << 44 << 29;
    QTest::newRow("Space") << 57 << 44;
    QTest::newRow("Caps Lock") << 58 << 57;
    QTest::newRow("F1") << 59 << 58;
    QTest::newRow("F10") << 68 << 67;
    QTest::newRow("F11") << 87 << 68;
    QTest::newRow("F12") << 88 << 69;
    QTest::newRow("Keypad 7") << 71 << 95;
    QTest::newRow("Keypad Enter") << 96 << 88;
    QTest::newRow("Keypad /") << 98 << 84;
    QTest::newRow("ISO key left of Z") << 86 << 100;
    QTest::newRow("Left Ctrl") << 29 << 224;
    QTest::newRow("Left Shift") << 42 << 225;
    QTest::newRow("Left Alt") << 56 << 226;
    QTest::newRow("Left Meta") << 125 << 227;
    QTest::newRow("Right Ctrl") << 97 << 228;
    QTest::newRow("Right Shift") << 54 << 229;
    QTest::newRow("Right Alt") << 100 << 230;
    QTest::newRow("Right Meta") << 126 << 231;
    QTest::newRow("Print") << 99 << 70;
    QTest::newRow("Pause") << 119 << 72;
    QTest::newRow("Menu") << 127 << 101;
    QTest::newRow("Insert") << 110 << 73;
    QTest::newRow("Home") << 102 << 74;
    QTest::newRow("Page Up") << 104 << 75;
    QTest::newRow("Delete") << 111 << 76;
    QTest::newRow("End") << 107 << 77;
    QTest::newRow("Page Down") << 109 << 78;
    QTest::newRow("Right") << 106 << 79;
    QTest::newRow("Left") << 105 << 80;
    QTest::newRow("Down") << 108 << 81;
    QTest::newRow("Up") << 103 << 82;
  }
  // The keycode decides, whatever the layout makes of the key: a German Z
  // (Qt::Key_Z on the evdev Y key) is still the usage of the US Y.
  void byKeycode() {
    QFETCH(int, evdev);
    QFETCH(int, usage);
    QCOMPARE(hidUsage(xkb(evdev), Qt::Key_unknown), usage);
    QCOMPARE(hidUsage(xkb(evdev), Qt::Key_Z), usage);
  }

  // A generated event has no keycode; then the Qt key decides.
  void byQtKey_data() {
    QTest::addColumn<int>("key");
    QTest::addColumn<int>("usage");
    QTest::newRow("A") << int(Qt::Key_A) << 4;
    QTest::newRow("Z") << int(Qt::Key_Z) << 29;
    QTest::newRow("1") << int(Qt::Key_1) << 30;
    QTest::newRow("9") << int(Qt::Key_9) << 38;
    QTest::newRow("0") << int(Qt::Key_0) << 39;
    QTest::newRow("Return") << int(Qt::Key_Return) << 40;
    QTest::newRow("Escape") << int(Qt::Key_Escape) << 41;
    QTest::newRow("Backspace") << int(Qt::Key_Backspace) << 42;
    QTest::newRow("Tab") << int(Qt::Key_Tab) << 43;
    QTest::newRow("Space") << int(Qt::Key_Space) << 44;
    QTest::newRow("Control") << int(Qt::Key_Control) << 224;
    QTest::newRow("Shift") << int(Qt::Key_Shift) << 225;
    QTest::newRow("Alt") << int(Qt::Key_Alt) << 226;
    QTest::newRow("Meta") << int(Qt::Key_Meta) << 227;
    QTest::newRow("Delete") << int(Qt::Key_Delete) << 76;
  }
  void byQtKey() {
    QFETCH(int, key);
    QFETCH(int, usage);
    QCOMPARE(hidUsage(0, key), usage);
  }

  // A keycode the table does not know leaves the Qt key to decide.
  void unknownKeycodeFallsBackToTheQtKey() {
    QCOMPARE(hidUsage(xkb(84), Qt::Key_A), 4);
    QCOMPARE(hidUsage(xkb(200), Qt::Key_Escape), 41);
  }

  // A key with neither gets 0, which the console does not send.
  void unknownKeyHasNone_data() {
    QTest::addColumn<quint32>("scan");
    QTest::addColumn<int>("key");
    QTest::newRow("no keycode, F13") << quint32(0) << int(Qt::Key_F13);
    QTest::newRow("unmapped keycode") << xkb(84) << int(Qt::Key_unknown);
    QTest::newRow("keycode past the table") << xkb(200) << int(Qt::Key_unknown);
  }
  void unknownKeyHasNone() {
    QFETCH(quint32, scan);
    QFETCH(int, key);
    QCOMPARE(hidUsage(scan, key), 0);
  }
};
QTEST_GUILESS_MAIN(KeymapTest)
#include "keymap_test.moc"
