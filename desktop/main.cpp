#include "agentapi.h"
#include "console.h"
#include "controller.h"
#include "devtools/qmlwarnings.h"
#include "devtools/selftest.h"
#include "devtools/tour.h"
#include "families.h"
#include "framethumbnail.h"
#include "i18n.h"
#include "settings.h"
#include "singleinstance.h"
#include <QIcon>
#include <QImage>
#include <algorithm>
#include <cstring>
#include <QApplication>
#include <QFile>
#include <QJsonDocument>
#include <QJsonObject>
#include <QPointer>
#include <QQmlApplicationEngine>
#include <QQmlContext>
#include <QQuickWindow>
#include <QStandardPaths>
#include <QStyleHints>
#include <QTimer>
#include <QWindow>
#include <dlfcn.h>
int main(int argc, char **argv) {
  // Test mode must be on before QApplication: the platform theme reads
  // kdeglobals (colours, icon theme, fonts) during construction.
  bool test = std::any_of(argv + 1, argv + argc, [](const char *a) { return !strcmp(a, "--self-test"); });
  if (test)
    QStandardPaths::setTestModeEnabled(true);
  // The AppImage also runs where the Breeze icons are missing, as on GNOME or
  // in a bare container. KIconThemes, which it bundles, then makes its built-in
  // Breeze icons the theme; on Plasma that does nothing. Its AppRun sets
  // OLDIPMI_BUNDLE, so an installed OldIPMI keeps the desktop's icons.
  if (qEnvironmentVariableIsSet("OLDIPMI_BUNDLE"))
    if (void *themes = dlopen("libKF6IconThemes.so.6", RTLD_NOW))
      if (auto initTheme = reinterpret_cast<void (*)()>(dlsym(themes, "_ZN10KIconTheme9initThemeEv")))
        initTheme(); // KIconTheme::initTheme(), before the QApplication as it asks
  QApplication app(argc, argv);
  if (test) {
    devtools::recordQmlWarnings();
    QGuiApplication::styleHints()->setCursorFlashTime(0); // a steady cursor in every screenshot
  }
  app.setApplicationName("OldIPMI");
  app.setOrganizationName("OldIPMI");
  app.setApplicationVersion(OLDIPMI_VERSION);
  app.setApplicationDisplayName("OldIPMI");
  app.setDesktopFileName(QStringLiteral(OLDIPMI_APP_ID));
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
  if (tour && (profiles = devtools::writeTourProfiles()).isEmpty()) {
    fprintf(stderr, "Screenshot tour: could not write the test-mode connections\n");
    return 5;
  }
  qmlRegisterType<Console>("OldIPMI", 1, 0, "ConsoleView");
  qmlRegisterType<FrameThumbnail>("OldIPMI", 1, 0, "FrameThumbnail");
  // The family table (desktop/families.json) for qml/families.js.
  qmlRegisterSingletonType("OldIPMI", 1, 0, "FamilyTable",
                           [](QQmlEngine *, QJSEngine *js) -> QJSValue { return familyTableScript(js); });
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
  engine.rootContext()->setContextProperty("testClock", test ? double(devtools::testNow.toMSecsSinceEpoch()) : 0.0);
  // The self-test's configuration folder (in ~/.qttest), which its pages show as ~/.config.
  engine.rootContext()->setContextProperty(
      "testConfigDir", test ? QStandardPaths::writableLocation(QStandardPaths::GenericConfigLocation) : QString());
  const QUrl mainQml("qrc:/desktop/qml/Main.qml");
  devtools::setQmlRoot(mainQml.adjusted(QUrl::RemoveFilename).toString());
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
    return devtools::runScreenshotTour(app, engine, controller, testCredentials, app.arguments()[shots + 1], size);
  }
  if (test || preview)
    QTimer::singleShot(100, &controller, &Controller::preview);
  if (test)
    QTimer::singleShot(600, &app, [&] {
      auto w = qobject_cast<QQuickWindow *>(engine.rootObjects().first());
      if (!w || w->grabWindow().isNull())
        app.exit(3);
      else if (QString why = devtools::consoleSizeChanges(w, console); !why.isEmpty()) {
        fprintf(stderr, "Console view changed size: %s\n", qPrintable(why));
        app.exit(5);
      } else if (QString why = devtools::copyFails(w, console); !why.isEmpty()) {
        fprintf(stderr, "Copy: %s\n", qPrintable(why));
        app.exit(5);
      } else if (QString why = devtools::keyboardFails(w, console, controller); !why.isEmpty()) {
        fprintf(stderr, "Keyboard: %s\n", qPrintable(why));
        app.exit(5);
      } else if (QString why = devtools::fullScreenFails(w, console, controller); !why.isEmpty()) {
        fprintf(stderr, "Full screen: %s\n", qPrintable(why));
        app.exit(5);
      } else if (QString why = devtools::unknownFamilyFails(w); !why.isEmpty()) {
        fprintf(stderr, "Unknown family: %s\n", qPrintable(why));
        app.exit(5);
      } else {
        QString path = qEnvironmentVariable("OLDIPMI_TEST_SCREENSHOT");
        if (!path.isEmpty())
          w->grabWindow().save(path);
        app.exit(devtools::qmlWarningsResult());
      }
    });
  return app.exec();
}
