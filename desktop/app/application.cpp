#include "app/application.h"
#include "console.h"
#include "devtools/qmlwarnings.h"
#include "devtools/selftest.h"
#include "framethumbnail.h"
#include <QCoreApplication>
#include <QQmlContext>
#include <QStandardPaths>
#include <QWindow>
#include <cstdio>

Application::Application(const Options &options, const QString &profiles)
    : m_controller(nullptr, options.test ? &m_testCredentials : nullptr, profiles),
      // The self-test never opens the agent socket. Its pages show where an installed
      // OldIPMI has it and oldipmi-mcp: the same in every run, and no local paths.
      m_agent(&m_controller, options.test ? QString("/run/user/1000/oldipmi/agent.sock") : QString()),
      m_launch(!options.test && options.agentApi,
               options.test ? double(devtools::testNow.toMSecsSinceEpoch()) : 0.0,
               options.test ? QStandardPaths::writableLocation(QStandardPaths::GenericConfigLocation) : QString()) {
  FrameThumbnail::setController(&m_controller);
  if (options.test)
    m_agent.setMcpPath("/usr/bin/oldipmi-mcp");
  applyAgentSetting();
  QObject::connect(&m_settings, &AppSettings::changed, &m_agent, [this] { applyAgentSetting(); });
  m_engine.rootContext()->setContextObject(&m_i18n);
  // The module's singletons (app/qmltypes.h) are these objects.
  ControllerForeign::instance = &m_controller;
  AppSettingsForeign::instance = &m_settings;
  AgentApiForeign::instance = &m_agent;
  Launch::instance = &m_launch;
}

void Application::applyAgentSetting() {
  if (!m_launch.agentAllowed() || m_settings.agentApiEnabled() == m_agent.enabled())
    return;
  m_agent.setEnabled(m_settings.agentApiEnabled());
  if (m_agent.enabled() && !m_agent.listening())
    fprintf(stderr, "Agent API unavailable at %s: %s\n", qPrintable(m_agent.socketPath()),
            qPrintable(m_agent.errorString()));
}

ExitCode Application::load() {
  const QUrl mainQml("qrc:/qt/qml/OldIPMI/Main.qml");
  devtools::setQmlRoot(mainQml.adjusted(QUrl::RemoveFilename).toString());
  m_engine.load(mainQml);
  if (m_engine.rootObjects().isEmpty())
    return ExitCode::QmlNotLoaded;
  QObject *root = m_engine.rootObjects().first();
  m_quitRequests.root = root;
  QCoreApplication::instance()->installEventFilter(&m_quitRequests);
  m_console = root->findChild<Console *>("console");
  if (!m_console)
    return ExitCode::NoConsoleView;
  QObject::connect(&m_controller, &Controller::frameReady, m_console, &Console::setFrame);
  QObject::connect(m_console, &Console::keyInput, &m_controller, &Controller::key);
  QObject::connect(m_console, &Console::pointerInput, &m_controller, &Controller::pointer);
  QObject::connect(m_console, &Console::releaseInput, &m_controller, &Controller::releaseInput);
  return ExitCode::Ok;
}

void Application::activate(const QByteArray &token) {
  auto *w = qobject_cast<QWindow *>(m_engine.rootObjects().first());
  if (!w)
    return;
  if (!token.isEmpty()) // Wayland: lets the compositor grant focus to this window
    qputenv("XDG_ACTIVATION_TOKEN", token);
  w->setWindowStates(w->windowStates() & ~Qt::WindowMinimized);
  w->show();
  w->raise();
  w->requestActivate();
}

bool Application::QuitRequests::eventFilter(QObject *watched, QEvent *e) {
  if (e->type() == QEvent::Quit && watched == qApp && root)
    root->setProperty("quitConfirmed", true);
  return false;
}
