#pragma once
// The C++ types and singletons of the QML module OldIPMI (qt_add_qml_module in
// CMakeLists.txt). Most of their classes live in oldipmi_desktop, which knows
// nothing of QML modules; declared here, in the application, they are
// registered with the module and described in its type information, which
// qmllint and the QML compiler read.
#include "agentapi.h"
#include "console.h"
#include "controller.h"
#include "families.h"
#include "framethumbnail.h"
#include "settings.h"
#include <QJSEngine>
#include <QJsonArray>
#include <QObject>
#include <QString>
#include <QtQml/qqmlregistration.h>

// The remote screen: draws the frames, takes the keyboard and the pointer.
struct ConsoleViewForeign {
  Q_GADGET
  QML_FOREIGN(Console)
  QML_NAMED_ELEMENT(ConsoleView)
};

// A live miniature of an open console, for the connection list.
struct FrameThumbnailForeign {
  Q_GADGET
  QML_FOREIGN(FrameThumbnail)
  QML_NAMED_ELEMENT(FrameThumbnail)
};

// The console families (desktop/families.json), which families.js reads.
class FamilyTable : public QObject {
  Q_OBJECT
  QML_ELEMENT
  QML_SINGLETON
  Q_PROPERTY(QJsonArray list READ list CONSTANT)
public:
  QJsonArray list() const { return familyTable(); }
};

// The singletons are the objects Application owns, set as instance before
// Main.qml loads; the engine gets them, and must not delete them.
template <typename T> T *applicationSingleton(T *instance) {
  Q_ASSERT(instance);
  QJSEngine::setObjectOwnership(instance, QJSEngine::CppOwnership);
  return instance;
}

// The connections, their sessions and the console actions.
struct ControllerForeign {
  Q_GADGET
  QML_FOREIGN(Controller)
  QML_NAMED_ELEMENT(Controller)
  QML_SINGLETON
public:
  static inline Controller *instance = nullptr;
  static Controller *create(QQmlEngine *, QJSEngine *) { return applicationSingleton(instance); }
};

// The preferences.
struct AppSettingsForeign {
  Q_GADGET
  QML_FOREIGN(AppSettings)
  QML_NAMED_ELEMENT(AppSettings)
  QML_SINGLETON
public:
  static inline AppSettings *instance = nullptr;
  static AppSettings *create(QQmlEngine *, QJSEngine *) { return applicationSingleton(instance); }
};

// The local control socket for oldipmi-mcp.
struct AgentApiForeign {
  Q_GADGET
  QML_FOREIGN(AgentApi)
  QML_NAMED_ELEMENT(AgentApi)
  QML_SINGLETON
public:
  static inline AgentApi *instance = nullptr;
  static AgentApi *create(QQmlEngine *, QJSEngine *) { return applicationSingleton(instance); }
};

// How this run was started; the same for the whole session.
class Launch : public QObject {
  Q_OBJECT
  QML_ELEMENT
  QML_SINGLETON
  // Whether the agent socket (auth-less, for oldipmi-mcp) may open: false with
  // --no-agent-api and in the self-test. Settings then says that its switch
  // applies from the next start.
  Q_PROPERTY(bool agentAllowed READ agentAllowed CONSTANT)
  // The test mode's fixed time, in milliseconds since the epoch; 0 otherwise.
  Q_PROPERTY(double testClock READ testClock CONSTANT)
  // The self-test's configuration folder (in ~/.qttest), which its pages show
  // as ~/.config; "" otherwise.
  Q_PROPERTY(QString testConfigDir READ testConfigDir CONSTANT)
public:
  Launch(bool agentAllowed, double testClock, const QString &testConfigDir)
      : m_agentAllowed(agentAllowed), m_testClock(testClock), m_testConfigDir(testConfigDir) {}
  bool agentAllowed() const { return m_agentAllowed; }
  double testClock() const { return m_testClock; }
  QString testConfigDir() const { return m_testConfigDir; }
  static inline Launch *instance = nullptr;
  static Launch *create(QQmlEngine *, QJSEngine *) { return applicationSingleton(instance); }

private:
  const bool m_agentAllowed;
  const double m_testClock;
  const QString m_testConfigDir;
};
