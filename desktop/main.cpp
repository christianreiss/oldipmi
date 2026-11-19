#include "agentapi.h"
#include "console.h"
#include "controller.h"
#include "i18n.h"
#include "settings.h"
#include "singleinstance.h"
#include <QCursor>
#include <QIcon>
#include <algorithm>
#include <cstring>
#include <QApplication>
#include <QFile>
#include <QJsonDocument>
#include <QJsonObject>
#include <QQmlApplicationEngine>
#include <QQmlContext>
#include <QQuickWindow>
#include <QStandardPaths>
#include <QTimer>
#include <QWindow>
#include <functional>
#include <memory>
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
  struct Step { QString name; std::function<void()> act; int wait; };
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
  QVariantMap demo{{"name", "Demo BMC"}, {"host", "oldipmi.invalid"}, {"family", "aten"},
                   {"username", "ADMIN"}, {"remember", false}};
  // A private address with no host behind it: the login stays pending until cancelled.
  QVariantMap pending{{"name", "Lab server"}, {"host", "10.255.255.1"}, {"family", "ami"},
                      {"username", "ADMIN"}, {"remember", false}};
  auto steps = std::make_shared<QList<Step>>(QList<Step>{
      {"01-selected", [] {}, 600},
      {"02-new-connection", [=] { call("newConnection"); }, 700},
      {"03-edit-connection", [=, &controller] {
         closePopups();
         auto p = controller.hosts().first().toMap();
         call("editConnection", p);
       }, 700},
      {"04-settings-general", [=] { closePopups(); call("openSettings", QString("general")); }, 900},
      {"05-settings-agents", [=] { call("openSettings", QString("agents")); }, 900},
      {"06-connecting", [=, &controller] {
         for (auto *w : QGuiApplication::topLevelWindows())
           if (w != qobject_cast<QWindow *>(root)) w->close();
         controller.connectHost(pending, "not-a-real-password");
       }, 800},
      {"07-error", [=, &controller] {
         controller.disconnectHost();
         controller.connectHost(demo, "not-a-real-password");
       }, 4000},
      {"08-console", [=, &controller] {
         controller.disconnectHost();
         controller.connectHost(QVariantMap{{"name", "Demo console"}, {"host", "demo"}, {"family", "demo"}}, {});
         if (auto *d = root->findChild<QObject *>("consoleDetails")) d->setProperty("checked", true);
       }, 1500},
      {"09-power-confirm", [=] { root->setProperty("pendingPower", "reset");
         if (auto *d = root->findChild<QObject *>("powerDialog")) QMetaObject::invokeMethod(d, "open"); }, 700},
      {"10-switch", [=] {
         // Connecting another profile opens it next to the first; both stay connected.
         closePopups();
         call("connectProfile", QVariantMap{{"name", "Demo wide"}, {"host", "demo-1920x1080"}, {"family", "demo"}});
       }, 1500},
      {"11-empty", [=, &controller] {
         closePopups();
         controller.disconnectAll();
         for (const auto &h : controller.hosts()) controller.removeHost(h.toMap().value("id").toString());
         call("loadHost", QVariantMap{});
       }, 600},
  });
  auto index = std::make_shared<int>(0);
  auto next = std::make_shared<std::function<void()>>();
  *next = [=, &app] {
    if (*index > 0) {
      const auto &done = (*steps)[*index - 1];
      int n = 0;
      for (auto *w : QGuiApplication::topLevelWindows())
        if (auto *qw = qobject_cast<QQuickWindow *>(w); qw && qw->isVisible())
          qw->grabWindow().save(dir + "/" + done.name + (n++ ? "-" + QString::number(n) : "") + ".png");
    }
    if (*index >= steps->size()) { app.exit(0); return; }
    auto &step = (*steps)[(*index)++];
    step.act();
    QTimer::singleShot(qMin(300, step.wait / 2), &app, fit);
    QTimer::singleShot(step.wait, &app, *next);
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
int main(int argc, char **argv) {
  // Test mode must be on before QApplication: the platform theme reads
  // kdeglobals (colours, icon theme, fonts) during construction.
  bool test = std::any_of(argv + 1, argv + argc, [](const char *a) { return !strcmp(a, "--self-test"); });
  if (test)
    QStandardPaths::setTestModeEnabled(true);
  QApplication app(argc, argv);
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
  Controller controller;
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
      } else {
        QString path = qEnvironmentVariable("OLDIPMI_TEST_SCREENSHOT");
        if (!path.isEmpty())
          w->grabWindow().save(path);
        app.exit(0);
      }
    });
  return app.exec();
}
