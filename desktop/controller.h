#pragma once
#include "credentials.h"
#include "session.h"
#include <QImage>
#include <QJsonObject>
#include <QLocalSocket>
#include <QObject>
#include <QProcess>
#include <QVariantList>
#include <QHash>
#include <functional>
#include <memory>

class Controller : public QObject {
  Q_OBJECT
  Q_PROPERTY(QVariantList hosts READ hosts NOTIFY hostsChanged)
  Q_PROPERTY(QString state READ state NOTIFY changed)
  Q_PROPERTY(QString message READ message NOTIFY changed)
  Q_PROPERTY(QVariantMap consoleInfo READ consoleInfo NOTIFY changed)
  Q_PROPERTY(bool mounted READ mounted NOTIFY changed)
  // Profile of the session being set up or shown; empty when idle.
  Q_PROPERTY(QVariantMap activeProfile READ activeProfile NOTIFY changed)
  // Setup stage that failed last (an ipc::Stage*), else empty.
  Q_PROPERTY(QString errorStage READ errorStage NOTIFY changed)
  // Class of the error in message (an ipc::Code*), else empty.
  Q_PROPERTY(QString errorCode READ errorCode NOTIFY changed)
  Q_PROPERTY(bool hasFrame READ hasFrame NOTIFY changed)
  // Every session the app holds (open, failed or ended), by profile id:
  // {state, errorStage, message, startedAt}. Re-evaluates whenever any session changes.
  Q_PROPERTY(QVariantMap sessions READ sessions NOTIFY changed)
  Q_PROPERTY(int openCount READ openCount NOTIFY changed)
  // Profile id of the session the console view shows; empty when none.
  Q_PROPERTY(QString shownId READ shownId NOTIFY changed)
public:
  explicit Controller(QObject *parent = nullptr, Credentials *credentials = nullptr,
                      QString configFile = {});
  ~Controller();
  QVariantList hosts() const { return m_hosts; }
  // The single-session properties describe the shown session (or "disconnected").
  QString state() const { return m_shown ? m_shown->stateName() : QString(ipc::StateDisconnected); }
  QString message() const;
  QString errorCode() const;
  QVariantMap consoleInfo() const { return m_shown ? m_shown->consoleInfo : QVariantMap(); }
  bool mounted() const { return m_shown && m_shown->mounted; }
  QVariantMap activeProfile() const { return m_shown ? m_shown->profile : QVariantMap(); }
  QString errorStage() const { return m_shown ? m_shown->errorStageName() : QString(); }
  bool hasFrame() const { return m_shown && !m_shown->lastFrame.isNull(); }
  QVariantMap sessions() const;
  int openCount() const;
  QString shownId() const { return m_shown ? m_shown->id : QString(); }
  Session *shown() const { return m_shown; }
  Session *session(const QString &id) const { return m_sessions.value(id); }
  QList<Session *> allSessions() const { return m_sessions.values(); }
  // Profile ids of sessions that are set up or connected (not failed/ended ones).
  QStringList openIds() const;
  Q_INVOKABLE bool isOpen(QString id) const;
  // Show an existing session in the console view; an empty or unknown id shows none.
  Q_INVOKABLE void show(QString id);
  Q_INVOKABLE void disconnectProfile(QString id);
  Q_INVOKABLE void disconnectAll();
  Q_INVOKABLE QVariantMap duplicateHost(QString id);
  Q_INVOKABLE QVariantMap host(QString id) const;
  Q_INVOKABLE void sendKeys(QVariantList usages);
  // Puts the shown console's picture on the clipboard at its own resolution, without
  // alpha (as the MCP screenshot). Returns its size; an invalid size when nothing is shown.
  Q_INVOKABLE QSize copyFrame();
  Q_INVOKABLE QString profilesPath() const { return m_configFile; }
  // Profile waiting for a password (after passwordRequired/walletUnavailable).
  Q_INVOKABLE QVariantMap pendingProfile() const { return m_pendingProfile; }
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
  // Power action for a specific session (by profile id); the shown one when id is empty.
  Q_INVOKABLE void powerFor(QString id, QString action);
  Q_INVOKABLE QString chooseCertificate(bool key);
  void preview();
  void agentNote(QString message);
signals:
  void changed();
  void hostsChanged();
  void profileActivated(QVariantMap profile);
  void passwordRequired();
  void walletUnavailable();
  void frameReady(QImage image);

private:
  QVariantList m_hosts;
  QVariantMap m_pendingProfile;
  QString m_pendingPassword, m_configFile, m_message;
  quint64 m_messageStamp = 0;
  bool m_profilesReadable = true;
  std::unique_ptr<Credentials> m_ownedCredentials;
  Credentials *m_credentials;
  QHash<QString, Session *> m_sessions;
  Session *m_shown = nullptr;
  void setMessage(const QString &m);
  void fail(QString message);
  void start(QVariantMap profile, QString password);
  void setShown(Session *s);
  void removeSession(Session *s);
  bool persist(const QVariantList &hosts);
  void loadProfiles();
  void stamp(const QString &id);
};
