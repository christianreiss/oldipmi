#include "singleinstance.h"
#include <QDir>
#include <QFile>
#include <QLocalSocket>
#include <QLockFile>
#include <unistd.h>

QString SingleInstance::defaultDir() {
  auto dir = qEnvironmentVariable("XDG_RUNTIME_DIR");
  if (dir.isEmpty())
    dir = QDir::tempPath() + "/oldipmi-" + QString::number(getuid());
  return dir + "/oldipmi";
}
SingleInstance::SingleInstance(QString dir, QObject *parent)
    : QObject(parent), m_dir(dir.isEmpty() ? defaultDir() : dir) {
  m_server.setSocketOptions(QLocalServer::UserAccessOption);
  connect(&m_server, &QLocalServer::newConnection, this, [this] {
    while (auto *s = m_server.nextPendingConnection()) {
      connect(s, &QLocalSocket::disconnected, s, &QObject::deleteLater);
      connect(s, &QLocalSocket::readyRead, this, [this, s] {
        if (!s->canReadLine()) {
          if (s->bytesAvailable() > 1024)
            s->abort();
          return;
        }
        auto line = s->readLine(1024).trimmed();
        s->disconnectFromServer();
        if (!line.startsWith("activate"))
          return;
        QByteArray token;
        for (char ch : line.mid(8).trimmed()) // printable ASCII only
          if (ch > ' ' && ch < 127)
            token += ch;
        emit activationRequested(token.left(256));
      });
    }
  });
}
SingleInstance::~SingleInstance() {
  m_server.close();
  if (m_lock)
    m_lock->unlock();
}
bool SingleInstance::acquire(const QByteArray &token) {
  QDir().mkpath(m_dir);
  QFile::setPermissions(m_dir, QFile::ReadOwner | QFile::WriteOwner | QFile::ExeOwner);
  const QString sock = m_dir + "/instance.sock";
  m_lock = std::make_unique<QLockFile>(m_dir + "/instance.lock");
  // Never stale by age; a lock whose process has ended is reclaimed.
  m_lock->setStaleLockTime(0);
  if (m_lock->tryLock(0)) {
    QLocalServer::removeServer(sock);
    if (!m_server.listen(sock))
      fprintf(stderr, "OldIPMI: cannot listen on %s: %s; a second start will not raise this window.\n",
              qPrintable(sock), qPrintable(m_server.errorString()));
    return true;
  }
  m_lock.reset();
  QLocalSocket s;
  s.connectToServer(sock);
  if (s.waitForConnected(1000)) {
    s.write("activate " + token + "\n");
    s.waitForBytesWritten(1000);
    s.disconnectFromServer();
  }
  return false;
}
