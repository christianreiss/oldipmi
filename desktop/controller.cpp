#include "controller.h"
#include <QApplication>
#include <QDir>
#include <QFile>
#include <QFileDialog>
#include <QJsonArray>
#include <QJsonDocument>
#include <QSaveFile>
#include <QStandardPaths>
#include <QTimer>
#include <QUuid>
#include <QDateTime>
#include <QtEndian>
#include <fcntl.h>
#include <memory>
#include <sys/socket.h>
#include <unistd.h>

static QString configPath() {
  return QStandardPaths::writableLocation(QStandardPaths::AppConfigLocation) +
         "/hosts.json";
}
Controller::Controller(QObject *p, Credentials *credentials, QString configFile) : QObject(p) {
  m_configFile = configFile.isEmpty() ? configPath() : configFile;
  if (!credentials)
    m_ownedCredentials = kdeCredentials();
  m_credentials = credentials ? credentials : m_ownedCredentials.get();
  loadProfiles();
  connect(qApp, &QGuiApplication::applicationStateChanged, this,
          [this](Qt::ApplicationState s) {
            if (s != Qt::ApplicationActive)
              releaseInput();
          });
}
Controller::~Controller() {
  m_shown = nullptr;
  for (auto *s : m_sessions.values()) {
    disconnect(s, nullptr, this, nullptr);
    delete s; // stops the backend
  }
}
void Controller::setMessage(const QString &m) {
  m_message = m;
  m_messageStamp = Session::nextStamp();
}
void Controller::fail(QString message) {
  setMessage(message);
  emit changed();
}
QString Controller::message() const {
  // Whichever was written last: a session's own status or an app-level notice.
  return m_shown && m_shown->messageStamp > m_messageStamp ? m_shown->message : m_message;
}
QVariantMap Controller::sessions() const {
  QVariantMap out;
  for (auto *s : m_sessions)
    out[s->id] = QVariantMap{{"state", s->state}, {"errorStage", s->errorStage},
                             {"message", s->message}, {"startedAt", s->startedAt}};
  return out;
}
QStringList Controller::openIds() const {
  QStringList out;
  for (auto *s : m_sessions)
    if (s->state != "disconnected")
      out << s->id;
  out.sort();
  return out;
}
int Controller::openCount() const { return int(openIds().size()); }
bool Controller::isOpen(QString id) const {
  auto *s = m_sessions.value(id);
  return s && s->state != "disconnected";
}
void Controller::setShown(Session *s) {
  if (s == m_shown)
    return;
  if (m_shown)
    m_shown->releaseInput(); // nothing stays held on a console that is no longer in front
  m_shown = s;
  emit frameReady(s ? s->lastFrame : QImage());
  emit changed();
}
void Controller::show(QString id) { setShown(m_sessions.value(id)); }
void Controller::removeSession(Session *s) {
  if (!s)
    return;
  m_sessions.remove(s->id);
  if (s == m_shown) {
    m_shown = nullptr;
    emit frameReady(QImage());
  }
  s->releaseInput();
  disconnect(s, nullptr, this, nullptr);
  delete s; // stops the backend
}
void Controller::disconnectAll() {
  cancelCredentials();
  auto all = m_sessions.values();
  for (auto *s : all)
    removeSession(s);
  emit changed();
}
void Controller::loadProfiles() {
  QFile f(m_configFile);
  bool initialized = false;
  if (f.exists()) {
    if (!f.open(QIODevice::ReadOnly) || f.size() > 1024 * 1024) {
      m_profilesReadable = false;
    } else {
      auto doc = QJsonDocument::fromJson(f.readAll());
      if (doc.isArray()) {
        m_hosts = doc.array().toVariantList();
        // Preserve the old array format for rollback; never replace a backup.
        if (!QFile::exists(m_configFile + ".pre-profiles") &&
            !f.copy(m_configFile + ".pre-profiles"))
          m_profilesReadable = false;
      } else if (doc.isObject() && doc["version"].toInt() == 1 &&
                 doc["profiles"].isArray()) {
        m_hosts = doc["profiles"].toArray().toVariantList();
        initialized = doc["defaultsVersion"].toInt() >= 1;
      } else {
        m_profilesReadable = false;
      }
    }
  }
  if (!m_profilesReadable) {
    fail("Could not read or migrate profiles safely; the existing file was preserved.");
    return;
  }
  if (initialized)
    return;
  auto profiles = m_hosts;
  for (const auto &name : {QString("Lab-AMI"), QString("Lab-Avocent"), QString("Lab-Cisco")}) {
    QString host = name.toLower() + ".example.net";
    bool exists = false;
    for (const auto &v : profiles)
      if (v.toMap().value("host").toString().trimmed().compare(host, Qt::CaseInsensitive) == 0)
        exists = true;
    if (exists)
      continue;
    bool ami = name == "Lab-AMI";
    profiles.append(QVariantMap{
      {"id", QUuid::createUuid().toString(QUuid::WithoutBraces)},
      {"name", name}, {"host", host}, {"username", "admin"},
      {"family", ami ? "ami" : name == "Lab-Avocent" ? "avocent" : "cisco"},
      {"remember", true}, {"http", ami}, {"legacyTLS", true},
      {"allowPlainConsole", ami}});
  }
  if (persist(profiles))
    m_hosts = profiles;
}
bool Controller::persist(const QVariantList &hosts) {
  if (!m_profilesReadable) {
    fail("Profiles were not saved because the existing file could not be read safely.");
    return false;
  }
  QDir().mkpath(QFileInfo(m_configFile).absolutePath());
  QSaveFile f(m_configFile);
  if (!f.open(QIODevice::WriteOnly)) {
    fail("Could not save host profiles.");
    return false;
  }
  f.setPermissions(QFile::ReadOwner | QFile::WriteOwner);
  auto data = QJsonDocument(QJsonObject{{"version", 1}, {"defaultsVersion", 1},
               {"profiles", QJsonArray::fromVariantList(hosts)}}).toJson();
  if (f.write(data) != data.size() || !f.commit()) {
    fail("Could not save host profiles.");
    return false;
  }
  return true;
}
QVariantMap Controller::saveHost(QVariantMap p, QString password, bool remember) {
  if (!m_profilesReadable) {
    fail("Cannot modify an unreadable profiles file.");
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
    fail("Enter a BMC address before saving.");
    return {};
  }
  if (clean.value("id").toString().isEmpty())
    clean["id"] = QUuid::createUuid().toString(QUuid::WithoutBraces);
  auto profiles = m_hosts;
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
      fail("KWallet is unavailable; the password was not saved.");
      return {};
    }
  } else if (previous.value("remember").toBool() &&
             (!remember || previous.value("host") != clean.value("host") ||
              previous.value("username") != clean.value("username"))) {
    if (!m_credentials->remove(clean["id"].toString())) {
      fail("Could not remove the saved password from KWallet.");
      return {};
    }
  }
  if (index >= 0)
    profiles[index] = clean;
  else
    profiles.append(clean);
  if (!persist(profiles))
    return {};
  m_hosts = profiles;
  emit hostsChanged();
  m_message = "Profile saved.";
  emit changed();
  return clean;
}
bool Controller::removeHost(QString id) {
  if (!m_profilesReadable) {
    fail("Cannot modify an unreadable profiles file.");
    return false;
  }
  auto profiles = m_hosts;
  for (int i = 0; i < profiles.size(); i++)
    if (profiles[i].toMap().value("id").toString() == id) {
      if (profiles[i].toMap().value("remember").toBool() && !m_credentials->remove(id)) {
        fail("Could not remove the saved password from KWallet.");
        return false;
      }
      profiles.removeAt(i);
      if (!persist(profiles))
        return false;
      m_hosts = profiles;
      emit hostsChanged();
      return true;
    }
  return false;
}
QVariantMap Controller::host(QString id) const {
  for (const auto &v : m_hosts)
    if (v.toMap().value("id").toString() == id)
      return v.toMap();
  return {};
}
QVariantMap Controller::duplicateHost(QString id) {
  auto p = host(id);
  if (p.isEmpty())
    return {};
  p.remove("id");
  p.remove("lastConnected");
  p["name"] = p.value("name").toString() + " (copy)";
  // The copy gets its own wallet entry; it starts without a saved password.
  auto saved = saveHost(p, {}, false);
  if (!saved.isEmpty()) {
    m_message = "Duplicated as \"" + saved.value("name").toString() + "\".";
    emit changed();
  }
  return saved;
}
void Controller::stamp(const QString &id) {
  auto profiles = m_hosts;
  for (auto &v : profiles) {
    auto p = v.toMap();
    if (p.value("id").toString() != id)
      continue;
    p["lastConnected"] = QDateTime::currentDateTimeUtc().toString(Qt::ISODate);
    v = p;
    if (persist(profiles)) {
      m_hosts = profiles;
      emit hostsChanged();
    }
    return;
  }
}
void Controller::clearMessage() {
  if (m_shown) {
    m_shown->errorStage.clear();
    m_shown->message.clear();
  }
  m_message.clear();
  emit changed();
}
void Controller::sendKeys(QVariantList usages) {
  if (m_shown)
    m_shown->sendKeys(usages);
}
void Controller::agentNote(QString message) {
  setMessage(message);
  emit changed();
}
void Controller::connectHost(QVariantMap p, QString password) {
  // Already open (or being set up): bring it to the front instead of opening a second one.
  if (auto id = p.value("id").toString(); !id.isEmpty() && isOpen(id)) {
    show(id);
    return;
  }
  m_pendingProfile = p;
  m_pendingPassword.clear();
  bool entered = !password.isEmpty();
  // The synthetic "demo" console (internal/demo) needs no password and never touches KWallet.
  bool demo = p.value("family").toString() == "demo";
  if (!entered && !demo && p.value("remember").toBool()) {
    // Do not reuse a stored credential after changing its host or account.
    for (const auto &v : m_hosts) {
      auto saved = v.toMap();
      if (saved.value("id") == p.value("id") && saved.value("host") == p.value("host") &&
          saved.value("username") == p.value("username")) {
        if (!m_credentials->read(p.value("id").toString(), password))
          password.clear();
        break;
      }
    }
  }
  if (password.isEmpty() && !demo) {
    fail("Enter the password for " + p.value("username").toString() + "@" +
         p.value("host").toString() + " to open its console.");
    emit passwordRequired();
    return;
  }
  // Saving before connection keeps edited settings attached to the same UUID.
  auto saved = saveHost(p, entered ? password : QString(), p.value("remember").toBool());
  if (saved.isEmpty()) {
    m_pendingPassword = password;
    emit walletUnavailable();
    return;
  }
  m_pendingProfile.clear();
  emit profileActivated(saved);
  start(saved, password);
  stamp(saved.value("id").toString());
}
void Controller::continueWithoutWallet() {
  if (m_pendingPassword.isEmpty())
    return;
  auto profile = m_pendingProfile;
  auto password = m_pendingPassword;
  cancelCredentials();
  start(profile, password);
}
void Controller::cancelCredentials() {
  m_pendingPassword.clear();
  m_pendingProfile.clear();
  if (m_message.startsWith("Enter the password")) {
    setMessage({});
    emit changed();
  }
}
void Controller::start(QVariantMap p, QString password) {
  auto id = p.value("id").toString();
  if (id.isEmpty())
    id = QUuid::createUuid().toString(QUuid::WithoutBraces);
  if (auto *old = m_sessions.value(id))
    removeSession(old); // a failed or ended session of this profile makes way for the new one
  auto *s = new Session(id, p, this);
  connect(s, &Session::changed, this, &Controller::changed);
  connect(s, &Session::frameReady, this, [this, s](QImage im) {
    if (s == m_shown)
      emit frameReady(im);
  });
  m_sessions[id] = s;
  setShown(s);
  s->begin(password);
}
void Controller::disconnectHost() { disconnectProfile(shownId()); }
void Controller::disconnectProfile(QString id) {
  auto *s = m_sessions.value(id);
  bool promptIsOurs = !m_pendingProfile.isEmpty() &&
                      (id.isEmpty() || m_pendingProfile.value("id").toString() == id);
  // Nothing open and no password prompt: only dismiss a shown failure and keep
  // the last message, instead of reporting a cancellation that did not happen.
  if (!promptIsOurs && (!s || s->state == "disconnected")) {
    if (s) {
      setMessage(s->message); // the failure text stays visible after its session is gone
      removeSession(s);
      emit changed();
    }
    return;
  }
  bool cancelled = !s || s->state != "connected";
  if (promptIsOurs)
    cancelCredentials();
  removeSession(s);
  setMessage(cancelled ? "Connection cancelled." : "Disconnected.");
  emit changed();
}
void Controller::key(int usage, bool down) {
  if (m_shown)
    m_shown->key(usage, down);
}
void Controller::pointer(int x, int y, int buttons, int wheel) {
  if (m_shown)
    m_shown->pointer(x, y, buttons, wheel);
}
void Controller::releaseInput() {
  if (m_shown)
    m_shown->releaseInput();
}
void Controller::mountISO() {
  releaseInput();
  auto f = QFileDialog::getOpenFileName(nullptr, "Mount read-only ISO", {},
                                        "ISO images (*.iso)");
  if (!f.isEmpty() && m_shown)
    m_shown->command({{"op", "mount"}, {"path", f}});
}
void Controller::ejectISO() {
  if (m_shown)
    m_shown->command({{"op", "eject"}});
}
void Controller::power(QString action) {
  releaseInput();
  if (m_shown)
    m_shown->command({{"op", "power"}, {"action", action}});
}
void Controller::powerFor(QString id, QString action) {
  auto *s = id.isEmpty() ? m_shown : m_sessions.value(id);
  if (!s)
    return;
  s->releaseInput();
  s->command({{"op", "power"}, {"action", action}});
}
QString Controller::chooseCertificate(bool key) {
  return QFileDialog::getOpenFileName(
      nullptr, key ? "Client private key" : "Client certificate", {},
      "PEM files (*.pem *.crt *.key)");
}
void Controller::preview() {
  auto *s = new Session("", {{"id", ""}, {"name", "Preview"}, {"host", "synthetic framebuffer"}}, this);
  connect(s, &Session::changed, this, &Controller::changed);
  connect(s, &Session::frameReady, this, [this, s](QImage im) {
    if (s == m_shown)
      emit frameReady(im);
  });
  if (auto *old = m_sessions.value(s->id))
    removeSession(old);
  m_sessions[s->id] = s;
  setShown(s);
  s->preview();
}
