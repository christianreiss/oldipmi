#pragma once
#include "channels.h"
#include "ipc.h"
#include <QElapsedTimer>
#include <QImage>
#include <QJsonObject>
#include <QObject>
#include <QPointer>
#include <QVariantMap>
#include <functional>
#include <map>
#include <optional>

class BackendProcess;

// One console connection: its own oldipmi-core (BackendProcess), state and
// latest frame. Controller owns zero or more of them.
class Session : public QObject {
  Q_OBJECT
public:
  // A console's state: disconnected, one of the setup stages in order, or
  // connected. The core and QML know them by name (ipc::Stage*, ipc::State*).
  enum class State { Disconnected, LoggingIn, Discovering, Connecting, Connected };
  Q_ENUM(State)
  using Result = std::function<void(bool, QString)>;
  // Monotonic stamp shared with Controller so the newest message wins.
  static quint64 nextStamp();

  Session(QString key, QVariantMap profile, QObject *parent = nullptr);
  ~Session() override;
  // Starts the core, once a core for the same host that is still logging out
  // has ended: a BMC may allow a single session.
  void begin(const QString &password);
  // Closes the console and clears the frame, without waiting for the core: it
  // gets the disconnect, logs out of the BMC in the background and ends as
  // BackendProcess::stop() lets it. Meanwhile the session's parent keeps it,
  // so destroying the parent waits for it.
  void stop();
  void command(QJsonObject obj);
  // With a callback: it gets the reply, or why there is none, once and only
  // while `context` exists. It never runs from the destructor: a session that
  // is deleted settles its callbacks from the event loop (C-07).
  void command(QJsonObject obj, const QObject *context, Result done);
  void key(int usage, bool down);
  void pointer(int x, int y, int buttons, int wheel);
  void releaseInput();
  void sendKeys(const QVariantList &usages);
  bool connected() const { return state == State::Connected; }
  QString stateName() const;
  QString errorStageName() const; // empty without an error stage
  qint64 msSinceChange() const { return sinceChange.isValid() ? sinceChange.elapsed() : 0; }
  void setMessage(const QString &m);

  const QString id;       // profile id; unique among open sessions
  QVariantMap profile;
  State state = State::Disconnected;
  std::optional<State> errorStage; // the setup stage the console failed in
  QString message;
  // The class of the error in message (an ipc::Code*), if the core named one;
  // setMessage drops it.
  QString errorCode;
  quint64 messageStamp = 0;
  QVariantMap consoleInfo;
  bool mounted = false;
  QImage lastFrame;
  qint64 frameSeq = 0, changeSeq = 0;
  // When begin() or preview() started this session (ms since the epoch); the UI shows
  // how long a setup has been running.
  qint64 startedAt = 0;
  QElapsedTimer sinceChange;

  void preview(); // synthetic framebuffer, no backend
signals:
  void changed();
  void frameReady(QImage image);

private:
  BackendProcess *m_backend = nullptr;
  QMetaObject::Connection m_waitForHost; // begin() waits for another core to end
  ControlChannel m_controlIn;
  VideoChannel m_videoIn;
  int m_id = 0;
  bool m_disconnectSent = false;
  size_t m_frameHash = 0;
  struct Pending {
    QPointer<const QObject> context;
    Result done;
  };
  std::map<int, Pending> m_pending;
  static void settle(const Pending &p, bool ok, const QString &message);
  void launch(const QString &password);
  void sendDisconnect();
  void fail(const QString &message);
  void failPending(const QString &why);
  void readControl();
  void readVideo();
};
