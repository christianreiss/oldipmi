#include "agentapi.h"
#include "console.h"
#include "controller.h"
#include "framethumbnail.h"
#include "i18n.h"
#include "settings.h"
#include "singleinstance.h"
#include <QCursor>
#include <QIcon>
#include <algorithm>
#include <cstring>
#include <QApplication>
#include <QElapsedTimer>
#include <QEventLoop>
#include <QFile>
#include <QJsonDocument>
#include <QJsonObject>
#include <QKeyEvent>
#include <QMouseEvent>
#include <QQmlApplicationEngine>
#include <QQmlContext>
#include <QQuickWindow>
#include <QStandardPaths>
#include <QTimer>
#include <QWindow>
#include <functional>
#include <memory>
// --self-test and its screenshot tour fail on any warning from OldIPMI's own QML
// (binding loops, type errors, unresolved names), so every UI state the tour
// visits is also checked for them.
static QStringList s_qmlWarnings;
static QtMessageHandler s_previousHandler = nullptr;
static void recordQmlWarning(QtMsgType type, const QMessageLogContext &context, const QString &message) {
  const char *qml = "qrc:/desktop/qml/";
  if (type != QtDebugMsg && type != QtInfoMsg &&
      (message.contains(QLatin1String(qml)) || (context.file && !strncmp(context.file, qml, strlen(qml)))))
    s_qmlWarnings << message;
  if (s_previousHandler)
    s_previousHandler(type, context, message);
  else
    fprintf(stderr, "%s\n", qPrintable(qFormatLogMessage(type, context, message)));
}
static int qmlWarningsResult() {
  if (s_qmlWarnings.isEmpty())
    return 0;
  fprintf(stderr, "%lld QML warning(s):\n%s\n", static_cast<long long>(s_qmlWarnings.size()),
          qPrintable(s_qmlWarnings.join('\n')));
  return 6;
}
// Developer aid (--screenshots DIR, with --self-test): walks through the UI
// states and saves one PNG per state and window. Never contacts a real BMC:
// the error state uses the reserved name "oldipmi.invalid".
static void call0(QObject *root, const char *method, const QVariant &arg) {
  QMetaObject::invokeMethod(root, method, Q_ARG(QVariant, arg));
}
static int runScreenshotTour(QApplication &app, QQmlApplicationEngine &engine,
                             Controller &controller, const QString &dir, QSize size) {
  auto *root = engine.rootObjects().first();
  // Optional --size WxH: every grabbed window (main, dialogs' host, settings)
  // gets the same logical size, so review tools can compare a fixed matrix.
  auto fit = [size] {
    if (size.isEmpty())
      return;
    for (auto *w : QGuiApplication::topLevelWindows())
      if (w->isVisible() && w->size() != size) w->resize(size);
  };
  fit();
  // Offscreen Qt puts the pointer at the window origin, which renders false
  // hover states (tooltips); park it outside every window.
  QCursor::setPos(-10000, -10000);
  // A previous tour removed all test-mode profiles; seed examples again.
  if (controller.hosts().isEmpty()) {
    controller.saveHost({{"name", "Lab-AMI"}, {"host", "lab-ami.example.net"}, {"family", "ami"}, {"username", "ADMIN"}}, {}, false);
    controller.saveHost({{"name", "Lab-ATEN"}, {"host", "lab-aten.example.net"}, {"family", "aten"}, {"username", "ADMIN"}}, {}, false);
    controller.saveHost({{"name", "Lab-Avocent"}, {"host", "lab-avocent.example.net"}, {"family", "avocent"}, {"username", "root"}, {"legacyTLS", true}}, {}, false);
    call0(root, "loadHost", controller.hosts().first());
  }
  // `wait` ms after `act`, the step is grabbed as soon as `ready` (if given) holds,
  // or 6 s later at the latest: a busy machine must not change what is captured.
  struct Step { QString name; std::function<void()> act; int wait; std::function<bool()> ready = {}; };
  auto call = [root](const char *method, QVariant arg = {}) {
    if (arg.isValid())
      QMetaObject::invokeMethod(root, method, Q_ARG(QVariant, arg));
    else
      QMetaObject::invokeMethod(root, method);
  };
  auto closePopups = [root] {
    for (auto *o : root->findChildren<QObject *>())
      if (o->inherits("QQuickPopup") && o->property("opened").toBool())
        QMetaObject::invokeMethod(o, "close");
  };
  auto *window = qobject_cast<QQuickWindow *>(root);
  auto console = [root] { return root->findChild<Console *>("console"); };
  auto popup = [root](const char *name) { return root->findChild<QObject *>(name); };
  auto popupOpen = [popup](const char *name) {
    auto *p = popup(name);
    return p && p->property("opened").toBool();
  };
  QVariantMap demo{{"name", "Demo BMC"}, {"host", "oldipmi.invalid"}, {"family", "aten"},
                   {"username", "ADMIN"}, {"remember", false}};
  // A private address with no host behind it: the login stays pending until cancelled.
  QVariantMap pending{{"name", "Lab server"}, {"host", "10.255.255.1"}, {"family", "ami"},
                      {"username", "ADMIN"}, {"remember", false}};
  auto failed = std::make_shared<bool>(false); // a check along the tour failed: exit 5
  auto steps = std::make_shared<QList<Step>>(QList<Step>{
      {"01-selected", [] {}, 600},
      {"02-new-connection", [=] { call("newConnection"); }, 700, [=] { return popupOpen("connectionDialog"); }},
      {"03-edit-connection", [=, &controller] {
         closePopups();
         auto p = controller.hosts().first().toMap();
         call("editConnection", p);
       }, 700, [=] { return popupOpen("connectionDialog"); }},
      {"04-settings-general", [=] { closePopups(); call("openSettings", QString("general")); }, 900},
      {"05-settings-agents", [=] { call("openSettings", QString("agents")); }, 900},
      {"06-connecting", [=, &controller] {
         for (auto *w : QGuiApplication::topLevelWindows())
           if (w != qobject_cast<QWindow *>(root)) w->close();
         controller.connectHost(pending, "not-a-real-password");
       }, 800, [&controller] { return controller.state() == "logging_in"; }},
      {"07-error", [=, &controller] {
         controller.disconnectHost();
         controller.connectHost(demo, "not-a-real-password");
       }, 600, [&controller] { return !controller.errorStage().isEmpty(); }},
      {"08-console", [=, &controller] {
         controller.disconnectHost();
         controller.connectHost(QVariantMap{{"name", "Demo console"}, {"host", "demo"}, {"family", "demo"}}, {});
         if (auto *d = root->findChild<QObject *>("consoleDetails")) d->setProperty("checked", true);
       }, 900, [&controller] { return controller.hasFrame(); }},
      {"09-power-confirm", [=] { root->setProperty("pendingPower", "reset");
         if (auto *d = root->findChild<QObject *>("powerDialog")) QMetaObject::invokeMethod(d, "open"); },
       700, [=] { return popupOpen("powerDialog"); }},
      {"10-switch", [=] {
         // Connecting another profile opens it next to the first; both stay connected.
         closePopups();
         call("connectProfile", QVariantMap{{"name", "Demo wide"}, {"host", "demo-1920x1080"}, {"family", "demo"}});
       }, 900, [&controller] { return controller.hasFrame() && controller.openCount() == 2; }},
      // Keyboard and mouse go to the server: the accent ring and the status strip.
      {"10a-captured", [=] {
         window->requestActivate();
         if (auto *c = console()) c->capture();
       }, 500, [=] { auto *c = console(); return c && c->captured(); }},
      // Full screen: the floating bar (shown for a moment after entering) and the
      // note that keyboard and mouse go to the server.
      {"10b-fullscreen", [=, &app] {
         call("toggleFullscreen");
         QTimer::singleShot(250, &app, [=] {
           if (auto *c = console()) { c->release(); window->requestActivate(); c->capture(); }
         });
       }, 800, [=] { auto *c = console(); return window->visibility() == QWindow::FullScreen && c && c->captured(); }},
      // Released in full screen: the note says how to take control again.
      {"10b1-fullscreen-released", [=] { if (auto *c = console()) c->release(); },
       700, [=] { auto *c = console(); return window->visibility() == QWindow::FullScreen && c && !c->captured(); }},
      // Full screen without a console (a closed connection selected, as after
      // losing one): the header with Quit and the connection list stay.
      {"10b2-fullscreen-idle", [=, &controller] { call("loadHost", controller.hosts().first()); },
       700, [=, &controller] { return window->visibility() == QWindow::FullScreen && !controller.hasFrame(); }},
      {"10c-delete-confirm", [=, &controller] {
         if (auto *c = console()) c->release();
         if (window->visibility() == QWindow::FullScreen)
           call("toggleFullscreen");
         call("deleteConnection", controller.hosts().first());
       }, 700, [=] { return popupOpen("deleteDialog") && window->visibility() != QWindow::FullScreen; }},
      {"10d-password", [=, &controller] {
         closePopups();
         if (auto *d = popup("credentialsDialog")) {
           d->setProperty("profile", controller.hosts().first());
           QMetaObject::invokeMethod(d, "open");
         }
       }, 700, [=] { return popupOpen("credentialsDialog"); }},
      // Editing a profile with security exceptions opens the advanced options;
      // scrolled to them, so a small window shows the controls themselves.
      {"10e-advanced", [=, &controller] {
         closePopups();
         for (const auto &h : controller.hosts())
           if (h.toMap().value("legacyTLS").toBool()) { call("editConnection", h); break; }
         QTimer::singleShot(300, popup("connectionDialog"), [=] {
           QMetaObject::invokeMethod(popup("connectionDialog"), "revealAdvanced");
         });
       }, 700, [=] { return popupOpen("connectionDialog"); }},
      // Two consoles are open, so Disconnect all asks first and names them.
      {"10f-disconnect-all", [=] {
         closePopups();
         call("disconnectAll");
       }, 700, [=] { return popupOpen("disconnectAllDialog"); }},
      // Confirms the Disconnect all dialog: it closes exactly the consoles it names.
      {"11-empty", [=, &controller] {
         if (auto *confirm = popup("disconnectAllConfirm"))
           QMetaObject::invokeMethod(confirm, "clicked");
         if (controller.openCount()) {
           fprintf(stderr, "Screenshot tour: Disconnect all left %d console(s) open\n", controller.openCount());
           *failed = true;
         }
         closePopups();
         controller.disconnectAll();
         for (const auto &h : controller.hosts()) controller.removeHost(h.toMap().value("id").toString());
         call("loadHost", QVariantMap{});
       }, 600},
  });
  auto index = std::make_shared<int>(0);
  auto next = std::make_shared<std::function<void()>>();
  auto settle = std::make_shared<std::function<void(int)>>();
  *settle = [=, &app](int waited) {
    const auto &step = (*steps)[*index - 1];
    if (step.ready && !step.ready() && waited < 6000) {
      QTimer::singleShot(100, &app, [=] { (*settle)(waited + 100); });
      return;
    }
    if (step.ready && !step.ready())
      fprintf(stderr, "Screenshot tour: %s did not become ready\n", qPrintable(step.name));
    (*next)();
  };
  *next = [=, &app] {
    if (*index > 0) {
      const auto &done = (*steps)[*index - 1];
      int n = 0;
      for (auto *w : QGuiApplication::topLevelWindows())
        if (auto *qw = qobject_cast<QQuickWindow *>(w); qw && qw->isVisible())
          qw->grabWindow().save(dir + "/" + done.name + (n++ ? "-" + QString::number(n) : "") + ".png");
    }
    if (*index >= steps->size()) { app.exit(*failed ? 5 : qmlWarningsResult()); return; }
    auto &step = (*steps)[(*index)++];
    step.act();
    QTimer::singleShot(qMin(300, step.wait / 2), &app, fit);
    QTimer::singleShot(step.wait, &app, [=] { (*settle)(0); });
  };
  QTimer::singleShot(300, &app, *next);
  return app.exec();
}
// The console keeps its size when it takes or gives back the keyboard, and when
// the window is deactivated and activated again (focus follows mouse does that
// whenever the pointer leaves and re-enters): any change rescales the remote
// screen, which then wobbles under the pointer. Returns what changed, or "".
static QString consoleSizeChanges(QQuickWindow *w, Console *console) {
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
// Without a mouse: Tab reaches the capture chip of the status strip, and Enter
// there gives keyboard and mouse to the server. In the connection list the focus
// lands on a row, rebuilding the list keeps the selected row, and the menu key
// opens the row's context menu. Returns what failed, or "".
static QString keyboardFails(QQuickWindow *w, Console *console, Controller &controller) {
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
// Quit stays visible in full screen: in its corner while a console is shown, and
// in the header, with the connection list, when there is none (as after losing
// the connection). Released, Tab brings the keyboard focus to the floating bar or
// Quit: the bar shows and stays while it has the focus, and Enter on its Take
// control button gives keyboard and mouse to the server. Returns what failed, or "".
static QString fullScreenFails(QQuickWindow *w, Console *console, Controller &controller) {
  auto *corner = w->findChild<QQuickItem *>("quitCorner");
  auto *header = w->findChild<QQuickItem *>("headerQuitButton");
  auto *bar = w->findChild<QQuickItem *>("fullscreenBar");
  auto *take = w->findChild<QQuickItem *>("fullscreenTakeControl");
  auto *peek = w->findChild<QObject *>("fullscreenPeek");
  if (!corner || !header)
    return "no Quit button";
  if (!bar || !take || !peek)
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
int main(int argc, char **argv) {
  // Test mode must be on before QApplication: the platform theme reads
  // kdeglobals (colours, icon theme, fonts) during construction.
  bool test = std::any_of(argv + 1, argv + argc, [](const char *a) { return !strcmp(a, "--self-test"); });
  if (test)
    QStandardPaths::setTestModeEnabled(true);
  QApplication app(argc, argv);
  if (test)
    s_previousHandler = qInstallMessageHandler(recordQmlWarning);
  app.setApplicationName("OldIPMI");
  app.setOrganizationName("OldIPMI");
  app.setApplicationVersion(OLDIPMI_VERSION);
  app.setApplicationDisplayName("OldIPMI");
  app.setDesktopFileName("net.alpha_labs.OldIPMI");
  app.setWindowIcon(QIcon::fromTheme("oldipmi", QIcon(":/oldipmi.svg")));
  // One instance per user session; a second start shows the running window.
  // The self-test runs with isolated test paths and stays independent.
  SingleInstance instance;
  if (!test && !instance.acquire(qgetenv("XDG_ACTIVATION_TOKEN"))) {
    fprintf(stderr, "OldIPMI is already running; showing its window.\n");
    return 0;
  }
  bool preview = app.arguments().contains("--preview");
  qmlRegisterType<Console>("OldIPMI", 1, 0, "ConsoleView");
  qmlRegisterType<FrameThumbnail>("OldIPMI", 1, 0, "FrameThumbnail");
  Controller controller;
  FrameThumbnail::setController(&controller);
  AppSettings settings;
  AgentApi agent(&controller);
  // Auth-less local control socket for oldipmi-mcp; skipped by self-test and
  // --no-agent-api, and switchable at runtime in Settings → AI agents.
  bool agentAllowed = !test && !app.arguments().contains("--no-agent-api");
  auto applyAgentSetting = [&] {
    if (!agentAllowed || settings.agentApiEnabled() == agent.enabled())
      return;
    agent.setEnabled(settings.agentApiEnabled());
    if (agent.enabled() && !agent.listening())
      fprintf(stderr, "Agent API unavailable at %s: %s\n", qPrintable(agent.socketPath()),
              qPrintable(agent.errorString()));
  };
  applyAgentSetting();
  QObject::connect(&settings, &AppSettings::changed, &agent, applyAgentSetting);
  QQmlApplicationEngine engine;
  LocalizedContext i18n;
  engine.rootContext()->setContextObject(&i18n);
  engine.rootContext()->setContextProperty("controller", &controller);
  // False for the whole session with --no-agent-api or --self-test: Settings then
  // says that the agent switch applies from the next start.
  engine.rootContext()->setContextProperty("agentAllowed", agentAllowed);
  engine.rootContext()->setContextProperty("agent", &agent);
  engine.rootContext()->setContextProperty("settings", &settings);
  engine.load(QUrl("qrc:/desktop/qml/Main.qml"));
  if (engine.rootObjects().isEmpty())
    return 1;
  engine.rootContext()->setContextProperty("mainWindow", engine.rootObjects().first());
  QObject::connect(&instance, &SingleInstance::activationRequested, &app, [&](const QByteArray &token) {
    auto *w = qobject_cast<QWindow *>(engine.rootObjects().first());
    if (!w)
      return;
    if (!token.isEmpty()) // Wayland: lets the compositor grant focus to this window
      qputenv("XDG_ACTIVATION_TOKEN", token);
    w->setWindowStates(w->windowStates() & ~Qt::WindowMinimized);
    w->show();
    w->raise();
    w->requestActivate();
  });
  auto console = engine.rootObjects().first()->findChild<Console *>("console");
  if (!console)
    return 2;
  QObject::connect(&controller, &Controller::frameReady, console,
                   &Console::setFrame);
  QObject::connect(console, &Console::keyInput, &controller, &Controller::key);
  QObject::connect(console, &Console::pointerInput, &controller,
                   &Controller::pointer);
  QObject::connect(console, &Console::releaseInput, &controller,
                   &Controller::releaseInput);
  if (app.arguments().contains("--probe-ui")) {
    QFile input;
    if (!input.open(stdin, QIODevice::ReadOnly))
      return 4;
    auto json = QJsonDocument::fromJson(input.readAll()).object();
    auto profile = json["profile"].toObject().toVariantMap();
    if (profile.value("id").toString().isEmpty()) {
      for (const auto &v : controller.hosts()) {
        auto saved = v.toMap();
        if (saved.value("host") == profile.value("host")) {
          for (auto it = profile.cbegin(); it != profile.cend(); ++it)
            saved[it.key()] = it.value();
          profile = saved;
          break;
        }
      }
    }
    QObject::connect(&controller, &Controller::changed, &app, [&] {
      // Probe diagnostics use the same allowlisted metadata as the GUI.
      QJsonObject event{{"state", controller.state()},
                        {"info", QJsonObject::fromVariantMap(controller.consoleInfo())}};
      auto bytes = QJsonDocument(event).toJson(QJsonDocument::Compact);
      fprintf(stdout, "%s\n", bytes.constData());
      fflush(stdout);
      if (controller.state() == "disconnected" && controller.message().contains(" failed:")) {
        fprintf(stderr, "%s\n", qPrintable(controller.message()));
        app.exit(4);
      }
    });
    QTimer::singleShot(0, &controller, [&, json, profile] {
      QMetaObject::invokeMethod(
          engine.rootObjects().first(), "loadHost",
          Q_ARG(QVariant, QVariant(profile)));
      auto details = engine.rootObjects().first()->findChild<QObject *>("consoleDetails");
      if (details) details->setProperty("checked", true);
      controller.connectHost(profile,
                             json["password"].toString());
    });
    QObject::connect(
        &controller, &Controller::frameReady, &app, [&](QImage im) {
          if (im.isNull())
            return;
          QTimer::singleShot(200, &app, [&] {
            auto w = qobject_cast<QQuickWindow *>(engine.rootObjects().first());
            QString path = qEnvironmentVariable("OLDIPMI_TEST_SCREENSHOT");
            if (!path.isEmpty())
              w->grabWindow().save(path);
            app.exit(0);
          });
        });
    QTimer::singleShot(30000, &app, [&] {
      fprintf(stderr, "GUI probe timed out: %s\n",
              qPrintable(controller.message()));
      app.exit(4);
    });
  }
  // Only with --self-test: the tour deletes profiles, which must be test-mode copies.
  if (int i = app.arguments().indexOf("--screenshots"); test && i > 0 && i + 1 < app.arguments().size()) {
    QSize size;
    if (int s = app.arguments().indexOf("--size"); s > 0 && s + 1 < app.arguments().size()) {
      auto wh = app.arguments()[s + 1].split('x');
      if (wh.size() == 2)
        size = {wh[0].toInt(), wh[1].toInt()};
      if (size.isEmpty()) {
        fprintf(stderr, "--size expects WIDTHxHEIGHT\n");
        return 1;
      }
    }
    return runScreenshotTour(app, engine, controller, app.arguments()[i + 1], size);
  }
  if (test || preview)
    QTimer::singleShot(100, &controller, &Controller::preview);
  if (test)
    QTimer::singleShot(600, &app, [&] {
      auto w = qobject_cast<QQuickWindow *>(engine.rootObjects().first());
      if (!w || w->grabWindow().isNull())
        app.exit(3);
      else if (QString why = consoleSizeChanges(w, console); !why.isEmpty()) {
        fprintf(stderr, "Console view changed size: %s\n", qPrintable(why));
        app.exit(5);
      } else if (QString why = keyboardFails(w, console, controller); !why.isEmpty()) {
        fprintf(stderr, "Keyboard: %s\n", qPrintable(why));
        app.exit(5);
      } else if (QString why = fullScreenFails(w, console, controller); !why.isEmpty()) {
        fprintf(stderr, "Full screen: %s\n", qPrintable(why));
        app.exit(5);
      } else {
        QString path = qEnvironmentVariable("OLDIPMI_TEST_SCREENSHOT");
        if (!path.isEmpty())
          w->grabWindow().save(path);
        app.exit(qmlWarningsResult());
      }
    });
  return app.exec();
}
