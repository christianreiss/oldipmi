#include "session.h"
#include <QColor>
#include <QCoreApplication>
#include <QDateTime>
#include <QJsonDocument>
#include <QtEndian>
#include <fcntl.h>
#include <sys/socket.h>
#include <unistd.h>

quint64 Session::nextStamp() {
  static quint64 clock = 0;
  return ++clock;
}
Session::Session(QString key, QVariantMap p, QObject *parent)
    : QObject(parent), id(std::move(key)), profile(std::move(p)) {}
Session::~Session() { stop(); }
void Session::setMessage(const QString &m) {
  message = m;
  messageStamp = nextStamp();
}
void Session::fail(const QString &m) {
  setMessage(m);
  emit changed();
}
void Session::failPending(const QString &why) {
  auto pending = std::move(m_pending);
  m_pending.clear();
  for (auto &entry : pending)
    entry.second(false, why);
}
void Session::stop() {
  failPending("Connection closed.");
  lastFrame = QImage();
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
  mounted = false;
}
void Session::begin(const QString &password) {
  consoleInfo.clear();
  errorStage.clear();
  startedAt = QDateTime::currentMSecsSinceEpoch();
  state = "logging_in";
  setMessage("Opening console on " + profile.value("host").toString() + "…");
  emit changed();
  int ctl[2], vid[2];
  if (socketpair(AF_UNIX, SOCK_STREAM | SOCK_CLOEXEC, 0, ctl) < 0) {
    state = "disconnected";
    fail("Cannot create private IPC.");
    return;
  }
  if (socketpair(AF_UNIX, SOCK_STREAM | SOCK_CLOEXEC, 0, vid) < 0) {
    close(ctl[0]);
    close(ctl[1]);
    state = "disconnected";
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
    state = "disconnected";
    fail("Cannot pass private IPC descriptors.");
    return;
  }
  m_control = new QLocalSocket(this);
  m_video = new QLocalSocket(this);
  m_control->setSocketDescriptor(ctl[0], QLocalSocket::ConnectedState, QIODevice::ReadWrite);
  m_video->setSocketDescriptor(vid[0], QLocalSocket::ConnectedState, QIODevice::ReadOnly);
  connect(m_control, &QLocalSocket::readyRead, this, &Session::readControl);
  connect(m_video, &QLocalSocket::readyRead, this, &Session::readVideo);
  m_process = new QProcess(this);
  m_process->setChildProcessModifier([childCtl, childVid] {
    dup2(childCtl, 3);
    dup2(childVid, 4);
    close(childCtl);
    close(childVid);
  });
  connect(m_process, &QProcess::errorOccurred, this, [this](QProcess::ProcessError) {
    state = "disconnected";
    fail("The native backend could not run. Check the application bundle.");
  });
  connect(m_process, qOverload<int, QProcess::ExitStatus>(&QProcess::finished), this,
          [this](int, QProcess::ExitStatus) {
            readControl();
            failPending("Connection ended.");
            lastFrame = QImage();
            state = "disconnected";
            mounted = false;
            emit frameReady(QImage());
            if (message.startsWith("Connected") || message.startsWith("Opening") ||
                message.startsWith("Logging") || message.startsWith("Discovering") ||
                message.startsWith("Connecting"))
              setMessage("Connection ended. Reconnect to obtain a fresh session.");
            emit changed();
          });
  m_process->setProgram(
      qEnvironmentVariable("OLDIPMI_BIN_DIR", QCoreApplication::applicationDirPath()) +
      "/oldipmi-core");
  m_process->start();
  bool started = m_process->waitForStarted(3000);
  close(childCtl);
  close(childVid);
  if (!started)
    return;
  command({{"op", "connect"},
           {"profile", QJsonObject::fromVariantMap(profile)},
           {"password", password}});
}
void Session::command(QJsonObject o, Result done) {
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
void Session::readControl() {
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
    // A reply carries its command's id; only an event's error is the console's.
    const bool consoleError = o["type"] == "error" && o.value("id").toInt() == 0;
    if (consoleError && state != "connected" && state != "disconnected")
      errorStage = state;
    // A console that never delivered a picture failed while connecting.
    if (consoleError && state == "connected" && lastFrame.isNull())
      errorStage = "connecting";
    if (o.contains("state"))
      state = o["state"].toString();
    if (o["type"] == "error")
      setMessage(o["message"].toString());
    else if (o["state"] == "logging_in")
      setMessage("Logging in…");
    else if (o["state"] == "discovering")
      setMessage("Discovering console…");
    else if (o["state"] == "connecting")
      setMessage("Connecting to console…");
    else if (o["state"] == "connected")
      setMessage("Connected — click the console to control it.");
    if (o.contains("info")) {
      auto resolution = consoleInfo.value("resolution");
      consoleInfo = o["info"].toObject().toVariantMap();
      if (resolution.isValid())
        consoleInfo["resolution"] = resolution;
    }
    if (o.contains("mounted"))
      mounted = o["mounted"].toBool();
    if (int id = o.value("id").toInt(); id && m_pending.count(id) &&
        (o["type"] == "ok" || o["type"] == "error")) {
      auto done = std::move(m_pending[id]);
      m_pending.erase(id);
      done(o["type"] == "ok", o["message"].toString());
    }
    emit changed();
  }
}
void Session::readVideo() {
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
    latest = QImage(reinterpret_cast<const uchar *>(m_videoBuffer.constData() + 12), w, h, w * 4,
                    QImage::Format_RGBA8888)
                 .copy();
    m_videoBuffer.remove(0, 4 + n);
  }
  if (!latest.isNull()) {
    auto resolution = QString::number(latest.width()) + " × " + QString::number(latest.height());
    if (consoleInfo.value("resolution").toString() != resolution) {
      consoleInfo["resolution"] = resolution;
      emit changed();
    }
    size_t hash = qHashBits(latest.constBits(), size_t(latest.sizeInBytes()));
    if (lastFrame.isNull() || hash != m_frameHash || latest.size() != lastFrame.size()) {
      m_frameHash = hash;
      ++changeSeq;
      sinceChange.restart();
    }
    ++frameSeq;
    bool first = lastFrame.isNull();
    lastFrame = latest;
    emit frameReady(latest);
    if (first)
      emit changed();
  }
}
void Session::key(int usage, bool down) {
  if (connected())
    command({{"op", "key"}, {"input", QJsonObject{{"usage", usage}, {"down", down}}}});
}
void Session::pointer(int x, int y, int buttons, int wheel) {
  if (connected())
    command({{"op", "pointer"},
             {"input", QJsonObject{{"x", x}, {"y", y}, {"buttons", buttons}, {"wheel", wheel}}}});
}
void Session::releaseInput() {
  if (connected())
    command({{"op", "release"}});
}
void Session::sendKeys(const QVariantList &usages) {
  // Press in order, release in reverse: a chord such as Ctrl+Alt+Del.
  if (!connected())
    return;
  releaseInput();
  for (const auto &u : usages)
    key(u.toInt(), true);
  for (auto it = usages.crbegin(); it != usages.crend(); ++it)
    key(it->toInt(), false);
}
void Session::preview() {
  startedAt = QDateTime::currentMSecsSinceEpoch();
  QImage im(800, 600, QImage::Format_RGBA8888);
  im.fill(QColor("#223548")); // slop-ok: synthetic framebuffer content, not UI chrome
  state = "connected"; // a framebuffer without a backend; input and power go nowhere
  lastFrame = im;
  ++frameSeq;
  ++changeSeq;
  sinceChange.restart();
  emit frameReady(im);
  setMessage("Desktop preview — no BMC connection");
  emit changed();
}
