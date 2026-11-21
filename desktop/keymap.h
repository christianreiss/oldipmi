#pragma once
#include <QtGlobal>

// The HID usage of a key, on the Keyboard/Keypad page (0x07) of the HID
// Usage Tables, which the BMC's virtual keyboard takes. The key's XKB
// keycode decides, whatever the layout makes of the key: Qt reports it as
// the native scan code on both xcb and Wayland (the evdev code plus 8). For
// an event without a keycode the table knows, the Qt::Key decides. 0 for a
// key that has none.
int hidUsage(quint32 nativeScanCode, int qtKey);
