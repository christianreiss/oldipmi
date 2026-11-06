#pragma once
#include <QElapsedTimer>
#include <QImage>
#include <QJsonObject>
#include <QLocalSocket>
#include <QObject>
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
  void begin(const QString &password, const QString &jnlp);
  // Ends the backend process (blocking up to a few seconds) and clears the frame.
  void stop();
  void command(QJsonObject obj, Result done = {});
  void key(int usage, bool down);
  void pointer(int x, int y, int buttons, int wheel);
  void releaseInput();
  void sendKeys(const QVariantList &usages);
  bool connected() const { return state == "connected"; }
  qint64 msSinceChange() const { return sinceChange.isValid() ? sinceChange.elapsed() : 0; }
  void setMessage(const QString &m);

  const QString id;       // profile id; unique among open sessions
  QVariantMap profile;
  QString state = "disconnected", message, errorStage;
  quint64 messageStamp = 0;
  QVariantMap consoleInfo;
  bool mounted = false;
  QImage lastFrame;
  qint64 frameSeq = 0, changeSeq = 0;
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
  size_t m_frameHash = 0;
  std::map<int, Result> m_pending;
  void fail(const QString &message);
  void failPending(const QString &why);
  void readControl();
  void readVideo();
};
