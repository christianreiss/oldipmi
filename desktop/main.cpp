#include "app/application.h"
#include "app/exitcode.h"
#include "app/options.h"
#include "devtools/probeui.h"
#include "devtools/qmlwarnings.h"
#include "devtools/selftest.h"
#include "devtools/tour.h"
#include "singleinstance.h"
#include <QApplication>
#include <QIcon>
#include <QStandardPaths>
#include <QStyleHints>
#include <QTimer>
#include <cstdio>
#include <dlfcn.h>
int main(int argc, char **argv) {
  // Test mode must be on before QApplication: the platform theme reads
  // kdeglobals (colours, icon theme, fonts) during construction.
  const bool test = selfTestRequested(argc, argv);
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
  const Options options = parseOptions(app.arguments(), test);
  // One instance per user session; a second start shows the running window.
  // The self-test runs with isolated test paths and stays independent. The UI
  // probe leaves a running OldIPMI alone and fails: it would probe nothing (C-18).
  SingleInstance instance;
  if (!test && !instance.acquire(qgetenv("XDG_ACTIVATION_TOKEN"), !options.probe)) {
    if (options.probe) {
      fprintf(stderr, "OldIPMI is already running; close it before --probe-ui.\n");
      return code(ExitCode::AlreadyRunning);
    }
    fprintf(stderr, "OldIPMI is already running; showing its window.\n");
    return code(ExitCode::Ok);
  }
  QString profiles;
  if (options.tour && (profiles = devtools::writeTourProfiles()).isEmpty()) {
    fprintf(stderr, "Screenshot tour: could not write the test-mode connections\n");
    return code(ExitCode::CheckFailed);
  }
  Application application(options, profiles);
  if (const ExitCode failed = application.load(); failed != ExitCode::Ok)
    return code(failed);
  QObject::connect(&instance, &SingleInstance::activationRequested, &app,
                   [&application](const QByteArray &token) { application.activate(token); });
  if (options.probe && !devtools::startProbeUi(app, application.engine(), application.controller()))
    return code(ExitCode::ProbeFailed);
  if (options.tour) {
    if (options.badSize) {
      fprintf(stderr, "--size expects WIDTHxHEIGHT\n");
      return code(ExitCode::BadArgument);
    }
    return devtools::runScreenshotTour(app, application.engine(), application.controller(),
                                       application.testCredentials(), options.tourDir, options.tourSize);
  }
  if (test || options.preview)
    QTimer::singleShot(100, &application.controller(), &Controller::preview);
  if (test)
    devtools::startSelfTest(app, application.engine(), application.console(), application.controller(),
                            application.settings());
  return app.exec();
}
