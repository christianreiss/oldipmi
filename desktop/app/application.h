#pragma once
#include "agentapi.h"
#include "app/exitcode.h"
#include "app/options.h"
#include "controller.h"
#include "credentials.h"
#include "i18n.h"
#include "settings.h"
#include <QByteArray>
#include <QObject>
#include <QPointer>
#include <QQmlApplicationEngine>

class Console;

// OldIPMI's window and everything behind it, for one run of the program.
// The members are built in the order they are declared and destroyed in reverse,
// the engine with all of the QML first:
// - the root context object outlives the engine;
// - the agent API goes before the controller, whose sessions then drop the
//   answers it still waits for;
// - the controller, last, logs every console out of its BMC, and the test
//   credentials it was given outlive it.
class Application {
public:
  // profiles: the connections file for the controller, "" for the usual one.
  Application(const Options &options, const QString &profiles);
  // Loads Main.qml and connects the console view to the controller. Ok, or what
  // the program exits with when that fails.
  ExitCode load();
  // A second start of OldIPMI asked for the window; token is its XDG activation
  // token, if it had one.
  void activate(const QByteArray &token);

  QQmlApplicationEngine &engine() { return m_engine; }
  Controller &controller() { return m_controller; }
  MemoryCredentials &testCredentials() { return m_testCredentials; }
  Console *console() const { return m_console; }

private:
  void applyAgentSetting();

  // The self-test never opens the user's wallet.
  MemoryCredentials m_testCredentials;
  Controller m_controller;
  AppSettings m_settings;
  AgentApi m_agent;
  // Auth-less local control socket for oldipmi-mcp; skipped by self-test and
  // --no-agent-api, and switchable at runtime in Settings → AI agents.
  const bool m_agentAllowed;
  // The root context object must outlive the engine, which is destroyed first.
  LocalizedContext m_i18n;
  QQmlApplicationEngine m_engine;
  // Any quit request of the application (Qt.quit() after Quit was confirmed, or the
  // session manager when the session ends) closes the window without asking about
  // the open consoles; only Quit itself and closing the window ask (Main.qml).
  struct QuitRequests : QObject {
    QPointer<QObject> root;
    bool eventFilter(QObject *watched, QEvent *e) override;
  } m_quitRequests;
  Console *m_console = nullptr;
};
