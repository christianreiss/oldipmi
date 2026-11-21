#include "session.h"
#include "backendprocess.h"
#include <QColor>
#include <QDateTime>
#include <QTimer>

// The message of a console that is ready, and the start of the notice of an
// ISO that went away by itself, which a new mount retires.
static const QString connectedText = "Connected — click the console to control it.";
static const QString isoGoneText = "The ISO is no longer mounted: ";

// The name the core and QML give a state.
static QString nameOf(Session::State state) {
  switch (state) {
  case Session::State::Disconnected:
    return ipc::StateDisconnected;
  case Session::State::LoggingIn:
    return ipc::StageLoggingIn;
  case Session::State::Discovering:
    return ipc::StageDiscovering;
  case Session::State::Connecting:
    return ipc::StageConnecting;
  case Session::State::Connected:
    return ipc::StateConnected;
  }
  return {};
}
// The state the core names. A name this app does not know has none and
// leaves the state as it was: the core and the app ship together, and
// ipc_names checks that both spell the same names.
static std::optional<Session::State> stateNamed(const QString &name) {
  using State = Session::State;
  for (State state : {State::Disconnected, State::LoggingIn, State::Discovering, State::Connecting, State::Connected})
    if (nameOf(state) == name)
      return state;
  return std::nullopt;
}
QString Session::stateName() const { return nameOf(state); }
QString Session::errorStageName() const { return errorStage ? nameOf(*errorStage) : QString(); }

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
  if (m_backend && m_backend->control() && !m_disconnectSent) {
    m_disconnectSent = true;
    command({{"op", ipc::OpDisconnect}});
  }
}
void Session::stop() {
  disconnect(m_waitForHost);
  failPending("Connection closed.");
  lastFrame = QImage();
  if (m_backend) {
    // Nothing the core does from here on concerns this session.
    disconnect(m_backend, nullptr, this, nullptr);
    if (m_backend->control()) {
      disconnect(m_backend->control(), nullptr, this, nullptr);
      disconnect(m_backend->video(), nullptr, this, nullptr);
    }
    sendDisconnect();
    m_backend->setParent(parent());
    m_backend->stop();
    m_backend = nullptr;
  }
  m_controlIn.clear();
  m_videoIn.clear();
  mounted = false;
}
void Session::begin(const QString &password) {
  m_disconnectSent = false;
  consoleInfo.clear();
  errorStage.reset();
  startedAt = QDateTime::currentMSecsSinceEpoch();
  state = State::LoggingIn;
  setMessage("Opening console on " + profile.value("host").toString() + "…");
  emit changed();
  launch(password);
}
void Session::launch(const QString &password) {
  const QString host = profile.value("host").toString();
  if (auto *old = BackendProcess::stopping(host)) {
    m_waitForHost = connect(old, &BackendProcess::finished, this, [this, password] { launch(password); },
                            Qt::SingleShotConnection);
    return;
  }
  m_backend = new BackendProcess(host, this);
  connect(m_backend, &BackendProcess::errorOccurred, this, [this](QProcess::ProcessError error) {
    state = State::Disconnected;
    if (error == QProcess::FailedToStart)
      fail("The native backend could not run. Check the application bundle.");
    else if (error == QProcess::Crashed)
      fail("The native backend crashed. Reconnect to obtain a fresh session.");
    else
      fail("The native backend failed: " + m_backend->errorString());
  });
  connect(m_backend, &BackendProcess::finished, this, [this] {
    readControl();
    failPending("Connection ended.");
    lastFrame = QImage();
    state = State::Disconnected;
    mounted = false;
    emit frameReady(QImage());
    if (message.startsWith("Connected") || message.startsWith("Opening") ||
        message.startsWith("Logging") || message.startsWith("Discovering") ||
        message.startsWith("Connecting") || message.startsWith(isoGoneText))
      setMessage("Connection ended. Reconnect to obtain a fresh session.");
    emit changed();
  });
  const auto started = m_backend->start();
  if (started == BackendProcess::Start::NoControlPair || started == BackendProcess::Start::NoVideoPair ||
      started == BackendProcess::Start::NoDescriptors) {
    delete m_backend;
    m_backend = nullptr;
    state = State::Disconnected;
    fail(started == BackendProcess::Start::NoControlPair ? "Cannot create private IPC."
         : started == BackendProcess::Start::NoVideoPair ? "Cannot create video IPC."
                                                         : "Cannot pass private IPC descriptors.");
    return;
  }
  connect(m_backend->control(), &QLocalSocket::readyRead, this, &Session::readControl);
  connect(m_backend->video(), &QLocalSocket::readyRead, this, &Session::readVideo);
  if (started != BackendProcess::Start::Started)
    return;
  command({{"op", ipc::OpConnect},
           {"profile", QJsonObject::fromVariantMap(profile)},
           {"password", password}});
}
void Session::command(QJsonObject o) { command(std::move(o), nullptr, {}); }
void Session::command(QJsonObject o, const QObject *context, Result done) {
  Q_ASSERT(!done || context);
  Pending p{context, std::move(done)};
  QLocalSocket *control = m_backend ? m_backend->control() : nullptr;
  if (!control || control->state() != QLocalSocket::ConnectedState) {
    settle(p, false, "Not connected.");
    return;
  }
  o["version"] = 1;
  o["id"] = ++m_id;
  if (p.done)
    m_pending[m_id] = std::move(p);
  if (control->bytesToWrite() > 1024 * 1024) {
    fail("Input queue is full; disconnect and reconnect.");
    m_backend->terminate();
    failPending("Input queue is full.");
    return;
  }
  control->write(ControlChannel::frame(o));
  control->flush();
}
void Session::readControl() {
  if (!m_backend || !m_backend->control())
    return;
  m_controlIn.append(m_backend->control()->readAll());
  for (ControlEvent e;;) {
    switch (m_controlIn.next(e)) {
    case ControlChannel::Status::NeedMore:
      return;
    case ControlChannel::Status::BadLength:
    case ControlChannel::Status::BadMessage:
      fail("Invalid backend control message.");
      m_backend->terminate();
      return;
    case ControlChannel::Status::BadVersion:
      fail("Incompatible backend version.");
      m_backend->terminate();
      return;
    case ControlChannel::Status::Event:
      break;
    }
    // A reply carries its command's id; only an event's error is the console's.
    const bool consoleError = e.type == ipc::EventError && e.id == 0;
    if (consoleError && state != State::Connected && state != State::Disconnected)
      errorStage = state;
    // A console that never delivered a picture failed while connecting.
    if (consoleError && state == State::Connected && lastFrame.isNull())
      errorStage = State::Connecting;
    const std::optional<State> newState = e.state ? stateNamed(*e.state) : std::nullopt;
    if (newState)
      state = *newState;
    if (e.type == ipc::EventError)
      setMessage(e.message);
    else if (e.type == ipc::EventMedia && !e.message.isEmpty())
      setMessage(isoGoneText + e.message);
    else if (newState == State::LoggingIn)
      setMessage("Logging in…");
    else if (newState == State::Discovering)
      setMessage("Discovering console…");
    else if (newState == State::Connecting)
      setMessage("Connecting to console…");
    else if (newState == State::Connected)
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
  if (!m_backend || !m_backend->video())
    return;
  m_videoIn.append(m_backend->video()->readAll());
  QImage latest;
  if (m_videoIn.take(latest) == VideoChannel::Status::BadFrame) {
    fail("Invalid framebuffer from backend.");
    m_backend->terminate();
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
  state = State::Connected; // a framebuffer without a backend; input and power go nowhere
  lastFrame = im;
  ++frameSeq;
  ++changeSeq;
  sinceChange.restart();
  emit frameReady(im);
  setMessage("Desktop preview — no BMC connection");
  emit changed();
}
