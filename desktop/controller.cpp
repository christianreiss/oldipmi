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
Controller::~Controller() { stop(); }
void Controller::fail(QString message) {
  m_message = message;
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
  m_errorStage.clear();
  m_message.clear();
  emit changed();
}
void Controller::sendKeys(QVariantList usages) {
  // Press in order, release in reverse: a chord such as Ctrl+Alt+Del.
  if (m_state != "connected")
    return;
  releaseInput();
  for (const auto &u : usages)
    key(u.toInt(), true);
  for (auto it = usages.crbegin(); it != usages.crend(); ++it)
    key(it->toInt(), false);
}
void Controller::failPending(const QString &why) {
  auto pending = std::move(m_pending);
  m_pending.clear();
  for (auto &entry : pending)
    entry.second(false, why);
}
void Controller::agentNote(QString message) {
  m_message = message;
  emit changed();
}
void Controller::stop() {
  failPending("Connection closed.");
  m_lastFrame = QImage();
  if (m_process) {
    disconnect(m_process, nullptr, this, nullptr);
    if (m_control)
      command({{"op", "disconnect"}});
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
  m_controlBuffer.clear();
  m_videoBuffer.clear();
  m_mounted = false;
}
void Controller::connectHost(QVariantMap p, QString password) {
  if (m_state != "disconnected")
    return;
  m_consoleInfo.clear();
  m_errorStage.clear();
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
    m_message.clear();
    emit changed();
  }
}
void Controller::start(QVariantMap p, QString password, QString jnlp) {
  stop();
  emit frameReady(QImage());
  m_consoleInfo.clear();
  m_errorStage.clear();
  m_activeProfile = p;
  m_state = jnlp.isEmpty() ? "logging_in" : "discovering";
  m_message = "Opening console on " + p.value("host").toString() + "…";
  emit changed();
  int ctl[2], vid[2];
  if (socketpair(AF_UNIX, SOCK_STREAM | SOCK_CLOEXEC, 0, ctl) < 0) {
    m_state = "disconnected";
    fail("Cannot create private IPC.");
    return;
  }
  if (socketpair(AF_UNIX, SOCK_STREAM | SOCK_CLOEXEC, 0, vid) < 0) {
    close(ctl[0]);
    close(ctl[1]);
    m_state = "disconnected";
    fail("Cannot create video IPC.");
    return;
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
    m_state = "disconnected";
    fail("Cannot pass private IPC descriptors.");
    return;
  }
  m_control = new QLocalSocket(this);
  m_video = new QLocalSocket(this);
  m_control->setSocketDescriptor(ctl[0], QLocalSocket::ConnectedState,
                                 QIODevice::ReadWrite);
  m_video->setSocketDescriptor(vid[0], QLocalSocket::ConnectedState,
                               QIODevice::ReadOnly);
  connect(m_control, &QLocalSocket::readyRead, this, &Controller::readControl);
  connect(m_video, &QLocalSocket::readyRead, this, &Controller::readVideo);
  m_process = new QProcess(this);
  m_process->setChildProcessModifier([childCtl, childVid] {
    dup2(childCtl, 3);
    dup2(childVid, 4);
    close(childCtl);
    close(childVid);
  });
  connect(
      m_process, &QProcess::errorOccurred, this,
      [this](QProcess::ProcessError) {
        m_state = "disconnected";
        fail("The native backend could not run. Check the application bundle.");
      });
  connect(m_process, qOverload<int, QProcess::ExitStatus>(&QProcess::finished),
          this, [this](int, QProcess::ExitStatus) {
            readControl();
            failPending("Connection ended.");
            m_lastFrame = QImage();
            m_state = "disconnected";
            m_mounted = false;
            emit frameReady(QImage());
            if (m_message.startsWith("Connected") ||
                m_message.startsWith("Opening") ||
                m_message.startsWith("Logging") || m_message.startsWith("Discovering") ||
                m_message.startsWith("Connecting"))
              m_message =
                  "Connection ended. Reconnect to obtain a fresh session.";
            emit changed();
          });
  m_process->setProgram(
      qEnvironmentVariable("OLDIPMI_BIN_DIR",
                           QCoreApplication::applicationDirPath()) +
      "/oldipmi-core");
  m_process->start();
  bool started = m_process->waitForStarted(3000);
  close(childCtl);
  close(childVid);
  if (!started)
    return;
  command({{"op", "connect"},
           {"profile", QJsonObject::fromVariantMap(p)},
           {"password", password},
           {"jnlp", jnlp}});
}
void Controller::disconnectHost() {
  // Nothing open and no password prompt: only dismiss a shown failure and keep
  // the last message, instead of reporting a cancellation that did not happen.
  if (m_state == "disconnected" && m_pendingProfile.isEmpty()) {
    if (!m_errorStage.isEmpty() || !m_activeProfile.isEmpty()) {
      m_errorStage.clear();
      m_activeProfile.clear();
      emit changed();
    }
    return;
  }
  bool cancelled = m_state != "connected";
  cancelCredentials();
  releaseInput();
  stop();
  m_state = "disconnected";
  m_errorStage.clear();
  m_activeProfile.clear();
  m_message = cancelled ? "Connection cancelled." : "Disconnected.";
  emit frameReady(QImage());
  emit changed();
}
void Controller::command(QJsonObject o, Result done) {
  if (!m_control || m_control->state() != QLocalSocket::ConnectedState) {
    if (done)
      done(false, "Not connected.");
    return;
  }
  o["version"] = 1;
  o["id"] = ++m_id;
  if (done)
    m_pending[m_id] = std::move(done);
  QByteArray b = QJsonDocument(o).toJson(QJsonDocument::Compact);
  QByteArray h(4, 0);
  qToBigEndian<quint32>(b.size(), h.data());
  if (m_control->bytesToWrite() > 1024 * 1024) {
    fail("Input queue is full; disconnect and reconnect.");
    m_process->terminate();
    failPending("Input queue is full.");
    return;
  }
  m_control->write(h + b);
  m_control->flush();
}
void Controller::readControl() {
  if (!m_control)
    return;
  m_controlBuffer += m_control->readAll();
  while (m_controlBuffer.size() >= 4) {
    quint32 n = qFromBigEndian<quint32>(m_controlBuffer.constData());
    if (n == 0 || n > 1024 * 1024) {
      fail("Invalid backend control message.");
      m_process->terminate();
      return;
    }
    if (m_controlBuffer.size() < 4 + n)
      return;
    auto o = QJsonDocument::fromJson(m_controlBuffer.mid(4, n)).object();
    m_controlBuffer.remove(0, 4 + n);
    if (o.value("version").toInt() != 1) {
      fail("Incompatible backend version.");
      m_process->terminate();
      return;
    }
    if (o["type"] == "error" && m_state != "connected" && m_state != "disconnected")
      m_errorStage = m_state;
    // A console that never delivered a picture failed while connecting.
    if (o["type"] == "error" && m_state == "connected" && m_lastFrame.isNull())
      m_errorStage = "connecting";
    if (o.contains("state"))
      m_state = o["state"].toString();
    if (o["type"] == "error")
      m_message = o["message"].toString();
    else if (o["state"] == "logging_in")
      m_message = "Logging in…";
    else if (o["state"] == "discovering")
      m_message = "Discovering console…";
    else if (o["state"] == "connecting")
      m_message = "Connecting to console…";
    else if (o["state"] == "connected")
      m_message = "Connected — click the console to control it.";
    if (o.contains("info")) {
      auto resolution = m_consoleInfo.value("resolution");
      m_consoleInfo = o["info"].toObject().toVariantMap();
      if (resolution.isValid())
        m_consoleInfo["resolution"] = resolution;
    }
    if (o.contains("mounted"))
      m_mounted = o["mounted"].toBool();
    if (int id = o.value("id").toInt(); id && m_pending.count(id) &&
        (o["type"] == "ok" || o["type"] == "error")) {
      auto done = std::move(m_pending[id]);
      m_pending.erase(id);
      done(o["type"] == "ok", o["message"].toString());
    }
    emit changed();
  }
}
void Controller::readVideo() {
  if (!m_video)
    return;
  m_videoBuffer += m_video->readAll();
  QImage latest;
  while (m_videoBuffer.size() >= 12) {
    quint32 n = qFromBigEndian<quint32>(m_videoBuffer.constData()),
            w = qFromBigEndian<quint32>(m_videoBuffer.constData() + 4),
            h = qFromBigEndian<quint32>(m_videoBuffer.constData() + 8);
    if (w == 0 || h == 0 || w > 4096 || h > 2160 || n != 8 + w * h * 4) {
      fail("Invalid framebuffer from backend.");
      m_process->terminate();
      return;
    }
    if (m_videoBuffer.size() < 4 + n)
      break;
    latest =
        QImage(reinterpret_cast<const uchar *>(m_videoBuffer.constData() + 12),
               w, h, w * 4, QImage::Format_RGBA8888)
            .copy();
    m_videoBuffer.remove(0, 4 + n);
  }
  if (!latest.isNull()) {
    auto resolution = QString::number(latest.width()) + " × " + QString::number(latest.height());
    if (m_consoleInfo.value("resolution").toString() != resolution) {
      m_consoleInfo["resolution"] = resolution;
      emit changed();
    }
    size_t hash = qHashBits(latest.constBits(), size_t(latest.sizeInBytes()));
    if (m_lastFrame.isNull() || hash != m_frameHash || latest.size() != m_lastFrame.size()) {
      m_frameHash = hash;
      ++m_changeSeq;
      m_sinceChange.restart();
    }
    ++m_frameSeq;
    bool first = m_lastFrame.isNull();
    m_lastFrame = latest;
    emit frameReady(latest);
    if (first)
      emit changed();
  }
}
void Controller::key(int usage, bool down) {
  if (m_state == "connected")
    command({{"op", "key"},
             {"input", QJsonObject{{"usage", usage}, {"down", down}}}});
}
void Controller::pointer(int x, int y, int buttons, int wheel) {
  if (m_state == "connected")
    command(
        {{"op", "pointer"},
         {"input",
          QJsonObject{
              {"x", x}, {"y", y}, {"buttons", buttons}, {"wheel", wheel}}}});
}
void Controller::releaseInput() {
  if (m_state == "connected")
    command({{"op", "release"}});
}
void Controller::mountISO() {
  releaseInput();
  auto f = QFileDialog::getOpenFileName(nullptr, "Mount read-only ISO", {},
                                        "ISO images (*.iso)");
  if (!f.isEmpty())
    command({{"op", "mount"}, {"path", f}});
}
void Controller::ejectISO() { command({{"op", "eject"}}); }
void Controller::power(QString action) {
  releaseInput();
  command({{"op", "power"}, {"action", action}});
}
void Controller::openJNLP(QVariantMap p) {
  auto name = QFileDialog::getOpenFileName(nullptr, "Open fresh BMC session",
                                           {}, "JNLP session (*.jnlp)");
  if (name.isEmpty())
    return;
  QFile f(name);
  if (!f.open(QIODevice::ReadOnly) || f.size() > 1024 * 1024) {
    fail("Cannot read JNLP file, or file exceeds 1 MiB.");
    return;
  }
  start(p, {}, QString::fromUtf8(f.readAll()));
}
QString Controller::chooseCertificate(bool key) {
  return QFileDialog::getOpenFileName(
      nullptr, key ? "Client private key" : "Client certificate", {},
      "PEM files (*.pem *.crt *.key)");
}
void Controller::preview() {
  QImage im(800, 600, QImage::Format_RGBA8888);
  im.fill(QColor("#223548")); // slop-ok: synthetic framebuffer content, not UI chrome
  m_lastFrame = im;
  ++m_frameSeq;
  ++m_changeSeq;
  m_sinceChange.restart();
  emit frameReady(im);
  m_activeProfile = {{"id", ""}, {"name", "Preview"}, {"host", "synthetic framebuffer"}};
  m_message = "Desktop preview — no BMC connection";
  emit changed();
}
