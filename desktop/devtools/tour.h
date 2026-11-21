#pragma once
#include <QSize>
#include <QString>

class Controller;
class MemoryCredentials;
class QApplication;
class QQmlApplicationEngine;

namespace devtools {
// The tour's connections, written to the test-mode profiles file before the
// controller reads it: every run starts from the same list, whatever an earlier
// run left (the tour removes them at its end). Returns the file, or "" on failure.
QString writeTourProfiles();
// Developer aid (--screenshots DIR, with --self-test): walks through the UI
// states and saves one PNG per state and window. Never touches the network: the
// connecting and error states use the demo console's "hang" and "unreachable".
// It fails loudly, with exit 5: a step that never becomes ready, a named object
// that is missing, a QML method, signal or property that does not exist, or a
// PNG that cannot be saved. QML warnings make it exit 6.
int runScreenshotTour(QApplication &app, QQmlApplicationEngine &engine, Controller &controller,
                      MemoryCredentials &credentials, const QString &dir, QSize size);
} // namespace devtools
