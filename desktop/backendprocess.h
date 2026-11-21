#pragma once
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
  explicit BackendProcess(QObject *parent = nullptr);
  ~BackendProcess() override;
  // Creates the sockets and starts the core (OLDIPMI_BIN_DIR, else the app's
  // directory) with them.
  Start start();
  QLocalSocket *control() const { return m_control; }
  QLocalSocket *video() const { return m_video; }
  QString errorString() const;
  // Asks the core to end at once (SIGTERM).
  void terminate();
  // Ends the core: gives it 1.5 s to end by itself (it logs out after a
  // disconnect), then terminates it, and kills it 2.5 s later. Blocks meanwhile.
  void stop();

signals:
  void errorOccurred(QProcess::ProcessError error);
  void finished();

private:
  QProcess *m_process = nullptr;
  QLocalSocket *m_control = nullptr, *m_video = nullptr;
};
