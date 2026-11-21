#pragma once
#include "ipc.h"
#include <QElapsedTimer>
#include <QImage>
#include <QJsonObject>
#include <QLocalSocket>
#include <QObject>
#include <QPointer>
#include <QProcess>
#include <QVariantMap>
#include <functional>
#include <map>

// One console connection: its own oldipmi-core child process, control/video
// sockets, state and latest frame. Controller owns zero or more of them.
class Session : public QObject {
  Q_OBJECT
public:
  using Result = std::function<void(bool, QString)>;
  // Monotonic stamp shared with Controller so the newest message wins.
  static quint64 nextStamp();

  Session(QString key, QVariantMap profile, QObject *parent = nullptr);
  ~Session() override;
  void begin(const QString &password);
  // Tells the core to disconnect, without waiting: it logs out of the BMC
  // first, which takes it up to three seconds. Closing several consoles tells
  // all of them before stopping any, so that their logouts overlap.
  void sendDisconnect();
  // Ends the backend process (blocking up to a few seconds) and clears the frame.
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
  bool connected() const { return state == ipc::StateConnected; }
  qint64 msSinceChange() const { return sinceChange.isValid() ? sinceChange.elapsed() : 0; }
  void setMessage(const QString &m);

  const QString id;       // profile id; unique among open sessions
  QVariantMap profile;
  QString state = ipc::StateDisconnected, message, errorStage;
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
  QProcess *m_process = nullptr;
  QLocalSocket *m_control = nullptr, *m_video = nullptr;
  QByteArray m_controlBuffer, m_videoBuffer;
  int m_id = 0;
  bool m_disconnectSent = false;
  size_t m_frameHash = 0;
  struct Pending {
    QPointer<const QObject> context;
    Result done;
  };
  std::map<int, Pending> m_pending;
  static void settle(const Pending &p, bool ok, const QString &message);
  void fail(const QString &message);
  void failPending(const QString &why);
  void readControl();
  void readVideo();
};
