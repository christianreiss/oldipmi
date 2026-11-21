#include "backendprocess.h"
#include <QCoreApplication>
#include <fcntl.h>
#include <sys/socket.h>
#include <unistd.h>

BackendProcess::BackendProcess(QObject *parent) : QObject(parent) {}
BackendProcess::~BackendProcess() { stop(); }

BackendProcess::Start BackendProcess::start() {
  int ctl[2], vid[2];
  if (socketpair(AF_UNIX, SOCK_STREAM | SOCK_CLOEXEC, 0, ctl) < 0)
    return Start::NoControlPair;
  if (socketpair(AF_UNIX, SOCK_STREAM | SOCK_CLOEXEC, 0, vid) < 0) {
    close(ctl[0]);
    close(ctl[1]);
    return Start::NoVideoPair;
  }
  // Reserve high descriptors so dup2 cannot clobber a descriptor still needed.
  int childCtl = fcntl(ctl[1], F_DUPFD_CLOEXEC, 20),
      childVid = fcntl(vid[1], F_DUPFD_CLOEXEC, 20);
  close(ctl[1]);
  close(vid[1]);
  if (childCtl < 0 || childVid < 0) {
    close(ctl[0]);
    close(vid[0]);
    if (childCtl >= 0)
      close(childCtl);
    if (childVid >= 0)
      close(childVid);
    return Start::NoDescriptors;
  }
  m_control = new QLocalSocket(this);
  m_video = new QLocalSocket(this);
  m_control->setSocketDescriptor(ctl[0], QLocalSocket::ConnectedState, QIODevice::ReadWrite);
  m_video->setSocketDescriptor(vid[0], QLocalSocket::ConnectedState, QIODevice::ReadOnly);
  m_process = new QProcess(this);
  m_process->setChildProcessModifier([childCtl, childVid] {
    dup2(childCtl, 3);
    dup2(childVid, 4);
    close(childCtl);
    close(childVid);
  });
  connect(m_process, &QProcess::errorOccurred, this, &BackendProcess::errorOccurred);
  connect(m_process, qOverload<int, QProcess::ExitStatus>(&QProcess::finished), this,
          [this](int, QProcess::ExitStatus) { emit finished(); });
  // What the core logs (a Go panic, log.Printf, the AVPT trace) goes to the
  // app's own stderr, the terminal or the journal; nothing reads its stdout.
  // Kept by QProcess instead, it went unread and grew (C-08).
  m_process->setProcessChannelMode(QProcess::ForwardedErrorChannel);
  m_process->setStandardOutputFile(QProcess::nullDevice());
  m_process->setProgram(
      qEnvironmentVariable("OLDIPMI_BIN_DIR", QCoreApplication::applicationDirPath()) +
      "/oldipmi-core");
  m_process->start();
  bool started = m_process->waitForStarted(3000);
  close(childCtl);
  close(childVid);
  return started ? Start::Started : Start::NotStarted;
}

QString BackendProcess::errorString() const { return m_process ? m_process->errorString() : QString(); }

void BackendProcess::terminate() {
  if (m_process)
    m_process->terminate();
}

void BackendProcess::stop() {
  if (m_process) {
    disconnect(m_process, nullptr, this, nullptr);
    if (!m_process->waitForFinished(1500))
      m_process->terminate();
    if (!m_process->waitForFinished(2500)) {
      m_process->kill();
      m_process->waitForFinished(1000);
    }
    delete m_process;
    m_process = nullptr;
  }
  delete m_control;
  delete m_video;
  m_control = m_video = nullptr;
}
