#pragma once
#include <QElapsedTimer>
#include <QLocalSocket>
#include <QObject>
#include <QProcess>

// The oldipmi-core of one Session: the child process and the two sockets to it,
// the control socket on the core's descriptor 3 and the video socket on 4. What
// the core writes to stderr goes to the app's own stderr (C-08); nothing reads
// its stdout.
class BackendProcess : public QObject {
  Q_OBJECT
public:
  enum class Start {
    Started,
    NotStarted,      // the process did not start within 3 s; errorOccurred said why
    NoControlPair,   // the control socket pair could not be created
    NoVideoPair,     // the video socket pair could not be created
    NoDescriptors,   // the core's ends could not be moved out of the way of 3 and 4
  };
  // host is the BMC the core talks to.
  explicit BackendProcess(QString host, QObject *parent = nullptr);
  // Waits for a core that has not ended, as stop() lets it end, with the
  // times counted from stop(): quitting does not cut a logout short.
  ~BackendProcess() override;
  // Creates the sockets and starts the core (OLDIPMI_BIN_DIR, else the app's
  // directory) with them.
  Start start();
  QLocalSocket *control() const { return m_control; }
  QLocalSocket *video() const { return m_video; }
  QString errorString() const;
  // Asks the core to end at once (SIGTERM).
  void terminate();
  // Lets the core end without waiting for it: it gets 1.5 s to end by itself
  // (it logs out after a disconnect), is then terminated, and killed 2.5 s
  // later. What it still sends is read and dropped. Once the core has ended,
  // the object deletes itself.
  void stop();
  // A core for host that stop() is letting end, if there is one.
  static BackendProcess *stopping(const QString &host);

signals:
  void errorOccurred(QProcess::ProcessError error);
  // The core has ended.
  void finished();

private:
  const QString m_host;
  QProcess *m_process = nullptr;
  QLocalSocket *m_control = nullptr, *m_video = nullptr;
  QElapsedTimer m_stopping; // valid from stop() on
  int remaining(int ms) const;
};
