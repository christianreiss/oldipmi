#include "keymap.h"
#include <Qt>

int hidUsage(quint32 nativeScanCode, int qtKey) {
  // XKB keycodes used by Qt on both xcb and Wayland are evdev keycode + 8.
  static const int hid[] = {
      0,  41, 30, 31, 32, 33, 34, 35, 36, 37,  38, 39,  45,  46, 42,
      43, 20, 26, 8,  21, 23, 28, 24, 12, 18,  19, 47,  48,  40, 224,
      4,  22, 7,  9,  10, 11, 13, 14, 15, 51,  52, 53,  225, 49, 29,
      27, 6,  25, 5,  17, 16, 54, 55, 56, 229, 85, 226, 44,  57, 58,
      59, 60, 61, 62, 63, 64, 65, 66, 67, 83,  71, 95,  96,  97, 86,
      92, 93, 94, 87, 89, 90, 91, 98, 99, 0,   0,  100, 68,  69};
  int scan = int(nativeScanCode) - 8;
  if (scan > 0 && scan < int(sizeof(hid) / sizeof(int)) && hid[scan])
    return hid[scan];
  switch (scan) {
  case 96:
    return 88;
  case 97:
    return 228;
  case 98:
    return 84;
  case 99:
    return 70;
  case 100:
    return 230;
  case 102:
    return 74;
  case 103:
    return 82;
  case 104:
    return 75;
  case 105:
    return 80;
  case 106:
    return 79;
  case 107:
    return 77;
  case 108:
    return 81;
  case 109:
    return 78;
  case 110:
    return 73;
  case 111:
    return 76;
  case 119:
    return 72;
  case 125:
    return 227;
  case 126:
    return 231;
  case 127:
    return 101;
  }
  if (qtKey >= Qt::Key_A && qtKey <= Qt::Key_Z)
    return qtKey - Qt::Key_A + 4;
  if (qtKey >= Qt::Key_1 && qtKey <= Qt::Key_9)
    return qtKey - Qt::Key_1 + 30;
  switch (qtKey) {
  case Qt::Key_0:
    return 39;
  case Qt::Key_Return:
    return 40;
  case Qt::Key_Escape:
    return 41;
  case Qt::Key_Backspace:
    return 42;
  case Qt::Key_Tab:
    return 43;
  case Qt::Key_Space:
    return 44;
  case Qt::Key_Control:
    return 224;
  case Qt::Key_Shift:
    return 225;
  case Qt::Key_Alt:
    return 226;
  case Qt::Key_Meta:
    return 227;
  case Qt::Key_Delete:
    return 76;
  default:
    return 0;
  }
}
