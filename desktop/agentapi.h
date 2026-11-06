#pragma once
#include "controller.h"
#include <QJsonObject>
#include <QLocalServer>
#include <QObject>
#include <QPointer>

// Local, authentication-less control socket used by oldipmi-mcp. It only calls
// Controller methods on the GUI thread; the socket file is owner-only.
class AgentApi : public QObject {
  Q_OBJECT
  Q_PROPERTY(bool listening READ listening NOTIFY listeningChanged)
  Q_PROPERTY(QString socketPath READ socketPath CONSTANT)
  Q_PROPERTY(QString mcpPath READ mcpPath CONSTANT)
public:
  explicit AgentApi(Controller *controller, QString socketPath = {}, QObject *parent = nullptr);
  bool start();
  void stop() { m_server.close(); emit listeningChanged(); }
  bool listening() const { return m_server.isListening(); }
  QString socketPath() const { return m_path; }
  QString mcpPath() const;
  QString errorString() const { return m_server.errorString().isEmpty() ? "socket already in use or path too long (max ~107 bytes)" : m_server.errorString(); }
  Q_INVOKABLE QString mcpInstructions() const;
  static QString defaultSocketPath();
  // Runs one operation; `reply` is called exactly once (possibly later).
  using Reply = std::function<void(bool ok, QJsonValue result)>;
  void dispatch(const QString &op, const QJsonObject &args, Reply reply);
signals:
  void listeningChanged();

private:
  Controller *m_c;
  QString m_path;
  QLocalServer m_server;
  void accept();
  void note(const QString &what);
  QVariantMap findProfile(const QString &key) const;
};
