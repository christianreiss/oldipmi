#pragma once
#include <QObject>
#include <QVariantList>
#include <QVariantMap>

class Credentials;

// The connection profiles in hosts.json, and the passwords the wallet keeps
// for them. A profile holds id, name, host, username, family, http,
// legacyTLS, allowPlainConsole, jnlpPath, clientCert, clientKey, consoleMode,
// lastConnected and remember (testdata/ipc/connect.json); save() drops any
// other key. The file is {"version": 1, "profiles": [...]}; a file in the
// older array format is rewritten once, and kept as hosts.json.pre-profiles.
// Nothing is built in: without a file the list starts empty.
class ProfileStore : public QObject {
  Q_OBJECT
public:
  ProfileStore(QString file, Credentials *credentials, QObject *parent = nullptr);
  // Reads the file. One it cannot read safely is left alone, and nothing is
  // written to it then; failed says so.
  void load();
  QString path() const { return m_file; }
  QVariantList profiles() const { return m_profiles; }
  QVariantMap profile(const QString &id) const;
  // Saves profile under its id (a new one without), with the password in the
  // wallet if remember; returns it as saved, or {} after failed.
  QVariantMap save(const QVariantMap &profile, const QString &password, bool remember);
  // A copy of the profile with its own id, without its password or last
  // connection; {} if there is none, or after failed.
  QVariantMap duplicate(const QString &id);
  bool remove(const QString &id);
  // The password the wallet keeps for profile, if it is saved with the same
  // host and account: one is never reused after either changed.
  QString savedPassword(const QVariantMap &profile) const;
  // Notes that profile id was just connected.
  void stamp(const QString &id);

signals:
  void profilesChanged();
  void failed(const QString &message);

private:
  const QString m_file;
  Credentials *const m_credentials;
  QVariantList m_profiles;
  bool m_readable = true;
  bool persist(const QVariantList &profiles);
};
