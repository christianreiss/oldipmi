#pragma once
#include <QDateTime>
#include <QString>

class AppSettings;
class Console;
class Controller;
class QApplication;
class QQmlApplicationEngine;
class QQuickWindow;

// The checks of --self-test, which runs them on the demo console.
namespace devtools {
// The self-test's clock: every "elapsed" and "last connected" label reads the
// same in every run (the QML gets it as testClock; 0 is the real time).
extern const QDateTime testNow;
// The console keeps its size when it takes or gives back the keyboard, and when
// the window is deactivated and activated again (focus follows mouse does that
// whenever the pointer leaves and re-enters): any change rescales the remote
// screen, which then wobbles under the pointer. Returns what changed, or "".
QString consoleSizeChanges(QQuickWindow *w, Console *console);
// Released, Ctrl+Shift+C puts the remote screen on the clipboard at its own size;
// captured, the shortcut is off, since every key goes to the server. Only on the
// offscreen platform (CTest): on a desktop it would replace the user's clipboard.
// Returns what failed, or "".
QString copyFails(QQuickWindow *w, Console *console);
// Without a mouse: Tab reaches the capture chip of the status strip, and Enter
// there gives keyboard and mouse to the server. In the connection list the focus
// lands on a row, rebuilding the list keeps the selected row, and the menu key
// opens the row's context menu. Returns what failed, or "".
QString keyboardFails(QQuickWindow *w, Console *console, Controller &controller);
// Quit stays visible in full screen: in its corner while a console is shown, and
// in the header, with the connection list, when there is none (as after losing
// the connection). Released, Tab brings the keyboard focus to the floating bar or
// Quit: the bar shows and stays while it has the focus, the release note points to
// its Take control button, and Enter there gives keyboard and mouse to the server.
// Returns what failed, or "".
QString fullScreenFails(QQuickWindow *w, Console *console, Controller &controller);
// A connection of a family this version does not know (from a newer OldIPMI, say)
// keeps it: the dialog selects no tile and saves the stored family and console
// options. Returns what failed, or "".
QString unknownFamilyFails(QQuickWindow *w);
// The fit button switches the "scale to the window" setting, and the console
// follows that setting, also when it changes in Settings after the button was
// used. The setting is as before afterwards. Returns what failed, or "".
QString fitFails(QQuickWindow *w, Console *console, AppSettings &settings);
// --self-test: 600 ms after the start, checks that the window renders and runs
// the checks above, then makes the application exit with the first failure, or
// with qmlWarningsResult() when they all pass.
void startSelfTest(QApplication &app, QQmlApplicationEngine &engine, Console *console, Controller &controller,
                   AppSettings &settings);
} // namespace devtools
