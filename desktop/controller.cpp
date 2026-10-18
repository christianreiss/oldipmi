#include "controller.h"
#include <KWallet>
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
#include <QtEndian>
#include <fcntl.h>
#include <memory>
#include <sys/socket.h>
#include <unistd.h>

static QString configPath() {
  return QStandardPaths::writableLocation(QStandardPaths::AppConfigLocation) +
         "/hosts.json";
}
Controller::Controller(QObject *p) : QObject(p) {
  QFile f(configPath());
  if (f.open(QIODevice::ReadOnly) && f.size() <= 1024 * 1024) {
    auto doc = QJsonDocument::fromJson(f.readAll());
    if (doc.isArray())
      m_hosts = doc.array().toVariantList();
  }
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
void Controller::persist() {
  QDir().mkpath(QFileInfo(configPath()).absolutePath());
  QSaveFile f(configPath());
  if (!f.open(QIODevice::WriteOnly)) {
    fail("Could not save host profiles.");
    return;
  }
  f.setPermissions(QFile::ReadOwner | QFile::WriteOwner);
  f.write(QJsonDocument(QJsonArray::fromVariantList(m_hosts)).toJson());
  if (!f.commit())
    fail("Could not save host profiles.");
}
static std::unique_ptr<KWallet::Wallet> wallet() {
  return std::unique_ptr<KWallet::Wallet>(KWallet::Wallet::openWallet(
      KWallet::Wallet::NetworkWallet(), 0, KWallet::Wallet::Synchronous));
}
QString Controller::walletPassword(QString id) {
  auto w = wallet();
  QString password;
  if (!w || !w->setFolder("OldIPMI") || w->readPassword(id, password) != 0)
    fail("KWallet did not provide a password. Enter it for this connection.");
  return password;
}
bool Controller::walletWrite(QString id, QString password) {
  auto w = wallet();
  if (!w) {
    fail("KWallet is unavailable; the password was not saved.");
    return false;
  }
  if (!w->hasFolder("OldIPMI") && !w->createFolder("OldIPMI")) {
    fail("Cannot create the OldIPMI wallet folder.");
    return false;
  }
  if (!w->setFolder("OldIPMI") || w->writePassword(id, password) != 0) {
    fail("Could not save the password in KWallet.");
    return false;
  }
  return true;
}
QVariantMap Controller::saveHost(QVariantMap p, QString password,
                                 bool remember) {
  // Whitelist persisted fields. Passwords and JNLP tokens never enter this
  // file.
  QVariantMap clean;
  for (auto k :
       {"id", "name", "host", "username", "family", "http", "legacyTLS",
        "allowPlainConsole", "jnlpPath", "clientCert", "clientKey"})
    if (p.contains(k))
      clean[k] = p[k];
  if (clean.value("host").toString().trimmed().isEmpty()) {
    fail("Enter a BMC address before saving.");
    return p;
  }
  if (clean.value("id").toString().isEmpty())
    clean["id"] = QUuid::createUuid().toString(QUuid::WithoutBraces);
  clean["remember"] = false;
  if (remember) {
    if (password.isEmpty()) {
      fail("Enter the password before saving it to KWallet.");
      return p;
    }
    if (!walletWrite(clean["id"].toString(), password))
      return p;
    clean["remember"] = true;
  }
  for (int i = 0; i < m_hosts.size(); i++)
    if (m_hosts[i].toMap().value("id") == clean["id"]) {
      if (!remember && m_hosts[i].toMap().value("remember").toBool()) {
        auto w = wallet();
        if (!w || !w->setFolder("OldIPMI") ||
            w->removeEntry(clean["id"].toString()) != 0) {
          fail("Could not remove the saved password from KWallet.");
          return p;
        }
      }
      m_hosts.removeAt(i);
      break;
    }
  m_hosts.append(clean);
  persist();
  emit hostsChanged();
  return clean;
}
void Controller::removeHost(QString id) {
  for (int i = 0; i < m_hosts.size(); i++)
    if (m_hosts[i].toMap().value("id").toString() == id) {
      if (m_hosts[i].toMap().value("remember").toBool()) {
        auto w = wallet();
        if (!w || !w->setFolder("OldIPMI") || w->removeEntry(id) != 0) {
          fail("Could not remove the saved password from KWallet.");
          return;
        }
      }
      m_hosts.removeAt(i);
      persist();
      emit hostsChanged();
      return;
    }
}
void Controller::stop() {
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
  if (password.isEmpty() && p.value("remember").toBool())
    password = walletPassword(p.value("id").toString());
  if (password.isEmpty()) {
    fail("Enter a password to connect.");
    return;
  }
  start(p, password);
}
void Controller::start(QVariantMap p, QString password, QString jnlp) {
  stop();
  emit frameReady(QImage());
  m_state = "connecting";
  m_message = "Connecting to " + p.value("host").toString() + "…";
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
            m_state = "disconnected";
            m_mounted = false;
            emit frameReady(QImage());
            if (m_message.startsWith("Connected") ||
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
  releaseInput();
  stop();
  m_state = "disconnected";
  m_message = "Disconnected.";
  emit frameReady(QImage());
  emit changed();
}
void Controller::command(QJsonObject o) {
  if (!m_control || m_control->state() != QLocalSocket::ConnectedState)
    return;
  o["version"] = 1;
  o["id"] = ++m_id;
  QByteArray b = QJsonDocument(o).toJson(QJsonDocument::Compact);
  QByteArray h(4, 0);
  qToBigEndian<quint32>(b.size(), h.data());
  if (m_control->bytesToWrite() > 1024 * 1024) {
    fail("Input queue is full; disconnect and reconnect.");
    m_process->terminate();
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
    if (o.contains("state"))
      m_state = o["state"].toString();
    if (o["type"] == "error")
      m_message = o["message"].toString();
    else if (o["state"] == "connected")
      m_message = "Connected — click the console to control it.";
    if (o.contains("mounted"))
      m_mounted = o["mounted"].toBool();
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
  if (!latest.isNull())
    emit frameReady(latest);
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
  im.fill(QColor("#223548"));
  emit frameReady(im);
  m_message = "Desktop preview — no BMC connection";
  emit changed();
}
