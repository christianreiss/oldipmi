#include "agentapi.h"
#include "console.h"
#include "controller.h"
#include "framethumbnail.h"
#include "i18n.h"
#include "settings.h"
#include "singleinstance.h"
#include <QCursor>
#include <QIcon>
#include <QImage>
#include <algorithm>
#include <cstring>
#include <QApplication>
#include <QClipboard>
#include <QDateTime>
#include <QDir>
#include <QElapsedTimer>
#include <QEventLoop>
#include <QFile>
#include <QFileInfo>
#include <QJsonArray>
#include <QJsonDocument>
#include <QJsonObject>
#include <QKeyEvent>
#include <QKeySequence>
#include <QMouseEvent>
#include <QPointer>
#include <QQmlApplicationEngine>
#include <QQmlContext>
#include <QQuickWindow>
#include <QSet>
#include <QStandardPaths>
#include <QStyleHints>
#include <QTimer>
#include <QWindow>
#include <functional>
#include <memory>
// --self-test and its screenshot tour fail on any warning from OldIPMI's own QML
// (binding loops, type errors, unresolved names), so every UI state the tour
// visits is also checked for them.
static QStringList s_qmlWarnings;
static QtMessageHandler s_previousHandler = nullptr;
static QString s_qmlRoot; // the directory of Main.qml; empty until the engine loads it
static void recordQmlWarning(QtMsgType type, const QMessageLogContext &context, const QString &message) {
  if (type != QtDebugMsg && type != QtInfoMsg && !s_qmlRoot.isEmpty() &&
      (message.contains(s_qmlRoot) || (context.file && QString::fromUtf8(context.file).startsWith(s_qmlRoot))))
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
// states and saves one PNG per state and window. Never touches the network: the
// connecting and error states use the demo console's "hang" and "unreachable".
// It fails loudly, with exit 5: a step that never becomes ready, a named object
// that is missing, a QML method, signal or property that does not exist, or a
// PNG that cannot be saved. QML warnings make it exit 6.
// The self-test's clock: every "elapsed" and "last connected" label reads the
// same in every run (the QML gets it as testClock; 0 is the real time).
static const QDateTime s_testNow = QDateTime::fromString("2026-01-15T09:41:00Z", Qt::ISODate);
// The tour's connections, written to the test-mode profiles file before the
// controller reads it: every run starts from the same list, whatever an earlier
// run left (the tour removes them at its end). Returns the file, or "" on failure.
static QString writeTourProfiles() {
  const QString dir = QStandardPaths::writableLocation(QStandardPaths::AppConfigLocation);
  const QString path = QDir(dir).filePath("hosts.json");
  const QJsonArray profiles{
      QJsonObject{{"id", "tour-ami"}, {"name", "Lab-AMI"}, {"host", "lab-ami.example.net"},
                  {"username", "ADMIN"}, {"family", "ami"}, {"remember", false},
                  {"lastConnected", s_testNow.addSecs(-2 * 3600).toString(Qt::ISODate)}},
      QJsonObject{{"id", "tour-aten"}, {"name", "Lab-ATEN"}, {"host", "lab-aten.example.net"},
                  {"username", "ADMIN"}, {"family", "aten"}, {"remember", false}},
      QJsonObject{{"id", "tour-avocent"}, {"name", "Lab-Avocent"}, {"host", "lab-avocent.example.net"},
                  {"username", "root"}, {"family", "avocent"}, {"legacyTLS", true}, {"remember", false}}};
  const QByteArray data =
      QJsonDocument(QJsonObject{{"version", 1}, {"defaultsVersion", 1}, {"profiles", profiles}}).toJson();
  QFile f(path);
  if (!QDir().mkpath(dir) || !f.open(QIODevice::WriteOnly | QIODevice::Truncate) || f.write(data) != data.size())
    return {};
  return path;
}
static int runScreenshotTour(QApplication &app, QQmlApplicationEngine &engine, Controller &controller,
                             MemoryCredentials &credentials, const QString &dir, QSize size) {
  auto *root = engine.rootObjects().first();
  auto *window = qobject_cast<QQuickWindow *>(root);
  // Every failed check is reported when it happens; the tour then exits 5.
  auto problems = std::make_shared<QStringList>();
  auto problem = [problems](const QString &why) {
    fprintf(stderr, "Screenshot tour: %s\n", qPrintable(why));
    *problems << why;
  };
  auto label = [](QObject *o) {
    return o->objectName().isEmpty() ? QString(o->metaObject()->className()) : o->objectName();
  };
  // A named object a step needs: when it is missing the step fails instead of
  // being skipped.
  auto find = [root, problem](const char *name) -> QObject * {
    auto *o = root->findChild<QObject *>(name);
    if (!o)
      problem(QString("no object named \"%1\"").arg(name));
    return o;
  };
  // QML functions (var arguments): a name that does not exist, or arguments that
  // do not match, fail the step.
  auto invoke = [problem, label](QObject *o, const char *method, const QVariant &arg = {}) {
    if (!o)
      return; // find() reported it
    const bool ok = arg.isValid() ? QMetaObject::invokeMethod(o, method, Q_ARG(QVariant, arg))
                                  : QMetaObject::invokeMethod(o, method);
    if (!ok)
      problem(QString("%1.%2() cannot be called").arg(label(o), method));
  };
  auto emitWith = [problem, label](QObject *o, const char *signal, const QString &arg) {
    if (o && !QMetaObject::invokeMethod(o, signal, Q_ARG(QString, arg)))
      problem(QString("%1.%2(\"%3\") cannot be emitted").arg(label(o), signal, arg));
  };
  // setProperty() adds a dynamic property, and returns false, for a name the
  // object does not declare.
  auto set = [problem, label](QObject *o, const char *property, const QVariant &value) {
    if (o && !o->setProperty(property, value))
      problem(QString("%1 has no property %2").arg(label(o), property));
  };
  auto call = [=](const char *method, const QVariant &arg = {}) { invoke(root, method, arg); };
  auto visibleWindows = [] {
    QList<QQuickWindow *> out;
    for (auto *w : QGuiApplication::topLevelWindows())
      if (auto *qw = qobject_cast<QQuickWindow *>(w); qw && qw->isVisible())
        out << qw;
    return out;
  };
  // Optional --size WxH: every grabbed window (main, dialogs' host, settings)
  // gets the same logical size, so review tools can compare a fixed matrix.
  // Resizes the windows that differ; true when none did.
  auto fit = [size, visibleWindows] {
    bool fitted = true;
    for (auto *w : visibleWindows())
      if (!size.isEmpty() && w->size() != size) {
        w->resize(size);
        fitted = false;
      }
    return fitted;
  };
  fit();
  // Offscreen Qt puts the pointer at the window origin, which renders false
  // hover states (tooltips); park it outside every window.
  QCursor::setPos(-10000, -10000);
  // The connections from writeTourProfiles(), the first one selected.
  if (controller.hosts().size() != 3 || !QDir().mkpath(dir)) {
    fprintf(stderr, "Screenshot tour: the tour's connections or %s are missing\n", qPrintable(dir));
    return 5;
  }
  call("loadHost", controller.hosts().first());
  // `wait` ms after `act`, the step is grabbed as soon as `ready` (if given) holds,
  // every window has its size and the pictures have stopped changing, with no
  // limit a busy machine would reach.
  struct Step { QString name; std::function<void()> act; int wait; std::function<bool()> ready = {}; };
  // Every object of a window, also those that are only visual children (list
  // delegates and what they declare, such as each row's menu).
  auto objectsOf = [](QQuickWindow *w) {
    QList<QObject *> out, todo{w, w->contentItem()};
    QSet<QObject *> seen;
    while (!todo.isEmpty()) {
      QObject *o = todo.takeLast();
      if (!o || seen.contains(o))
        continue;
      seen.insert(o);
      out << o;
      todo << o->children();
      if (auto *item = qobject_cast<QQuickItem *>(o))
        for (auto *child : item->childItems())
          todo << child;
    }
    return out;
  };
  // A list view estimates the height of the rows it has not created yet, from the
  // ones it has, and which those are depends on how it was scrolled before; its
  // scroll bar is then a pixel or two longer or shorter. A clipped list creates
  // every row here (those outside stay hidden): the exact length, in every run.
  auto createAllRows = [objectsOf, visibleWindows] {
    constexpr int margin = 100000;
    for (auto *w : visibleWindows())
      for (auto *o : objectsOf(w))
        if (auto *view = qobject_cast<QQuickItem *>(o); view && view->inherits("QQuickItemView") && view->clip()
            && view->property("displayMarginEnd").toInt() < margin) {
          view->setProperty("displayMarginBeginning", margin);
          view->setProperty("displayMarginEnd", margin);
        }
  };
  auto popups = [objectsOf, window] {
    QList<QObject *> out;
    for (auto *o : objectsOf(window))
      if (o->inherits("QQuickPopup"))
        out << o;
    return out;
  };
  auto closePopups = [popups, invoke] {
    for (auto *o : popups())
      if (o->property("opened").toBool())
        invoke(o, "close");
  };
  auto closeOtherWindows = [window] {
    for (auto *w : QGuiApplication::topLevelWindows())
      if (w != window)
        w->close();
  };
  // Popups still fading in or out, and scrims between clear and dark: a grab
  // waits for them, so it never shows a dialog that is halfway open or closed.
  auto *sidebar = root->findChild<QObject *>("sidebar");
  if (!sidebar)
    problem("no object named \"sidebar\"");
  auto inTransition = [popups, label, visibleWindows, sidebar, &controller] {
    QStringList busy;
    // The list regroups its rows a moment after a console opens or closes.
    if (sidebar && sidebar->property("openRows").toInt() != controller.openCount())
      busy << "the connection list";
    for (auto *o : popups())
      if (o->property("visible").toBool() && !o->property("opened").toBool())
        busy << label(o);
    for (auto *w : visibleWindows()) {
      QQuickItem *top = w->contentItem();
      while (top->parentItem())
        top = top->parentItem();
      for (auto *layer : top->childItems())
        if (layer->inherits("QQuickOverlay"))
          for (auto *item : layer->childItems())
            if (item->isVisible() && item->opacity() > 0 && item->opacity() < 1)
              busy << QString("%1 at opacity %2").arg(label(item)).arg(item->opacity());
    }
    return busy;
  };
  // Silent lookups for the ready checks, which run many times; acts use find().
  auto console = [root] { return root->findChild<Console *>("console"); };
  auto popup = [popups](const char *name) -> QObject * {
    for (auto *o : popups())
      if (o->objectName() == QLatin1String(name))
        return o;
    return nullptr;
  };
  // Any popup of that name (every row has its own menu) is open.
  auto popupOpen = [popups](const char *name) {
    for (auto *o : popups())
      if (o->objectName() == QLatin1String(name) && o->property("opened").toBool())
        return true;
    return false;
  };
  // Scrolls the general settings to their end, the keyboard shortcuts; true once there.
  auto generalSettingsEnd = [objectsOf, visibleWindows] {
    for (auto *w : visibleWindows())
      for (auto *o : objectsOf(w))
        if (o->objectName() == QLatin1String("generalSettings")) {
          auto *f = o->property("flickable").value<QObject *>();
          if (!f)
            return false;
          auto get = [f](const char *name) { return f->property(name).toDouble(); };
          const double end = std::max(get("originY") - get("topMargin"),
                                      get("originY") + get("contentHeight") + get("bottomMargin") - get("height"));
          if (get("contentY") != end)
            f->setProperty("contentY", end);
          return get("contentY") == end;
        }
    return false;
  };
  auto consoleView = [console, problem] {
    auto *c = console();
    if (!c)
      problem("no console view");
    return c;
  };
  // Fails at the login as an unreachable BMC does, so the error page shows its hint.
  QVariantMap demo{{"name", "Demo BMC"}, {"host", "demo-unreachable"}, {"family", "demo"},
                   {"remember", false}};
  // The login stays pending until it is cancelled, however long the step takes.
  QVariantMap pending{{"name", "Lab server"}, {"host", "demo-hang"}, {"family", "demo"},
                      {"remember", false}};
  // The full-screen bar shows for a moment after entering full screen, and the
  // note after taking or giving back control: both are set before the grab
  // instead of hoping that it comes in time. True once the bar and the note are
  // shown or hidden as asked (an open menu of the bar keeps it shown).
  const QList<QObject *> transient{find("fullscreenPeek"), find("fullscreenNoteTimer"),
                                   find("fullscreenBar"), find("fullscreenNote")};
  auto transients = [=](bool bar, bool note) {
    if (transient.contains(nullptr))
      return true; // find() reported it
    const bool run[] = {bar, note};
    for (int i = 0; i < 2; ++i)
      if (transient[i]->property("running").toBool() != run[i])
        invoke(transient[i], run[i] ? "restart" : "stop");
    return transient[2]->property("opacity").toDouble() == (bar ? 1.0 : 0.0) &&
           transient[3]->property("opacity").toDouble() == (note ? 1.0 : 0.0);
  };
  // Keyboard and mouse to the server, once the window is active.
  auto takeControl = [=] {
    auto *c = console();
    if (!c || !window->isActive())
      return false;
    if (!c->captured())
      c->capture();
    return c->captured();
  };
  auto recaptured = std::make_shared<bool>(false); // 10b: control given back and taken again in full screen
  auto revealed = std::make_shared<bool>(false);   // 10e: scrolled to the advanced options
  auto steps = std::make_shared<QList<Step>>(QList<Step>{
      {"01-selected", [] {}, 600},
      {"02-new-connection", [=] { call("newConnection"); }, 700, [=] { return popupOpen("connectionDialog"); }},
      {"03-edit-connection", [=, &controller] {
         closePopups();
         call("editConnection", controller.hosts().first());
       }, 700, [=] { return popupOpen("connectionDialog"); }},
      {"04-settings-general", [=] { closePopups(); call("openSettings", QString("general")); }, 900,
       [=] { return visibleWindows().size() == 2; }},
      // The end of the general page: the keyboard shortcuts.
      {"04a-settings-shortcuts", [] {}, 300, [=] { return generalSettingsEnd(); }},
      {"05-settings-agents", [=] { call("openSettings", QString("agents")); }, 900,
       [=] { return visibleWindows().size() == 2; }},
      {"05a-settings-about", [=] { call("openSettings", QString("about")); }, 900,
       [=] { return visibleWindows().size() == 2; }},
      // The instructions for agents, as the AI agents page shows them.
      {"05b-mcp-instructions", [=] {
         closeOtherWindows();
         call("showMcpInstructions");
       }, 700, [=] { return popupOpen("mcpDialog") && visibleWindows().size() == 1; }},
      {"05c-main-menu", [=] { closePopups(); invoke(find("mcpButton"), "clicked"); }, 500,
       [=] { return popupOpen("mainMenu"); }},
      // The menu key opens the selected row's context menu below the row.
      {"05d-row-menu", [=] {
         closePopups();
         if (auto *list = find("connectionList")) {
           QKeyEvent press(QEvent::KeyPress, Qt::Key_Menu, Qt::NoModifier);
           QCoreApplication::sendEvent(list, &press);
         }
       }, 500, [=] { return popupOpen("rowMenu"); }},
      // A password to be saved while the wallet is unavailable: the warning, and
      // the offer to connect without saving (to the demo console: no network).
      {"05e-wallet", [=, &controller, &credentials] {
         closePopups();
         credentials.available = false;
         controller.connectHost(QVariantMap{{"name", "Demo console"}, {"host", "demo"}, {"family", "demo"},
                                            {"remember", true}}, "tour");
       }, 700, [=] { return popupOpen("walletDialog"); }},
      {"06-connecting", [=, &controller, &credentials] {
         closePopups(); // Cancel on the wallet warning, as the user would
         controller.cancelCredentials();
         credentials.available = true;
         controller.connectHost(pending, {});
       }, 800, [=, &controller] { return controller.state() == ipc::StageLoggingIn && visibleWindows().size() == 1; }},
      {"07-error", [=, &controller] {
         controller.disconnectHost();
         controller.connectHost(demo, {});
       }, 600, [&controller] {
         // The failed login ends its backend; only then is the console no longer open.
         return !controller.errorStage().isEmpty() && controller.openCount() == 0;
       }},
      {"08-console", [=, &controller] {
         controller.disconnectHost();
         controller.connectHost(QVariantMap{{"name", "Demo console"}, {"host", "demo"}, {"family", "demo"}}, {});
         set(find("consoleDetails"), "checked", true);
       }, 900, [&controller] { return controller.hasFrame(); }},
      // Through the console's own request, as its power menu sends it.
      {"09-power-confirm", [=] { emitWith(find("consolePane"), "powerRequested", "reset"); },
       700, [=] { return popupOpen("powerDialog"); }},
      {"10-switch", [=] {
         // Connecting another profile opens it next to the first; both stay connected.
         closePopups();
         call("connectProfile", QVariantMap{{"name", "Demo wide"}, {"host", "demo-1920x1080"}, {"family", "demo"}});
       }, 900, [&controller] { return controller.hasFrame() && controller.openCount() == 2; }},
      // Keyboard and mouse go to the server: the accent ring and the status strip.
      // The window is activated first: activation arrives later than the request and
      // resets the focus, so control is taken once the window is active.
      {"10a-captured", [=] {
         window->requestActivate();
         consoleView();
       }, 500, [=] { return takeControl(); }},
      // Full screen: the floating bar (shown for a moment after entering) and the
      // note that keyboard and mouse go to the server, taken again once full screen.
      {"10b-fullscreen", [=] { call("toggleFullscreen"); }, 800, [=] {
         auto *c = console();
         if (window->visibility() != QWindow::FullScreen || !c)
           return false;
         if (!*recaptured) {
           c->release();
           window->requestActivate();
           *recaptured = true;
         }
         return takeControl() && transients(true, true);
       }},
      // Released in full screen: the note says how to take control again.
      {"10b1-fullscreen-released", [=] { if (auto *c = consoleView()) c->release(); },
       700, [=] {
         auto *c = console();
         return window->visibility() == QWindow::FullScreen && c && !c->captured() && transients(true, true);
       }},
      // The bar's Send key menu; the bar stays while it is open.
      {"10b2-fullscreen-keys", [=] { invoke(find("fullscreenKeys"), "clicked"); }, 500, [=] {
         return window->visibility() == QWindow::FullScreen && popupOpen("fullscreenKeysMenu") && transients(true, false);
       }},
      // Full screen without a console (a closed connection selected, as after
      // losing one): the header with Quit and the connection list stay.
      {"10b3-fullscreen-idle", [=, &controller] {
         closePopups();
         call("loadHost", controller.hosts().first());
       }, 700, [=, &controller] {
         return window->visibility() == QWindow::FullScreen && !controller.hasFrame() && transients(false, false);
       }},
      {"10c-delete-confirm", [=, &controller] {
         if (auto *c = consoleView()) c->release();
         if (window->visibility() == QWindow::FullScreen)
           call("toggleFullscreen");
         call("deleteConnection", controller.hosts().first());
       }, 700, [=] { return popupOpen("deleteDialog") && window->visibility() != QWindow::FullScreen; }},
      // Connecting a profile without a saved password asks for it.
      {"10d-password", [=, &controller] {
         closePopups();
         controller.connectHost(controller.hosts().first().toMap(), {});
       }, 700, [=] { return popupOpen("credentialsDialog"); }},
      // Editing a profile with security exceptions opens the advanced options;
      // scrolled to them, so a small window shows the controls themselves.
      {"10e-advanced", [=, &controller] {
         invoke(find("credentialsDialog"), "reject"); // Cancel, as the user would
         closePopups();
         const auto hosts = controller.hosts();
         auto legacy = std::find_if(hosts.cbegin(), hosts.cend(),
                                    [](const QVariant &h) { return h.toMap().value("legacyTLS").toBool(); });
         if (legacy == hosts.cend())
           problem("no connection with legacy TLS");
         else
           call("editConnection", *legacy);
       }, 700, [=] {
         auto *d = popup("connectionDialog");
         if (!d || !d->property("opened").toBool())
           return false;
         if (!*revealed) {
           invoke(d, "revealAdvanced");
           *revealed = true;
         }
         return !d->property("revealPending").toBool();
       }},
      // Two consoles are open, so quitting asks first and names them, also when the
      // window is closed (its close button, Alt+F4).
      {"10e1-quit-confirm", [=, &app, &controller] {
         closePopups();
         // Both consoles of 10-switch must still be open; with only one, closing
         // the window rightly quits, so the tour stops here.
         if (controller.openCount() != 2) {
           problem(QString("%1 console(s) open before 10e1-quit-confirm, expected 2").arg(controller.openCount()));
           app.exit(5);
         } else if (window->close()) {
           problem("closing the window with two consoles open did not ask first");
           app.exit(5);
         }
       }, 700, [=] { return popupOpen("disconnectAllDialog") && popup("disconnectAllDialog")->property("quitting").toBool(); }},
      // Two consoles are open, so Disconnect all asks first and names them.
      {"10f-disconnect-all", [=, &controller] {
         closePopups(); // Cancel on the quit question keeps both consoles
         if (controller.openCount() != 2 || !window->isVisible())
           problem(QString("cancelling Quit left %1 console(s) open").arg(controller.openCount()));
         call("disconnectAll");
       }, 700, [=] { return popupOpen("disconnectAllDialog") && !popup("disconnectAllDialog")->property("quitting").toBool(); }},
      // Confirms the Disconnect all dialog: it closes exactly the consoles it names.
      {"11-empty", [=, &controller] {
         invoke(find("disconnectAllConfirm"), "clicked");
         if (controller.openCount())
           problem(QString("Disconnect all left %1 console(s) open").arg(controller.openCount()));
         closePopups();
         controller.disconnectAll();
         for (const auto &h : controller.hosts()) controller.removeHost(h.toMap().value("id").toString());
         call("loadHost", QVariantMap{});
       }, 600},
  });
  auto index = std::make_shared<int>(0);
  auto states = std::make_shared<QJsonObject>(); // "settings-general": "04-settings-general-2.png"
  auto next = std::make_shared<std::function<void()>>();
  auto settle = std::make_shared<std::function<void(int)>>();
  constexpr int readyLimit = 60000; // CTest's timeout bounds the whole tour
  // Once a step is ready its windows are grabbed every 100 ms until three
  // pictures in a row are the same: what is still being laid out or loaded (a
  // list's rows, a scroll bar, an image) never ends up half done in a picture.
  auto shots = std::make_shared<QList<QImage>>();
  auto unchanged = std::make_shared<int>(0);
  *settle = [=, &app, &controller](int waited) {
    const auto &step = (*steps)[*index - 1];
    createAllRows();
    const bool held = fit() && inTransition().isEmpty() && (!step.ready || step.ready());
    QList<QImage> now;
    if (held)
      for (auto *w : visibleWindows())
        now << w->grabWindow();
    *unchanged = held && now == *shots ? *unchanged + 1 : 0;
    *shots = now;
    const bool ready = *unchanged >= 2;
    if (!ready && waited < readyLimit) {
      QTimer::singleShot(100, &app, [=] { (*settle)(waited + 100); });
      return;
    }
    if (!ready && held) {
      problem(QString("%1: its picture kept changing for %2 s").arg(step.name).arg(readyLimit / 1000));
    } else if (!ready) {
      auto *focus = window->activeFocusItem();
      const QString busy = inTransition().join(", ");
      problem(QString("%1 did not become ready in %2 s (state %3, error stage \"%4\", %5 console(s) "
                      "open, frame %6, message \"%7\", window %8, focus on %9, in transition: %10)")
                  .arg(step.name).arg(readyLimit / 1000).arg(controller.state(), controller.errorStage())
                  .arg(controller.openCount())
                  .arg(controller.hasFrame() ? "shown" : "none", controller.message(),
                       window->isActive() ? "active" : "not active", focus ? label(focus) : "nothing",
                       busy.isEmpty() ? "nothing" : busy));
      for (auto *w : visibleWindows()) // what it looked like, for the report
        shots->append(w->grabWindow());
    }
    (*next)();
  };
  *next = [=, &app] {
    if (*index > 0) {
      const auto &done = (*steps)[*index - 1];
      int n = 0;
      for (const QImage &shot : std::as_const(*shots)) {
        const QString file = dir + "/" + done.name + (n++ ? "-" + QString::number(n) : "") + ".png";
        if (!shot.save(file))
          problem("cannot save " + file);
        (*states)[done.name.section('-', 1)] = QFileInfo(file).fileName(); // the newest window wins
      }
      shots->clear();
      *unchanged = 0;
    }
    if (*index >= steps->size()) {
      // For review tools (tools/slop/capture.py): the picture of each state.
      if (QFile f(dir + "/states.json"); problems->isEmpty() &&
          (!f.open(QIODevice::WriteOnly | QIODevice::Truncate) || f.write(QJsonDocument(*states).toJson()) < 0))
        problem("cannot write " + f.fileName());
      const int warnings = qmlWarningsResult(); // listed either way
      app.exit(problems->isEmpty() ? warnings : 5);
      return;
    }
    auto &step = (*steps)[(*index)++];
    step.act();
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
// Released, Ctrl+Shift+C puts the remote screen on the clipboard at its own size;
// captured, the shortcut is off, since every key goes to the server. Only on the
// offscreen platform (CTest): on a desktop it would replace the user's clipboard.
// Returns what failed, or "".
static QString copyFails(QQuickWindow *w, Console *console) {
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
// Quit: the bar shows and stays while it has the focus, the release note points to
// its Take control button, and Enter there gives keyboard and mouse to the server.
// Returns what failed, or "".
static QString fullScreenFails(QQuickWindow *w, Console *console, Controller &controller) {
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
int main(int argc, char **argv) {
  // Test mode must be on before QApplication: the platform theme reads
  // kdeglobals (colours, icon theme, fonts) during construction.
  bool test = std::any_of(argv + 1, argv + argc, [](const char *a) { return !strcmp(a, "--self-test"); });
  if (test)
    QStandardPaths::setTestModeEnabled(true);
  QApplication app(argc, argv);
  if (test) {
    s_previousHandler = qInstallMessageHandler(recordQmlWarning);
    QGuiApplication::styleHints()->setCursorFlashTime(0); // a steady cursor in every screenshot
  }
  app.setApplicationName("OldIPMI");
  app.setOrganizationName("OldIPMI");
  app.setApplicationVersion(OLDIPMI_VERSION);
  app.setApplicationDisplayName("OldIPMI");
  app.setDesktopFileName("net.alpha_labs.OldIPMI");
  app.setWindowIcon(QIcon::fromTheme("oldipmi", QIcon(":/oldipmi.svg")));
  // One instance per user session; a second start shows the running window.
  // The self-test runs with isolated test paths and stays independent. The UI
  // probe leaves a running OldIPMI alone and fails: it would probe nothing (C-18).
  const bool probe = app.arguments().contains("--probe-ui");
  SingleInstance instance;
  if (!test && !instance.acquire(qgetenv("XDG_ACTIVATION_TOKEN"), !probe)) {
    if (probe) {
      fprintf(stderr, "OldIPMI is already running; close it before --probe-ui.\n");
      return 6;
    }
    fprintf(stderr, "OldIPMI is already running; showing its window.\n");
    return 0;
  }
  bool preview = app.arguments().contains("--preview");
  const int shots = app.arguments().indexOf("--screenshots");
  // Only with --self-test: the tour deletes profiles, which must be test-mode copies.
  const bool tour = test && shots > 0 && shots + 1 < app.arguments().size();
  QString profiles;
  if (tour && (profiles = writeTourProfiles()).isEmpty()) {
    fprintf(stderr, "Screenshot tour: could not write the test-mode connections\n");
    return 5;
  }
  qmlRegisterType<Console>("OldIPMI", 1, 0, "ConsoleView");
  qmlRegisterType<FrameThumbnail>("OldIPMI", 1, 0, "FrameThumbnail");
  // The self-test never opens the user's wallet.
  MemoryCredentials testCredentials;
  Controller controller(nullptr, test ? &testCredentials : nullptr, profiles);
  FrameThumbnail::setController(&controller);
  AppSettings settings;
  // The self-test never opens the agent socket. Its pages show where an installed
  // OldIPMI has it and oldipmi-mcp: the same in every run, and no local paths.
  AgentApi agent(&controller, test ? QString("/run/user/1000/oldipmi/agent.sock") : QString());
  if (test)
    agent.setMcpPath("/usr/bin/oldipmi-mcp");
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
  // The root context object must outlive the engine, which is destroyed first.
  LocalizedContext i18n;
  QQmlApplicationEngine engine;
  engine.rootContext()->setContextObject(&i18n);
  engine.rootContext()->setContextProperty("controller", &controller);
  // False for the whole session with --no-agent-api or --self-test: Settings then
  // says that the agent switch applies from the next start.
  engine.rootContext()->setContextProperty("agentAllowed", agentAllowed);
  engine.rootContext()->setContextProperty("agent", &agent);
  engine.rootContext()->setContextProperty("settings", &settings);
  engine.rootContext()->setContextProperty("testClock", test ? double(s_testNow.toMSecsSinceEpoch()) : 0.0);
  // The self-test's configuration folder (in ~/.qttest), which its pages show as ~/.config.
  engine.rootContext()->setContextProperty(
      "testConfigDir", test ? QStandardPaths::writableLocation(QStandardPaths::GenericConfigLocation) : QString());
  const QUrl mainQml("qrc:/desktop/qml/Main.qml");
  s_qmlRoot = mainQml.adjusted(QUrl::RemoveFilename).toString();
  engine.load(mainQml);
  if (engine.rootObjects().isEmpty())
    return 1;
  engine.rootContext()->setContextProperty("mainWindow", engine.rootObjects().first());
  // Any quit request of the application (Qt.quit() after Quit was confirmed, or the
  // session manager when the session ends) closes the window without asking about
  // the open consoles; only Quit itself and closing the window ask (Main.qml).
  struct QuitRequests : QObject {
    QPointer<QObject> root;
    bool eventFilter(QObject *watched, QEvent *e) override {
      if (e->type() == QEvent::Quit && watched == qApp && root)
        root->setProperty("quitConfirmed", true);
      return false;
    }
  } quitRequests;
  quitRequests.root = engine.rootObjects().first();
  app.installEventFilter(&quitRequests);
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
  if (probe) {
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
                        {"message", controller.message()},
                        {"info", QJsonObject::fromVariantMap(controller.consoleInfo())}};
      auto bytes = QJsonDocument(event).toJson(QJsonDocument::Compact);
      fprintf(stdout, "%s\n", bytes.constData());
      fflush(stdout);
      if (controller.state() == ipc::StateDisconnected && controller.message().contains(" failed:")) {
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
    // Without a password there is nothing to probe: a prompt would wait for the user.
    QObject::connect(&controller, &Controller::passwordRequired, &app, [&] {
      fprintf(stderr, "%s\n", qPrintable(controller.message()));
      app.exit(7);
    });
    QObject::connect(
        &controller, &Controller::frameReady, &app, [&, framed = false](QImage im) mutable {
          if (im.isNull() || framed)
            return;
          framed = true;
          QJsonObject size{{"width", im.width()}, {"height", im.height()}};
          fprintf(stdout, "%s\n", QJsonDocument(QJsonObject{{"frame", size}}).toJson(QJsonDocument::Compact).constData());
          fflush(stdout);
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
  if (tour) {
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
    return runScreenshotTour(app, engine, controller, testCredentials, app.arguments()[shots + 1], size);
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
      } else if (QString why = copyFails(w, console); !why.isEmpty()) {
        fprintf(stderr, "Copy: %s\n", qPrintable(why));
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
