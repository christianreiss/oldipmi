#include "session.h"
#include <QColor>
#include <QCoreApplication>
#include <QDateTime>
#include <QTimer>
#include <fcntl.h>
#include <sys/socket.h>
#include <unistd.h>

// The message of a console that is ready, and the start of the notice of an
// ISO that went away by itself, which a new mount retires.
static const QString connectedText = "Connected — click the console to control it.";
static const QString isoGoneText = "The ISO is no longer mounted: ";

quint64 Session::nextStamp() {
  static quint64 clock = 0;
  return ++clock;
}
Session::Session(QString key, QVariantMap p, QObject *parent)
    : QObject(parent), id(std::move(key)), profile(std::move(p)) {}
Session::~Session() {
  // What a callback uses may be going away too, as at quit, when the agent API
  // goes before the sessions (C-07). So none runs from here: each gets its
  // answer from the event loop, and only while its context exists.
  for (auto &entry : std::exchange(m_pending, {}))
    if (const QObject *context = entry.second.context)
      QTimer::singleShot(0, context, [done = std::move(entry.second.done)] { done(false, "Connection closed."); });
  stop();
}
void Session::settle(const Pending &p, bool ok, const QString &message) {
  if (p.context && p.done)
    p.done(ok, message);
}
void Session::setMessage(const QString &m) {
  message = m;
  errorCode.clear(); // a class belongs to the message it came with
  messageStamp = nextStamp();
}
void Session::fail(const QString &m) {
  setMessage(m);
  emit changed();
}
void Session::failPending(const QString &why) {
  for (const auto &entry : std::exchange(m_pending, {}))
    settle(entry.second, false, why);
}
void Session::sendDisconnect() {
  if (m_process && m_control && !m_disconnectSent) {
    m_disconnectSent = true;
    command({{"op", ipc::OpDisconnect}});
  }
}
void Session::stop() {
  failPending("Connection closed.");
  lastFrame = QImage();
  if (m_process) {
    disconnect(m_process, nullptr, this, nullptr);
    sendDisconnect();
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
  m_controlIn.clear();
  m_videoIn.clear();
  mounted = false;
}
void Session::begin(const QString &password) {
  m_disconnectSent = false;
  consoleInfo.clear();
  errorStage.clear();
  startedAt = QDateTime::currentMSecsSinceEpoch();
  state = ipc::StageLoggingIn;
  setMessage("Opening console on " + profile.value("host").toString() + "…");
  emit changed();
  int ctl[2], vid[2];
  if (socketpair(AF_UNIX, SOCK_STREAM | SOCK_CLOEXEC, 0, ctl) < 0) {
    state = ipc::StateDisconnected;
    fail("Cannot create private IPC.");
    return;
  }
  if (socketpair(AF_UNIX, SOCK_STREAM | SOCK_CLOEXEC, 0, vid) < 0) {
    close(ctl[0]);
    close(ctl[1]);
    state = ipc::StateDisconnected;
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
    state = ipc::StateDisconnected;
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
  connect(m_process, &QProcess::errorOccurred, this, [this](QProcess::ProcessError error) {
    state = ipc::StateDisconnected;
    if (error == QProcess::FailedToStart)
      fail("The native backend could not run. Check the application bundle.");
    else if (error == QProcess::Crashed)
      fail("The native backend crashed. Reconnect to obtain a fresh session.");
    else
      fail("The native backend failed: " + m_process->errorString());
  });
  connect(m_process, qOverload<int, QProcess::ExitStatus>(&QProcess::finished), this,
          [this](int, QProcess::ExitStatus) {
            readControl();
            failPending("Connection ended.");
            lastFrame = QImage();
            state = ipc::StateDisconnected;
            mounted = false;
            emit frameReady(QImage());
            if (message.startsWith("Connected") || message.startsWith("Opening") ||
                message.startsWith("Logging") || message.startsWith("Discovering") ||
                message.startsWith("Connecting") || message.startsWith(isoGoneText))
              setMessage("Connection ended. Reconnect to obtain a fresh session.");
            emit changed();
          });
  // What the core logs (a Go panic, log.Printf, the AVPT trace) goes to the
  // app's own stderr, the terminal or the journal; nothing reads its stdout.
  // Kept by QProcess instead, it went unread and grew (C-08).
  m_process->setProcessChannelMode(QProcess::ForwardedErrorChannel);
  m_process->setStandardOutputFile(QProcess::nullDevice());
  m_process->setProgram(
      qEnvironmentVariable("OLDIPMI_BIN_DIR", QCoreApplication::applicationDirPath()) +
      "/oldipmi-core");
  m_process->start();
  bool started = m_process->waitForStarted(3000);
  close(childCtl);
  close(childVid);
  if (!started)
    return;
  command({{"op", ipc::OpConnect},
           {"profile", QJsonObject::fromVariantMap(profile)},
           {"password", password}});
}
void Session::command(QJsonObject o) { command(std::move(o), nullptr, {}); }
void Session::command(QJsonObject o, const QObject *context, Result done) {
  Q_ASSERT(!done || context);
  Pending p{context, std::move(done)};
  if (!m_control || m_control->state() != QLocalSocket::ConnectedState) {
    settle(p, false, "Not connected.");
    return;
  }
  o["version"] = 1;
  o["id"] = ++m_id;
  if (p.done)
    m_pending[m_id] = std::move(p);
  if (m_control->bytesToWrite() > 1024 * 1024) {
    fail("Input queue is full; disconnect and reconnect.");
    m_process->terminate();
    failPending("Input queue is full.");
    return;
  }
  m_control->write(ControlChannel::frame(o));
  m_control->flush();
}
void Session::readControl() {
  if (!m_control)
    return;
  m_controlIn.append(m_control->readAll());
  for (ControlEvent e;;) {
    switch (m_controlIn.next(e)) {
    case ControlChannel::Status::NeedMore:
      return;
    case ControlChannel::Status::BadLength:
    case ControlChannel::Status::BadMessage:
      fail("Invalid backend control message.");
      m_process->terminate();
      return;
    case ControlChannel::Status::BadVersion:
      fail("Incompatible backend version.");
      m_process->terminate();
      return;
    case ControlChannel::Status::Event:
      break;
    }
    // A reply carries its command's id; only an event's error is the console's.
    const bool consoleError = e.type == ipc::EventError && e.id == 0;
    if (consoleError && state != ipc::StateConnected && state != ipc::StateDisconnected)
      errorStage = state;
    // A console that never delivered a picture failed while connecting.
    if (consoleError && state == ipc::StateConnected && lastFrame.isNull())
      errorStage = ipc::StageConnecting;
    if (e.state)
      state = *e.state;
    const QString newState = e.state.value_or(QString());
    if (e.type == ipc::EventError)
      setMessage(e.message);
    else if (e.type == ipc::EventMedia && !e.message.isEmpty())
      setMessage(isoGoneText + e.message);
    else if (newState == ipc::StageLoggingIn)
      setMessage("Logging in…");
    else if (newState == ipc::StageDiscovering)
      setMessage("Discovering console…");
    else if (newState == ipc::StageConnecting)
      setMessage("Connecting to console…");
    else if (newState == ipc::StateConnected)
      setMessage(connectedText);
    if (consoleError)
      errorCode = e.code; // for the error page's hint
    if (e.info) {
      auto resolution = consoleInfo.value("resolution");
      consoleInfo = *e.info;
      if (resolution.isValid())
        consoleInfo["resolution"] = resolution;
    }
    if (e.mounted) {
      mounted = *e.mounted;
      if (mounted && message.startsWith(isoGoneText))
        setMessage(connectedText);
    }
    if (e.id && m_pending.count(e.id) && (e.type == ipc::EventOK || e.type == ipc::EventError)) {
      auto p = std::move(m_pending[e.id]);
      m_pending.erase(e.id);
      settle(p, e.type == ipc::EventOK, e.message);
    }
    emit changed();
  }
}
void Session::readVideo() {
  if (!m_video)
    return;
  m_videoIn.append(m_video->readAll());
  QImage latest;
  if (m_videoIn.take(latest) == VideoChannel::Status::BadFrame) {
    fail("Invalid framebuffer from backend.");
    m_process->terminate();
    return;
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
    command({{"op", ipc::OpKey}, {"input", QJsonObject{{"usage", usage}, {"down", down}}}});
}
void Session::pointer(int x, int y, int buttons, int wheel) {
  if (connected())
    command({{"op", ipc::OpPointer},
             {"input", QJsonObject{{"x", x}, {"y", y}, {"buttons", buttons}, {"wheel", wheel}}}});
}
void Session::releaseInput() {
  if (connected())
    command({{"op", ipc::OpRelease}});
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
  state = ipc::StateConnected; // a framebuffer without a backend; input and power go nowhere
  lastFrame = im;
  ++frameSeq;
  ++changeSeq;
  sinceChange.restart();
  emit frameReady(im);
  setMessage("Desktop preview — no BMC connection");
  emit changed();
}
