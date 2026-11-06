#pragma once
#include <QLocalServer>
#include <QObject>
#include <memory>

class QLockFile;

// One OldIPMI per user session: the first instance holds a lock file and a
// local socket; a later start asks it to show its window and then exits.
// Profiles live only in memory between saves, so two instances would
// overwrite each other's changes.
class SingleInstance : public QObject {
  Q_OBJECT
public:
  explicit SingleInstance(QString dir = {}, QObject *parent = nullptr);
  ~SingleInstance() override;
  // True when this process is now the only instance. False when another
  // instance runs; it has been asked to show itself (token: the launcher's
  // XDG activation token, passed on so the compositor lets it take focus).
  bool acquire(const QByteArray &token = {});
  static QString defaultDir();
signals:
  void activationRequested(const QByteArray &token);

private:
  QString m_dir;
  std::unique_ptr<QLockFile> m_lock;
  QLocalServer m_server;
};
