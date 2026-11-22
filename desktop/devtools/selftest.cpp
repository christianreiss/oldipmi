#include "devtools/selftest.h"
#include "app/exitcode.h"
#include "console.h"
#include "controller.h"
#include "devtools/qmlwarnings.h"
#include "settings.h"
#include <QApplication>
#include <QClipboard>
#include <QCoreApplication>
#include <QElapsedTimer>
#include <QEventLoop>
#include <QGuiApplication>
#include <QImage>
#include <QKeyEvent>
#include <QKeySequence>
#include <QMouseEvent>
#include <QQmlApplicationEngine>
#include <QQuickItem>
#include <QQuickWindow>
#include <QTimer>
#include <QWindow>
#include <cstdio>
#include <functional>

namespace devtools {
const QDateTime testNow = QDateTime::fromString("2026-01-15T09:41:00Z", Qt::ISODate);
QString consoleSizeChanges(QQuickWindow *w, Console *console) {
  auto settle = [w] {
    QCoreApplication::processEvents();
    w->grabWindow(); // polishes layouts and renders one frame
  };
  settle();
  const QRectF initial(console->mapToScene({0, 0}), console->size());
  QStringList changes;
  auto check = [&](const QString &step, bool captured) {
    settle();
    if (console->captured() != captured)
      changes << step + ": captured is " + (console->captured() ? "true" : "false");
    const QRectF now(console->mapToScene({0, 0}), console->size());
    if (now != initial)
      changes << QString("%1: %2x%3+%4+%5 became %6x%7+%8+%9").arg(step)
                     .arg(initial.width()).arg(initial.height()).arg(initial.x()).arg(initial.y())
                     .arg(now.width()).arg(now.height()).arg(now.x()).arg(now.y());
  };
  w->requestActivate();
  console->capture();
  check("capture", true);
  QWindow other; // takes the activation away, as leaving the window does
  other.resize(16, 16);
  other.show();
  other.requestActivate();
  check("window deactivated", false);
  w->requestActivate();
  check("window activated again", true);
  console->release();
  check("release", false);
  return changes.join("; ");
}
// Runs the event loop (timers, deferred calls, frames) until `ready` holds or
// `ms` pass: list models, delegates and menus settle a few frames later.
static bool waitFor(QQuickWindow *w, const std::function<bool()> &ready, int ms = 3000) {
  QElapsedTimer clock;
  clock.start();
  while (!ready()) {
    if (clock.hasExpired(ms))
      return false;
    QEventLoop loop;
    QTimer::singleShot(16, &loop, &QEventLoop::quit);
    loop.exec();
    w->grabWindow(); // polishes layouts and renders one frame
  }
  return true;
}
QString copyFails(QQuickWindow *w, Console *console) {
  if (QGuiApplication::platformName() != "offscreen")
    return {};
  auto *shortcut = w->findChild<QObject *>("copyScreenShortcut");
  if (!shortcut)
    return "no copy shortcut";
  if (QKeySequence(shortcut->property("sequence").toString()) != QKeySequence(Qt::CTRL | Qt::SHIFT | Qt::Key_C))
    return "the copy shortcut is not Ctrl+Shift+C";
  w->requestActivate();
  console->capture();
  if (!waitFor(w, [console] { return console->captured(); }, 1000))
    return "the console does not capture";
  const bool onWhileCaptured = shortcut->property("enabled").toBool();
  console->release();
  if (onWhileCaptured)
    return "Ctrl+Shift+C stays on while the console is captured";
  if (!waitFor(w, [shortcut] { return shortcut->property("enabled").toBool(); }, 1000))
    return "Ctrl+Shift+C is off while a console is shown and released";
  QGuiApplication::clipboard()->clear();
  QMetaObject::invokeMethod(shortcut, "activated");
  const QImage copied = QGuiApplication::clipboard()->image();
  const QSize frame = console->frameSize();
  if (copied.size() != frame)
    return QString("Ctrl+Shift+C copied a %1x%2 image of the %3x%4 screen")
        .arg(copied.width()).arg(copied.height()).arg(frame.width()).arg(frame.height());
  return {};
}
QString keyboardFails(QQuickWindow *w, Console *console, Controller &controller) {
  auto press = [w](Qt::Key key) {
    QKeyEvent event(QEvent::KeyPress, key, Qt::NoModifier);
    QCoreApplication::sendEvent(w, &event);
  };
  auto *chip = w->findChild<QQuickItem *>("captureButton");
  if (!chip)
    return "no capture chip in the status strip";
  console->release();
  w->requestActivate();
  chip->forceActiveFocus(Qt::TabFocusReason);
  if (!waitFor(w, [chip] { return chip->hasActiveFocus(); }))
    return "the capture chip does not take the keyboard focus";
  press(Qt::Key_Return);
  const bool captured = console->captured();
  console->release();
  if (!captured)
    return "Enter on the capture chip does not capture the console";

  auto *list = w->findChild<QQuickItem *>("connectionList");
  if (!list)
    return "no connection list";
  // Examples for the check, removed again; the test-mode profiles may be fewer
  // than two (the screenshot tour removes them).
  QStringList added;
  auto add = [&](int n) {
    added << controller.saveHost({{"name", QString("Keyboard check %1").arg(n)},
                                  {"host", QString("keys-%1.example.net").arg(n)}, {"family", "demo"}}, {}, false)
                 .value("id").toString();
  };
  for (int i = controller.hosts().size(); i < 2; ++i)
    add(i + 1);
  auto index = [list] { return list->property("currentIndex").toInt(); };
  auto selected = [w] { return w->property("selectedId").toString(); };
  const QString why = [&]() -> QString {
    if (!waitFor(w, [list] { return list->property("count").toInt() >= 2; }))
      return "the connection list does not show the profiles";
    list->forceActiveFocus(Qt::TabFocusReason);
    if (!waitFor(w, [&] { return list->hasActiveFocus() && index() >= 0; }))
      return list->hasActiveFocus() ? "the focused connection list has no current row"
                                    : "the connection list does not take the keyboard focus";
    // A row other than the first: ListView resets to the first row on every model change.
    const int last = list->property("count").toInt() - 1;
    list->setProperty("currentIndex", last);
    QString chosen;
    if (!waitFor(w, [&] { chosen = selected(); return index() == last && !chosen.isEmpty(); }))
      return "moving through the connection list does not select the row";
    // Saving a profile rebuilds the list while it has the focus.
    add(9);
    waitFor(w, [&] { return list->property("count").toInt() == last + 2; }, 1000);
    waitFor(w, [] { return false; }, 200);
    if (selected() != chosen)
      return "rebuilding the connection list changes the selected connection";
    press(Qt::Key_Menu);
    // Delegates are not QObject children of the list: walk the item tree and
    // look at the objects each item holds.
    QObject *menu = nullptr;
    std::function<void(QQuickItem *)> find = [&](QQuickItem *item) {
      for (auto *o : item->children())
        if (o->inherits("QQuickMenu") && o->property("visible").toBool())
          menu = o;
      for (auto *child : item->childItems())
        find(child);
    };
    waitFor(w, [&] { find(list); return menu != nullptr; }, 1000);
    if (!menu)
      return "the menu key does not open the connection's context menu";
    QMetaObject::invokeMethod(menu, "close");
    return {};
  }();
  for (const QString &id : added)
    controller.removeHost(id);
  waitFor(w, [] { return false; }, 100);
  return why;
}
QString fullScreenFails(QQuickWindow *w, Console *console, Controller &controller) {
  auto *corner = w->findChild<QQuickItem *>("quitCorner");
  auto *header = w->findChild<QQuickItem *>("headerQuitButton");
  auto *bar = w->findChild<QQuickItem *>("fullscreenBar");
  auto *take = w->findChild<QQuickItem *>("fullscreenTakeControl");
  auto *peek = w->findChild<QObject *>("fullscreenPeek");
  auto *note = w->findChild<QObject *>("fullscreenNote");
  if (!corner || !header)
    return "no Quit button";
  if (!bar || !take || !peek || !note)
    return "no full-screen bar";
  auto press = [w](Qt::Key key) {
    QKeyEvent down(QEvent::KeyPress, key, Qt::NoModifier);
    QCoreApplication::sendEvent(w, &down);
    QKeyEvent up(QEvent::KeyRelease, key, Qt::NoModifier);
    QCoreApplication::sendEvent(w, &up);
  };
  auto inBar = [w, bar, corner] {
    for (auto *item = w->activeFocusItem(); item; item = item->parentItem())
      if (item == bar || item == corner)
        return true;
    return false;
  };
  // A console shown (the keyboard check ends with a closed connection selected).
  controller.preview();
  if (!waitFor(w, [&] { return controller.hasFrame(); }))
    return "the preview console does not show";
  QMetaObject::invokeMethod(w, "toggleFullscreen");
  const QString why = [&]() -> QString {
    if (!waitFor(w, [&] { return w->visibility() == QWindow::FullScreen && corner->isVisible(); }))
      return QString("full screen with a console does not show Quit in the corner "
                     "(window %1, console %2, corner %3)")
          .arg(w->visibility() == QWindow::FullScreen ? "full screen" : "not full screen",
               controller.hasFrame() ? "shown" : "not shown", corner->isVisible() ? "visible" : "hidden");
    console->release();
    w->requestActivate();
    press(Qt::Key_Tab);
    if (!waitFor(w, [&] { return inBar() && bar->isVisible(); }, 1000))
      return QString("Tab, released, does not reach the full-screen bar or Quit (the focus is on %1)")
          .arg(w->activeFocusItem() ? w->activeFocusItem()->metaObject()->className() : "nothing");
    // From there a Right Ctrl tap does not take control, so the note does not offer it.
    if (!note->property("how").toString().startsWith("Take control"))
      return QString("released, with the focus in the full-screen bar, its note says \"%1\"")
          .arg(note->property("how").toString());
    // Past the first seconds, when the bar shows anyway, and with the pointer away
    // from the top edge, which shows it too (the offscreen platform may report one
    // there); then past its fade.
    waitFor(w, [peek] { return !peek->property("running").toBool(); }, 4000);
    const QPointF middle(w->width() / 2.0, w->height() / 2.0);
    QMouseEvent move(QEvent::MouseMove, middle, w->mapToGlobal(middle), Qt::NoButton, Qt::NoButton, Qt::NoModifier);
    QCoreApplication::sendEvent(w, &move);
    waitFor(w, [] { return false; }, 500);
    if (!inBar() || !bar->isVisible())
      return "the full-screen bar hides while it has the keyboard focus";
    // On through the bar with Tab to Take control, and Enter there.
    for (int i = 0; i < 12 && !take->hasActiveFocus(); ++i) {
      press(Qt::Key_Tab);
      waitFor(w, [] { return false; }, 50);
    }
    if (!take->hasActiveFocus())
      return "Tab does not reach Take control in the full-screen bar";
    press(Qt::Key_Return);
    if (!waitFor(w, [&] { return console->captured(); }, 1000))
      return "Enter on Take control in the full-screen bar does not capture the console";
    console->release();
    if (!waitFor(w, [&] { return !bar->isVisible(); }, 1500))
      return "the full-screen bar stays without the keyboard focus";
    controller.disconnectAll();
    if (!waitFor(w, [&] { return !controller.hasFrame() && header->isVisible(); }))
      return "full screen without a console does not show Quit";
    return {};
  }();
  if (w->visibility() == QWindow::FullScreen)
    QMetaObject::invokeMethod(w, "toggleFullscreen");
  waitFor(w, [w] { return w->visibility() != QWindow::FullScreen; }, 1000);
  return why;
}
QString unknownFamilyFails(QQuickWindow *w) {
  auto *dialog = w->findChild<QObject *>("connectionDialog");
  if (!dialog)
    return "no connection dialog";
  const QVariantMap stored{{"name", "Future"}, {"host", "bmc.example.net"}, {"family", "fixture"},
                           {"username", "admin"}, {"consoleMode", "java"}, {"jnlpPath", "/kvm.jnlp"}};
  QVariant saved;
  const bool called = QMetaObject::invokeMethod(dialog, "openFor", Q_ARG(QVariant, stored)) &&
                      QMetaObject::invokeMethod(dialog, "profile", Q_RETURN_ARG(QVariant, saved));
  const int tile = dialog->property("familyIndex").toInt();
  QMetaObject::invokeMethod(dialog, "close");
  if (!waitFor(w, [dialog] { return !dialog->property("visible").toBool(); }, 2000))
    return "the dialog does not close";
  const auto p = saved.toMap();
  if (!called)
    return "cannot open the dialog for a connection";
  if (tile != -1)
    return QString("tile %1 is selected").arg(tile);
  if (p.value("family") != "fixture")
    return "it saves the family as " + p.value("family").toString();
  if (p.value("consoleMode") != "java" || p.value("jnlpPath") != "/kvm.jnlp")
    return "it drops the stored console options";
  return {};
}
QString fitFails(QQuickWindow *w, Console *console, AppSettings &settings) {
  auto *action = w->findChild<QObject *>("fitAction");
  if (!action)
    return "no fit action";
  const bool before = settings.fitToWindow();
  QString why;
  QMetaObject::invokeMethod(action, "trigger");
  if (settings.fitToWindow() == before)
    why = "the button does not switch the setting";
  else if (!waitFor(w, [&] { return console->fit() != before; }, 1000))
    why = "the console does not follow the button";
  else {
    settings.setFitToWindow(before); // as the switch in Settings does
    if (!waitFor(w, [&] { return console->fit() == before; }, 1000))
      why = "after the button, the console no longer follows the setting";
  }
  settings.setFitToWindow(before);
  return why;
}
void startSelfTest(QApplication &app, QQmlApplicationEngine &engine, Console *console, Controller &controller,
                   AppSettings &settings) {
  QTimer::singleShot(600, &app, [&app, &engine, console, &controller, &settings] {
    auto w = qobject_cast<QQuickWindow *>(engine.rootObjects().first());
    if (!w || w->grabWindow().isNull())
      app.exit(code(ExitCode::NoScreenshot));
    else if (QString why = consoleSizeChanges(w, console); !why.isEmpty()) {
      fprintf(stderr, "Console view changed size: %s\n", qPrintable(why));
      app.exit(code(ExitCode::CheckFailed));
    } else if (QString why = copyFails(w, console); !why.isEmpty()) {
      fprintf(stderr, "Copy: %s\n", qPrintable(why));
      app.exit(code(ExitCode::CheckFailed));
    } else if (QString why = keyboardFails(w, console, controller); !why.isEmpty()) {
      fprintf(stderr, "Keyboard: %s\n", qPrintable(why));
      app.exit(code(ExitCode::CheckFailed));
    } else if (QString why = fullScreenFails(w, console, controller); !why.isEmpty()) {
      fprintf(stderr, "Full screen: %s\n", qPrintable(why));
      app.exit(code(ExitCode::CheckFailed));
    } else if (QString why = unknownFamilyFails(w); !why.isEmpty()) {
      fprintf(stderr, "Unknown family: %s\n", qPrintable(why));
      app.exit(code(ExitCode::CheckFailed));
    } else if (QString why = fitFails(w, console, settings); !why.isEmpty()) {
      fprintf(stderr, "Fit: %s\n", qPrintable(why));
      app.exit(code(ExitCode::CheckFailed));
    } else {
      QString path = qEnvironmentVariable("OLDIPMI_TEST_SCREENSHOT");
      if (!path.isEmpty())
        w->grabWindow().save(path);
      app.exit(code(qmlWarningsResult()));
    }
  });
}
} // namespace devtools
