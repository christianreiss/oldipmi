#pragma once
#include "credentials.h"
#include "session.h"
#include "sessionmanager.h"
#include <QImage>
#include <QJsonObject>
#include <QLocalSocket>
#include <QObject>
#include <QProcess>
#include <QVariantList>
#include <functional>
#include <memory>

class FileDialogs;
class ProfileStore;

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
  // Without credentials it uses KWallet, and without dialogs QFileDialog.
  explicit Controller(QObject *parent = nullptr, Credentials *credentials = nullptr,
                      QString configFile = {}, FileDialogs *dialogs = nullptr);
  ~Controller();
  QVariantList hosts() const;
  // The single-session properties describe the shown session (or "disconnected").
  QString state() const { return shown() ? shown()->stateName() : QString(ipc::StateDisconnected); }
  QString message() const;
  QString errorCode() const;
  QVariantMap consoleInfo() const { return shown() ? shown()->consoleInfo : QVariantMap(); }
  bool mounted() const { return shown() && shown()->mounted; }
  QVariantMap activeProfile() const { return shown() ? shown()->profile : QVariantMap(); }
  QString errorStage() const { return shown() ? shown()->errorStageName() : QString(); }
  bool hasFrame() const { return shown() && !shown()->lastFrame.isNull(); }
  QVariantMap sessions() const;
  int openCount() const;
  QString shownId() const { return shown() ? shown()->id : QString(); }
  Session *shown() const { return m_sessions->shown(); }
  Session *session(const QString &id) const { return m_sessions->session(id); }
  QList<Session *> allSessions() const { return m_sessions->all(); }
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
  Q_INVOKABLE QString profilesPath() const;
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
  QVariantMap m_pendingProfile;
  QString m_pendingPassword, m_message;
  quint64 m_messageStamp = 0;
  std::unique_ptr<Credentials> m_ownedCredentials;
  std::unique_ptr<ProfileStore> m_profiles; // uses the credentials
  std::unique_ptr<FileDialogs> m_ownedDialogs;
  FileDialogs *m_dialogs;
  std::unique_ptr<SessionManager> m_sessions; // the last member: it goes first
  void setMessage(const QString &m);
  void fail(QString message);
};
