#include "devtools/tour.h"
#include "app/exitcode.h"
#include "console.h"
#include "controller.h"
#include "credentials.h"
#include "devtools/qmlwarnings.h"
#include "devtools/selftest.h"
#include <QApplication>
#include <QCursor>
#include <QDir>
#include <QFile>
#include <QFileInfo>
#include <QImage>
#include <QJsonArray>
#include <QJsonDocument>
#include <QJsonObject>
#include <QKeyEvent>
#include <QQmlApplicationEngine>
#include <QQuickItem>
#include <QQuickWindow>
#include <QSet>
#include <QStandardPaths>
#include <QTimer>
#include <algorithm>
#include <functional>
#include <memory>

namespace devtools {
QString writeTourProfiles() {
  const QString dir = QStandardPaths::writableLocation(QStandardPaths::AppConfigLocation);
  const QString path = QDir(dir).filePath("hosts.json");
  const QJsonArray profiles{
      QJsonObject{{"id", "tour-ami"}, {"name", "Lab-AMI"}, {"host", "lab-ami.example.net"},
                  {"username", "ADMIN"}, {"family", "ami"}, {"remember", false},
                  {"lastConnected", testNow.addSecs(-2 * 3600).toString(Qt::ISODate)}},
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
int runScreenshotTour(QApplication &app, QQmlApplicationEngine &engine, Controller &controller,
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
    return code(ExitCode::CheckFailed);
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
           app.exit(code(ExitCode::CheckFailed));
         } else if (window->close()) {
           problem("closing the window with two consoles open did not ask first");
           app.exit(code(ExitCode::CheckFailed));
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
    // No picture is loaded far larger than shown. An Image whose sourceSize
    // follows its own width loads it again at twice the size each time, until
    // a layout sets that width; one that stays hidden ends at 8192 px (256 MiB).
    for (auto *w : visibleWindows())
      for (auto *o : objectsOf(w))
        if (auto *image = qobject_cast<QQuickItem *>(o); image && image->inherits("QQuickImage")) {
          const QSize source = image->property("sourceSize").toSize();
          if (std::max({image->implicitWidth(), image->implicitHeight(), qreal(source.width()),
                        qreal(source.height())}) > 2048)
            problem(QString("%1: %2 is loaded at %3x%4 (sourceSize %5x%6)")
                        .arg(step.name, image->property("source").toUrl().fileName())
                        .arg(image->implicitWidth()).arg(image->implicitHeight())
                        .arg(source.width()).arg(source.height()));
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
      const ExitCode warnings = qmlWarningsResult(); // listed either way
      app.exit(code(problems->isEmpty() ? warnings : ExitCode::CheckFailed));
      return;
    }
    auto &step = (*steps)[(*index)++];
    step.act();
    QTimer::singleShot(step.wait, &app, [=] { (*settle)(0); });
  };
  QTimer::singleShot(300, &app, *next);
  return app.exec();
}
} // namespace devtools
