#include "profilestore.h"
#include "credentials.h"
#include <QDateTime>
#include <QDir>
#include <QFile>
#include <QFileInfo>
#include <QJsonArray>
#include <QJsonDocument>
#include <QJsonObject>
#include <QSaveFile>
#include <QUuid>

ProfileStore::ProfileStore(QString file, Credentials *credentials, QObject *parent)
    : QObject(parent), m_file(std::move(file)), m_credentials(credentials) {}

void ProfileStore::load() {
  QFile f(m_file);
  bool migrate = false;
  if (f.exists()) {
    if (!f.open(QIODevice::ReadOnly) || f.size() > 1024 * 1024) {
      m_readable = false;
    } else {
      auto doc = QJsonDocument::fromJson(f.readAll());
      if (doc.isArray()) {
        m_profiles = doc.array().toVariantList();
        // Preserve the old array format for rollback; never replace a backup.
        if (!QFile::exists(m_file + ".pre-profiles") && !f.copy(m_file + ".pre-profiles"))
          m_readable = false;
        migrate = true;
      } else if (doc.isObject() && doc["version"].toInt() == 1 && doc["profiles"].isArray()) {
        m_profiles = doc["profiles"].toArray().toVariantList();
      } else {
        m_readable = false;
      }
    }
  }
  if (!m_readable) {
    emit failed("Could not read or migrate profiles safely; the existing file was preserved.");
    return;
  }
  if (migrate)
    persist(m_profiles); // rewritten once in the current format
}

bool ProfileStore::persist(const QVariantList &profiles) {
  if (!m_readable) {
    emit failed("Profiles were not saved because the existing file could not be read safely.");
    return false;
  }
  QDir().mkpath(QFileInfo(m_file).absolutePath());
  QSaveFile f(m_file);
  if (!f.open(QIODevice::WriteOnly)) {
    emit failed("Could not save host profiles.");
    return false;
  }
  f.setPermissions(QFile::ReadOwner | QFile::WriteOwner);
  // defaultsVersion is no longer read; it stops 0.3.0 and older from adding their presets again.
  auto data = QJsonDocument(QJsonObject{{"version", 1}, {"defaultsVersion", 1},
                                        {"profiles", QJsonArray::fromVariantList(profiles)}})
                  .toJson();
  if (f.write(data) != data.size() || !f.commit()) {
    emit failed("Could not save host profiles.");
    return false;
  }
  return true;
}

QVariantMap ProfileStore::profile(const QString &id) const {
  for (const auto &v : m_profiles)
    if (v.toMap().value("id").toString() == id)
      return v.toMap();
  return {};
}

QVariantMap ProfileStore::save(const QVariantMap &p, const QString &password, bool remember) {
  if (!m_readable) {
    emit failed("Cannot modify an unreadable profiles file.");
    return {};
  }
  QVariantMap clean, previous;
  for (auto k : {"id", "name", "host", "username", "family", "http", "legacyTLS",
                 "allowPlainConsole", "jnlpPath", "clientCert", "clientKey", "consoleMode",
                 "lastConnected"})
    if (p.contains(k))
      clean[k] = p[k];
  clean["host"] = clean.value("host").toString().trimmed();
  if (clean.value("host").toString().isEmpty()) {
    emit failed("Enter a BMC address before saving.");
    return {};
  }
  if (clean.value("id").toString().isEmpty())
    clean["id"] = QUuid::createUuid().toString(QUuid::WithoutBraces);
  auto profiles = m_profiles;
  int index = -1;
  for (int i = 0; i < profiles.size(); ++i)
    if (profiles[i].toMap().value("id") == clean["id"]) {
      index = i;
      previous = profiles[i].toMap();
      break;
    }
  clean["remember"] = remember;
  if (!clean.contains("lastConnected") && previous.contains("lastConnected"))
    clean["lastConnected"] = previous["lastConnected"];
  if (clean.value("name").toString().trimmed().isEmpty())
    clean["name"] = clean["host"];
  if (remember && !password.isEmpty()) {
    if (!m_credentials->write(clean["id"].toString(), password)) {
      emit failed("KWallet is unavailable; the password was not saved.");
      return {};
    }
  } else if (previous.value("remember").toBool() &&
             (!remember || previous.value("host") != clean.value("host") ||
              previous.value("username") != clean.value("username"))) {
    if (!m_credentials->remove(clean["id"].toString())) {
      emit failed("Could not remove the saved password from KWallet.");
      return {};
    }
  }
  if (index >= 0)
    profiles[index] = clean;
  else
    profiles.append(clean);
  if (!persist(profiles))
    return {};
  m_profiles = profiles;
  emit profilesChanged();
  return clean;
}

QVariantMap ProfileStore::duplicate(const QString &id) {
  auto p = profile(id);
  if (p.isEmpty())
    return {};
  p.remove("id");
  p.remove("lastConnected");
  p["name"] = p.value("name").toString() + " (copy)";
  // The copy gets its own wallet entry; it starts without a saved password.
  return save(p, {}, false);
}

bool ProfileStore::remove(const QString &id) {
  if (!m_readable) {
    emit failed("Cannot modify an unreadable profiles file.");
    return false;
  }
  auto profiles = m_profiles;
  for (int i = 0; i < profiles.size(); i++)
    if (profiles[i].toMap().value("id").toString() == id) {
      if (profiles[i].toMap().value("remember").toBool() && !m_credentials->remove(id)) {
        emit failed("Could not remove the saved password from KWallet.");
        return false;
      }
      profiles.removeAt(i);
      if (!persist(profiles))
        return false;
      m_profiles = profiles;
      emit profilesChanged();
      return true;
    }
  return false;
}

QString ProfileStore::savedPassword(const QVariantMap &p) const {
  QString password;
  for (const auto &v : m_profiles) {
    auto saved = v.toMap();
    if (saved.value("id") == p.value("id") && saved.value("host") == p.value("host") &&
        saved.value("username") == p.value("username")) {
      if (!m_credentials->read(p.value("id").toString(), password))
        password.clear();
      break;
    }
  }
  return password;
}

void ProfileStore::stamp(const QString &id) {
  auto profiles = m_profiles;
  for (auto &v : profiles) {
    auto p = v.toMap();
    if (p.value("id").toString() != id)
      continue;
    p["lastConnected"] = QDateTime::currentDateTimeUtc().toString(Qt::ISODate);
    v = p;
    if (persist(profiles)) {
      m_profiles = profiles;
      emit profilesChanged();
    }
    return;
  }
}
