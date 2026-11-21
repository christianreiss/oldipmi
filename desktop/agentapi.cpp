#include "agentapi.h"
#include "agentops.h"
#include "runtimedir.h"
#include <QBuffer>
#include <QCoreApplication>
#include <QDir>
#include <QFile>
#include <QFileInfo>
#include <QJsonArray>
#include <QJsonDocument>
#include <QLocalSocket>
#include <QStandardPaths>
#include <QTimer>
#include <algorithm>
#include <unistd.h>

static constexpr int MaxLine = 1 << 20;            // one request line
static constexpr qint64 MaxPending = 64 << 20;     // unread replies before a client is dropped
static constexpr qint64 CoalesceMs = 5000;         // merge repeated input into one activity entry

struct AgentApi::Client {
  QPointer<QLocalSocket> sock;
  QByteArray buf;
  QString name, version, mcpVersion; // of the MCP client, and of the oldipmi-mcp it runs
  bool greeted = false;                // said hello
  struct Held { // what this client pressed on one session and has not released
    QSet<int> keys;
    int buttons = 0, x = 0, y = 0;
  };
  QMap<QString, Held> held; // by session id
  bool closing = false;
  QString label() const {
    if (name.isEmpty())
      return "unidentified client";
    return version.isEmpty() ? name : name + " " + version;
  }
};

static QString clean(const QString &s, int max = 64) {
  QString out;
  for (QChar ch : s.left(max))
    if (ch.isPrint())
      out += ch;
  return out.trimmed();
}

QString AgentApi::defaultSocketPath() {
  auto env = qEnvironmentVariable("OLDIPMI_AGENT_SOCKET");
  return env.isEmpty() ? runtimeDirPath() + "/agent.sock" : env;
}
AgentApi::AgentApi(Controller *c, QString path, QObject *parent)
    : QObject(parent), m_c(c), m_path(path.isEmpty() ? defaultSocketPath() : path) {
  m_server.setSocketOptions(QLocalServer::UserAccessOption);
  m_retry.setSingleShot(true);
  m_retry.setInterval(3000);
  connect(&m_retry, &QTimer::timeout, this, [this] {
    if (m_enabled && !m_server.isListening())
      start();
  });
  connect(&m_server, &QLocalServer::newConnection, this, &AgentApi::accept);
  connect(m_c, &Controller::passwordRequired, this, [this] {
    if (!m_requesting.isEmpty() && m_c->pendingProfile().value("id").toString() == m_requesting)
      m_prompt = m_c->pendingProfile();
  });
  connect(m_c, &Controller::changed, this, [this] {
    if (!m_prompt.isEmpty() && m_c->pendingProfile().value("id").toString() != promptId())
      m_prompt.clear(); // the prompt was answered, dismissed or replaced by another
    // A session that is no longer connected has nothing held; a new one starts clean.
    for (auto &cl : m_clients)
      for (auto it = cl->held.begin(); it != cl->held.end();) {
        auto *s = m_c->session(it.key());
        if (s && s->connected())
          ++it;
        else
          it = cl->held.erase(it);
      }
  });
}
AgentApi::~AgentApi() {
  disconnect(m_c, nullptr, this, nullptr);
  stop();
}
void AgentApi::setEnabled(bool on) {
  if (on)
    start();
  else
    stop();
}
bool AgentApi::start() {
  if (!m_enabled) {
    m_enabled = true;
    emit enabledChanged();
  }
  if (m_server.isListening())
    return true;
  // The runtime directory must be this user's own (B-12); a path given
  // explicitly is the caller's choice.
  const QString dir = QFileInfo(m_path).absolutePath();
  QString unsafe;
  if (dir == runtimeDirPath()) {
    runtimeDir(&unsafe);
  } else {
    QDir().mkpath(dir);
    QFile::setPermissions(dir, QFile::ReadOwner | QFile::WriteOwner | QFile::ExeOwner);
  }
  // A live instance owns the socket; only remove a stale file.
  QLocalSocket probe;
  if (unsafe.isEmpty())
    probe.connectToServer(m_path);
  bool ok = false;
  const QString before = m_error;
  if (!unsafe.isEmpty()) {
    m_error = unsafe;
  } else if (probe.waitForConnected(200)) {
    m_error = "Another OldIPMI instance is already serving " + m_path + "; this window takes over when that one closes.";
  } else {
    QLocalServer::removeServer(m_path);
    ok = m_server.listen(m_path);
    m_error = ok ? QString()
                 : m_server.errorString().isEmpty()
                       ? "Cannot listen on " + m_path + " (path too long? max ~107 bytes)."
                       : m_server.errorString();
  }
  if (ok || m_error != before) // retries stay quiet until something changes
    log(label(nullptr), "server", ok ? "Agent control started" : "Agent control unavailable: " + m_error);
  if (!ok)
    m_retry.start();
  emit listeningChanged();
  return ok;
}
void AgentApi::stop() {
  m_retry.stop();
  bool was = m_enabled || m_server.isListening();
  if (m_enabled) {
    m_enabled = false;
    emit enabledChanged();
  }
  const auto all = m_clients;
  for (const auto &c : all) {
    if (c->sock)
      c->sock->abort();
    dropClient(c.get());
  }
  if (m_server.isListening()) {
    m_server.close(); // also removes the socket file
    QFile::remove(m_path);
  }
  m_error.clear();
  if (was) {
    log(label(nullptr), "server", "Agent control stopped");
    emit listeningChanged();
  }
}
QString AgentApi::mcpPath() const {
  if (!m_mcpPath.isEmpty())
    return m_mcpPath;
  // An AppImage mounts at a new temporary path on every run, so a registered
  // path inside it would break; prefer a stable copy on PATH.
  if (!qEnvironmentVariableIsEmpty("APPIMAGE")) {
    auto onPath = QStandardPaths::findExecutable("oldipmi-mcp");
    if (!onPath.isEmpty())
      return onPath;
  }
  return QDir::cleanPath(qEnvironmentVariable("OLDIPMI_BIN_DIR", QCoreApplication::applicationDirPath()) +
                         "/oldipmi-mcp");
}
QString AgentApi::mcpInstructions() const {
  QFile f(":/mcp_instructions.md");
  if (!f.open(QIODevice::ReadOnly))
    return "Instructions are unavailable in this build.";
  return QString::fromUtf8(f.readAll()).replace("{{MCP_PATH}}", mcpPath());
}
QString AgentApi::clientName() const {
  QStringList names;
  for (auto &c : m_clients)
    if (!names.contains(c->label()))
      names << c->label();
  return names.join(", ");
}
QString AgentApi::versionNote() const {
  const QString app = QCoreApplication::applicationVersion();
  QStringList other;
  bool older = false;
  for (auto &c : m_clients) {
    if (!c->greeted || c->mcpVersion == app)
      continue;
    if (c->mcpVersion.isEmpty())
      older = true;
    else if (!other.contains(c->mcpVersion))
      other << c->mcpVersion;
  }
  if (other.isEmpty() && !older)
    return {};
  QString which = other.isEmpty() ? "an oldipmi-mcp from an older OldIPMI" : "oldipmi-mcp " + other.join(", ");
  if (older && !other.isEmpty())
    which += " and an older one";
  return "An agent uses " + which + ", not the one of OldIPMI " + app +
         ": tools may be missing or fail. Update the oldipmi-mcp the agent starts to this version.";
}
QVariantList AgentApi::activity() const {
  QVariantList out;
  for (const auto &e : m_activity)
    out << QVariantMap{{"time", e.time}, {"client", e.client},
                       {"text", e.count > 0 ? e.text.arg(e.count) : e.text}};
  return out;
}
void AgentApi::clearActivity() {
  m_activity.clear();
  emit activityChanged();
}
QString AgentApi::label(const Client *c) { return c ? c->label() : QString("OldIPMI"); }
void AgentApi::log(const QString &who, const QString &kind, const QString &text, bool coalesce) {
  auto now = QDateTime::currentDateTime();
  if (coalesce && !m_activity.isEmpty()) {
    auto &e = m_activity.front();
    if (e.count > 0 && e.kind == kind && e.client == who && e.time.msecsTo(now) < CoalesceMs) {
      e.count++;
      e.time = now;
      e.text = text;
      emit activityChanged();
      return;
    }
  }
  m_activity.prepend({now, who, text, kind, coalesce ? 1 : 0}); // count 0: text has no counter
  while (m_activity.size() > MaxActivity)
    m_activity.removeLast();
  emit activityChanged();
}
void AgentApi::accept() {
  while (auto *s = m_server.nextPendingConnection()) {
    auto c = std::make_shared<Client>();
    c->sock = s;
    m_clients.push_back(c);
    std::weak_ptr<Client> weak = c;
    connect(s, &QLocalSocket::readyRead, this, [this, weak] {
      if (auto c = weak.lock())
        readClient(c.get());
    });
    connect(s, &QLocalSocket::disconnected, this, [this, weak] {
      if (auto c = weak.lock())
        dropClient(c.get());
    });
    log(label(c.get()), "client", "Agent connected");
    emit clientsChanged();
  }
}
void AgentApi::dropClient(Client *c) {
  if (c->closing)
    return;
  c->closing = true;
  releaseHeld(c);
  if (c->sock) {
    disconnect(c->sock, nullptr, this, nullptr);
    c->sock->deleteLater();
  }
  log(label(c), "client", "Agent disconnected");
  // The caller holds the client until it returns (see m_clients).
  std::erase_if(m_clients, [c](const auto &p) { return p.get() == c; });
  emit clientsChanged();
}
void AgentApi::releaseHeld(Client *c) {
  int n = 0;
  for (auto it = c->held.cbegin(); it != c->held.cend(); ++it) {
    n += it->keys.size() + (it->buttons ? 1 : 0);
    if (auto *s = m_c->session(it.key())) {
      for (int u : it->keys)
        s->key(u, false);
      if (it->buttons)
        s->pointer(it->x, it->y, 0, 0);
    }
  }
  c->held.clear();
  if (n)
    log(label(c), "release", QString("Released %1 held key(s)/button(s) of this agent").arg(n));
}
void AgentApi::readClient(Client *c) {
  if (c->closing || !c->sock)
    return;
  c->buf += c->sock->readAll();
  QPointer<QLocalSocket> sock(c->sock);
  auto send = [sock](const QJsonObject &r) {
    if (!sock || sock->state() != QLocalSocket::ConnectedState)
      return;
    if (sock->bytesToWrite() > MaxPending) { // the client stopped reading
      sock->abort();
      return;
    }
    sock->write(QJsonDocument(r).toJson(QJsonDocument::Compact) + "\n");
  };
  int nl;
  while (!c->closing && (nl = c->buf.indexOf('\n')) >= 0) {
    auto line = c->buf.left(nl).trimmed();
    c->buf.remove(0, nl + 1);
    if (line.isEmpty())
      continue;
    QJsonParseError pe;
    auto doc = QJsonDocument::fromJson(line, &pe);
    if (pe.error != QJsonParseError::NoError || !doc.isObject()) {
      send({{"id", 0}, {"ok", false}, {"error", "Invalid JSON request."}, {"code", agentops::ErrBadRequest}});
      continue;
    }
    auto o = doc.object();
    int id = o.value("id").toInt();
    dispatch(o.value("op").toString(), o.value("args").toObject(),
             [send, id](bool ok, QJsonValue result, QString code) {
               QJsonObject r{{"id", id}, {"ok", ok}};
               r[ok ? "result" : "error"] = result;
               if (!ok)
                 r["code"] = code;
               send(r);
             },
             c);
  }
  if (!c->closing && c->buf.size() > MaxLine) {
    send({{"id", 0}, {"ok", false}, {"error", "Request line exceeds 1 MiB; closing."}, {"code", agentops::ErrBadRequest}});
    c->buf.clear();
    sock->disconnectFromServer();
  }
}
void AgentApi::audit(const QString &who, const QString &kind, const QString &activity, const QString &status) {
  m_c->agentNote("Agent: " + status);
  log(who, kind, activity);
}
QVariantMap AgentApi::findProfile(const QString &key) const {
  for (const auto &v : m_c->hosts()) {
    auto p = v.toMap();
    if (p.value("id").toString() == key)
      return p;
  }
  for (const auto &v : m_c->hosts()) {
    auto p = v.toMap();
    if (p.value("name").toString().compare(key, Qt::CaseInsensitive) == 0 ||
        p.value("host").toString().compare(key, Qt::CaseInsensitive) == 0)
      return p;
  }
  return {};
}
static QJsonObject profileJson(const QVariantMap &p) {
  return {{"id", p["id"].toString()}, {"name", p["name"].toString()}, {"host", p["host"].toString()},
          {"family", p["family"].toString()}};
}
AgentApi::Failure AgentApi::noProfile(const QString &key) const {
  QStringList names;
  for (const auto &v : m_c->hosts())
    names << v.toMap().value("name").toString();
  return {agentops::ErrNoProfile, "No profile named '" + key + "'. Available: " + names.join(", ") + "."};
}
Session *AgentApi::resolve(const QJsonObject &a, Failure *failure) const {
  *failure = {};
  auto key = a.value("profile").toString();
  if (!key.isEmpty()) {
    auto p = findProfile(key);
    if (p.isEmpty()) {
      *failure = noProfile(key);
      return nullptr;
    }
    return m_c->session(p.value("id").toString());
  }
  auto open = m_c->openIds();
  if (open.size() > 1) {
    QStringList names;
    for (const auto &id : open)
      names << "'" + m_c->session(id)->profile.value("name").toString() + "'";
    *failure = {agentops::ErrAmbiguous,
                "Several consoles are open (" + names.join(", ") + "). Say which one with the profile argument."};
    return nullptr;
  }
  return open.isEmpty() ? nullptr : m_c->session(open.first());
}
Session *AgentApi::connected(const Call &c) const {
  Failure f;
  auto *s = resolve(c.args, &f);
  if (f.code) {
    c.fail(f.code, f.message);
    return nullptr;
  }
  if (!s || !s->connected()) {
    c.fail(agentops::ErrNotConnected,
           "Not connected (state: " + (s ? s->stateName() : QString(ipc::StateDisconnected)) + "). Use connect first.");
    return nullptr;
  }
  return s;
}
// The reply to power, mount and eject, once the console has answered.
static Session::Result slowReply(AgentApi::Reply reply) {
  return [reply = std::move(reply)](bool ok, QString m) {
    reply(ok, ok ? QJsonValue(QJsonObject{{"message", m}}) : QJsonValue(m), ok ? QString() : QString(agentops::ErrFailed));
  };
}
const AgentApi::Op AgentApi::Ops[] = {
    {agentops::OpHello, &AgentApi::opHello},           {agentops::OpListProfiles, &AgentApi::opListProfiles},
    {agentops::OpStatus, &AgentApi::opStatus},         {agentops::OpConnect, &AgentApi::opConnect},
    {agentops::OpDisconnect, &AgentApi::opDisconnect}, {agentops::OpScreenshot, &AgentApi::opScreenshot},
    {agentops::OpKey, &AgentApi::opKey},               {agentops::OpPointer, &AgentApi::opPointer},
    {agentops::OpRelease, &AgentApi::opRelease},       {agentops::OpPower, &AgentApi::opPower},
    {agentops::OpMount, &AgentApi::opMount},           {agentops::OpEject, &AgentApi::opEject},
};
void AgentApi::dispatch(const QString &op, const QJsonObject &args, Reply reply, Client *client) {
  Call c{args, client, std::move(reply)};
  for (const auto &o : Ops)
    if (op == QLatin1StringView(o.name))
      return (this->*o.run)(c);
  c.fail(agentops::ErrUnknownOp, "Unknown operation '" + op + "'.");
}
void AgentApi::opHello(Call &c) {
  if (c.client) {
    c.client->name = clean(c.args.value("name").toString());
    c.client->version = clean(c.args.value("version").toString(), 32);
    c.client->mcpVersion = clean(c.args.value("mcpVersion").toString(), 32);
    c.client->greeted = true;
    log(c.who(), "client", "Agent identified itself");
    emit clientsChanged();
  }
  QJsonArray ops;
  for (const auto &o : Ops)
    ops.append(QLatin1StringView(o.name));
  c.succeed(QJsonObject{{"app", "OldIPMI"}, {"version", QCoreApplication::applicationVersion()},
                        {"api", agentops::Api}, {"ops", ops}});
}
void AgentApi::opListProfiles(Call &c) {
  QJsonArray list;
  for (const auto &v : m_c->hosts()) {
    auto p = v.toMap();
    auto o = profileJson(p);
    o["username"] = p["username"].toString();
    o["passwordSaved"] = p["remember"].toBool();
    list.append(o);
  }
  log(c.who(), "profiles", "Listed profiles ×%1", true);
  c.succeed(QJsonObject{{"profiles", list}});
}
void AgentApi::opStatus(Call &c) {
  Failure f;
  auto *s = resolve(c.args, &f);
  if (f.code)
    return c.fail(f.code, f.message);
  QJsonArray sessions;
  auto all = m_c->allSessions();
  std::sort(all.begin(), all.end(), [](Session *x, Session *y) {
    return x->profile.value("name").toString().toLower() < y->profile.value("name").toString().toLower();
  });
  for (auto *o : all) {
    auto j = profileJson(o->profile);
    j["state"] = o->stateName();
    sessions.append(j);
  }
  // The prompt counts for the profile status is about: the console's, else
  // the one named; with neither, any prompt counts.
  QString about;
  if (s)
    about = s->id;
  else if (auto key = c.args.value("profile").toString(); !key.isEmpty())
    about = findProfile(key).value("id").toString();
  QJsonObject st{{"sessions", sessions},
                 {"passwordRequired", !m_prompt.isEmpty() && (about.isEmpty() || about == promptId())}};
  if (!m_prompt.isEmpty())
    st["passwordProfile"] = m_prompt.value("name").toString();
  if (s) {
    st["state"] = s->stateName();
    st["message"] = s->message;
    st["mounted"] = s->mounted;
    st["width"] = s->lastFrame.width();
    st["height"] = s->lastFrame.height();
    st["frameSeq"] = double(s->frameSeq);
    st["changeSeq"] = double(s->changeSeq);
    st["msSinceChange"] = double(s->msSinceChange());
    // Only an open session's; a closed one's would mislead.
    st["consoleInfo"] = s->state == Session::State::Disconnected ? QJsonObject{} : QJsonObject::fromVariantMap(s->consoleInfo);
    if (s->state != Session::State::Disconnected)
      st["profile"] = profileJson(s->profile);
  } else {
    st["state"] = ipc::StateDisconnected;
    st["message"] = m_c->message();
    st["mounted"] = false;
    st["width"] = 0;
    st["height"] = 0;
    st["frameSeq"] = 0;
    st["changeSeq"] = 0;
    st["msSinceChange"] = 0;
    st["consoleInfo"] = QJsonObject{};
  }
  c.succeed(st);
}
void AgentApi::opConnect(Call &c) {
  auto key = c.args.value("profile").toString();
  auto p = findProfile(key);
  if (p.isEmpty()) {
    auto f = noProfile(key);
    return c.fail(f.code, f.message);
  }
  auto name = p.value("name").toString();
  auto id = p.value("id").toString();
  if (m_c->isOpen(id))
    return c.succeed(QJsonObject{{"state", m_c->session(id)->stateName()}, {"profile", profileJson(p)}, {"already", true}});
  if (auto pending = m_c->pendingProfile(); !pending.isEmpty() && pending.value("id") != p.value("id"))
    return c.fail(agentops::ErrBusy, "A password prompt for '" + pending.value("name").toString() +
                                         "' is open in OldIPMI. Wait until the user answers or cancels it.");
  // Always ask again: the user may have dismissed an earlier password prompt.
  audit(c.who(), "connect", "Open console '" + name + "'", "opening " + name);
  m_requesting = id;
  m_c->connectHost(p, "");
  m_requesting.clear();
  auto *s = m_c->session(id);
  if (!s || s->state == Session::State::Disconnected) {
    if (promptId() == id) {
      log(c.who(), "connect", "Waiting for the user to enter the password for '" + name + "'");
      return c.succeed(QJsonObject{{"state", ipc::StateDisconnected}, {"passwordRequired", true}, {"profile", profileJson(p)}});
    }
    return c.fail(agentops::ErrFailed, m_c->message());
  }
  c.succeed(QJsonObject{{"state", s->stateName()}, {"profile", profileJson(p)}});
}
void AgentApi::opDisconnect(Call &c) {
  Failure f;
  auto *s = resolve(c.args, &f);
  bool all = c.args.value("all").toBool();
  if (f.code && !all)
    return c.fail(f.code, f.message);
  bool explicitProfile = !c.args.value("profile").toString().isEmpty();
  QJsonObject r;
  if (all) {
    QJsonArray closed;
    for (auto *o : m_c->allSessions())
      if (o->state != Session::State::Disconnected)
        closed.append(profileJson(o->profile));
    r["closed"] = closed;
    r["promptDismissed"] = !m_prompt.isEmpty();
    if (!closed.isEmpty() || !m_prompt.isEmpty())
      audit(c.who(), "connect", "Disconnect all consoles", "disconnect all");
    m_prompt.clear();
    m_c->disconnectAll();
  } else {
    auto prof = explicitProfile ? findProfile(c.args.value("profile").toString()) : QVariantMap{};
    // The prompt goes with its own profile: the one named, or, with none
    // named, the console closed; when no console is open, it is the setup
    // being cancelled.
    bool promptHere = !m_prompt.isEmpty() && (explicitProfile ? prof.value("id").toString() == promptId()
                                                              : !s || s->id == promptId());
    bool open = s && s->state != Session::State::Disconnected;
    r["previous"] = s ? s->stateName() : QString(ipc::StateDisconnected);
    r["promptDismissed"] = promptHere;
    if (open)
      r["profile"] = profileJson(s->profile);
    if (open || promptHere)
      audit(c.who(), "connect", "Disconnect", "disconnect");
    // What to close: the targeted console, else the profile named, else,
    // without a prompt to dismiss, the console in front.
    QString target;
    if (s)
      target = s->id;
    else if (explicitProfile)
      target = prof.value("id").toString();
    else if (m_prompt.isEmpty())
      target = m_c->shownId();
    if (promptHere)
      m_prompt.clear();
    m_c->disconnectProfile(target);
  }
  r["state"] = ipc::StateDisconnected;
  QJsonArray remaining;
  for (const auto &id : m_c->openIds())
    remaining.append(profileJson(m_c->session(id)->profile));
  r["stillOpen"] = remaining;
  c.succeed(r);
}
void AgentApi::opScreenshot(Call &c) {
  Failure f;
  auto *s = resolve(c.args, &f);
  if (f.code)
    return c.fail(f.code, f.message);
  auto img = s ? s->lastFrame : QImage();
  if (img.isNull())
    return c.fail(agentops::ErrNoFrame, "No frame yet (state: " + (s ? s->stateName() : QString(ipc::StateDisconnected)) +
                                            "). Connect first, or wait_for_change after connecting.");
  const auto &a = c.args;
  int fw = img.width(), fh = img.height();
  int x = a.value("x").toInt(0), y = a.value("y").toInt(0);
  int w = a.value("width").toInt(fw - x), h = a.value("height").toInt(fh - y);
  if (x < 0 || y < 0 || w <= 0 || h <= 0 || x + w > fw || y + h > fh)
    return c.fail(agentops::ErrBadArgs, QString("Region outside the %1x%2 framebuffer.").arg(fw).arg(fh));
  double scale = a.value("scale").toDouble(1.0);
  if (scale < 0.1 || scale > 1.0)
    return c.fail(agentops::ErrBadArgs, "scale must be between 0.1 and 1.0.");
  int maxSize = a.value("maxSize").toInt(0);
  if (maxSize > 0 && qMax(w, h) * scale > maxSize)
    scale = double(maxSize) / qMax(w, h);
  QImage out = img.copy(x, y, w, h);
  if (scale < 1.0)
    out = out.scaled(qMax(1, int(w * scale)), qMax(1, int(h * scale)), Qt::IgnoreAspectRatio,
                     Qt::SmoothTransformation);
  QByteArray png;
  QBuffer buf(&png);
  buf.open(QIODevice::WriteOnly);
  if (!out.convertToFormat(QImage::Format_RGB888).save(&buf, "PNG"))
    return c.fail(agentops::ErrFailed, "Could not encode the screenshot.");
  log(c.who(), "screenshot", "Screenshot ×%1", true);
  c.succeed(QJsonObject{{"png", QString::fromLatin1(png.toBase64())}, {"width", out.width()},
                        {"height", out.height()}, {"fullWidth", fw}, {"fullHeight", fh},
                        {"x", x}, {"y", y}, {"regionW", w}, {"regionH", h}});
}
void AgentApi::opKey(Call &c) {
  auto *s = connected(c);
  if (!s)
    return;
  int u = c.args.value("usage").toInt();
  if (u < 4 || u > 231)
    return c.fail(agentops::ErrBadArgs, "Invalid key usage.");
  bool down = c.args.value("down").toBool();
  s->key(u, down);
  if (c.client) {
    if (down)
      c.client->held[s->id].keys.insert(u);
    else
      c.client->held[s->id].keys.remove(u);
  }
  if (down) // never log which key: the agent may be typing a password
    log(c.who(), "keys", "Keyboard input: %1 key presses", true);
  c.succeed(QJsonObject{});
}
void AgentApi::opPointer(Call &c) {
  auto *s = connected(c);
  if (!s)
    return;
  auto img = s->lastFrame;
  if (img.isNull())
    return c.fail(agentops::ErrNoFrame, "No frame yet.");
  int x = c.args.value("x").toInt(), y = c.args.value("y").toInt();
  if (x < 0 || y < 0 || x >= img.width() || y >= img.height())
    return c.fail(agentops::ErrBadArgs, QString("Point %1,%2 is outside the %3x%4 framebuffer.")
                                            .arg(x).arg(y).arg(img.width()).arg(img.height()));
  int buttons = c.args.value("buttons").toInt() & 7;
  s->pointer(x, y, buttons, qBound(-1, c.args.value("wheel").toInt(), 1));
  if (c.client) {
    auto &h = c.client->held[s->id];
    h.buttons = buttons;
    h.x = x;
    h.y = y;
  }
  log(c.who(), "pointer", "Pointer input ×%1, last at " + QString::number(x) + "," + QString::number(y), true);
  c.succeed(QJsonObject{});
}
void AgentApi::opRelease(Call &c) {
  Session *only = nullptr;
  if (!c.args.value("profile").toString().isEmpty()) {
    Failure f;
    only = resolve(c.args, &f);
    if (f.code)
      return c.fail(f.code, f.message);
  }
  for (auto *s : m_c->allSessions())
    if (!only || s == only)
      s->releaseInput();
  for (auto &cl : m_clients) {
    if (only)
      cl->held.remove(only->id);
    else
      cl->held.clear();
  }
  log(c.who(), "release", "Released all keys and buttons");
  c.succeed(QJsonObject{});
}
void AgentApi::opPower(Call &c) {
  auto *s = connected(c);
  if (!s)
    return;
  auto action = c.args.value("action").toString();
  if (!QStringList{ipc::PowerOn, ipc::PowerOff, ipc::PowerReset, ipc::PowerSoft}.contains(action))
    return c.fail(agentops::ErrBadArgs, "Unknown power action.");
  s->releaseInput();
  audit(c.who(), "power", "Power " + action + " requested", "power " + action);
  s->power(action, this, [this, action, who = c.who(), done = slowReply(c.reply)](bool ok, QString m) {
    audit(who, "power", ok ? "Power " + action + " accepted" : "Power " + action + " failed",
          ok ? "power " + action + " accepted" : "power " + action + " failed: " + m);
    done(ok, ok ? "Power action '" + action + "' accepted." : m);
  });
}
void AgentApi::opMount(Call &c) {
  auto *s = connected(c);
  if (!s)
    return;
  QFileInfo f(c.args.value("path").toString());
  if (!f.isAbsolute() || !f.isFile() || !f.isReadable() ||
      f.suffix().compare("iso", Qt::CaseInsensitive) != 0)
    return c.fail(agentops::ErrBadArgs, "path must be an absolute, readable .iso file.");
  audit(c.who(), "media", "Mount ISO " + f.fileName(), "mount " + f.fileName());
  s->mount(f.canonicalFilePath(), this, slowReply(c.reply));
}
void AgentApi::opEject(Call &c) {
  auto *s = connected(c);
  if (!s)
    return;
  audit(c.who(), "media", "Eject ISO", "eject ISO");
  s->eject(this, slowReply(c.reply));
}
