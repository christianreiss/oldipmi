#pragma once
#include "credentials.h"
#include <QImage>
#include <QJsonObject>
#include <QLocalSocket>
#include <QObject>
#include <QProcess>
#include <QVariantList>
#include <QElapsedTimer>
#include <functional>
#include <map>

class Controller : public QObject {
  Q_OBJECT
  Q_PROPERTY(QVariantList hosts READ hosts NOTIFY hostsChanged)
  Q_PROPERTY(QString state READ state NOTIFY changed)
  Q_PROPERTY(QString message READ message NOTIFY changed)
  Q_PROPERTY(QVariantMap consoleInfo READ consoleInfo NOTIFY changed)
  Q_PROPERTY(bool mounted READ mounted NOTIFY changed)
public:
  explicit Controller(QObject *parent = nullptr, Credentials *credentials = nullptr,
                      QString configFile = {});
  ~Controller();
  QVariantList hosts() const { return m_hosts; }
  QString state() const { return m_state; }
  QString message() const { return m_message; }
  QVariantMap consoleInfo() const { return m_consoleInfo; }
  bool mounted() const { return m_mounted; }
  Q_INVOKABLE QVariantMap saveHost(QVariantMap profile, QString password,
                                   bool remember);
  Q_INVOKABLE bool removeHost(QString id);
  Q_INVOKABLE void connectHost(QVariantMap profile, QString password);
  Q_INVOKABLE void disconnectHost();
  Q_INVOKABLE void continueWithoutWallet();
  Q_INVOKABLE void cancelCredentials();
  Q_INVOKABLE void key(int usage, bool down);
  Q_INVOKABLE void pointer(int x, int y, int buttons, int wheel);
  Q_INVOKABLE void releaseInput();
  Q_INVOKABLE void mountISO();
  Q_INVOKABLE void ejectISO();
  Q_INVOKABLE void power(QString action);
  Q_INVOKABLE void openJNLP(QVariantMap profile);
  Q_INVOKABLE QString chooseCertificate(bool key);
  void preview();
  // Result callback: (ok, message). Invoked when the backend answers command `id`.
  using Result = std::function<void(bool, QString)>;
  void command(QJsonObject obj, Result done = {});
  void agentNote(QString message);
  QImage lastFrame() const { return m_lastFrame; }
  qint64 frameSeq() const { return m_frameSeq; }
  qint64 changeSeq() const { return m_changeSeq; }
  qint64 msSinceChange() const { return m_sinceChange.isValid() ? m_sinceChange.elapsed() : 0; }
signals:
  void changed();
  void hostsChanged();
  void profileActivated(QVariantMap profile);
  void passwordRequired();
  void walletUnavailable();
  void frameReady(QImage image);

private:
  QVariantList m_hosts;
  QVariantMap m_consoleInfo, m_pendingProfile;
  QString m_pendingPassword, m_configFile;
  bool m_profilesReadable = true;
  std::unique_ptr<Credentials> m_ownedCredentials;
  Credentials *m_credentials;
  QString m_state = "disconnected",
          m_message = "Choose a profile and open its console.";
  bool m_mounted = false;
  QProcess *m_process = nullptr;
  QLocalSocket *m_control = nullptr, *m_video = nullptr;
  QByteArray m_controlBuffer, m_videoBuffer;
  int m_id = 0;
  QImage m_lastFrame;
  qint64 m_frameSeq = 0, m_changeSeq = 0;
  size_t m_frameHash = 0;
  QElapsedTimer m_sinceChange;
  std::map<int, Result> m_pending;
  void failPending(const QString &why);
  void start(QVariantMap profile, QString password, QString jnlp = {});
  void fail(QString message);
  void readControl();
  void readVideo();
  bool persist(const QVariantList &hosts);
  void loadProfiles();
  void stop();
};
