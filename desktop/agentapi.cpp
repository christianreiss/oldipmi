#include "agentapi.h"
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
#include <unistd.h>

static constexpr int MaxLine = 1 << 20;            // one request line
static constexpr qint64 MaxPending = 64 << 20;     // unread replies before a client is dropped
static constexpr qint64 CoalesceMs = 5000;         // merge repeated input into one activity entry

struct AgentApi::Client {
  QPointer<QLocalSocket> sock;
  QByteArray buf;
  QString name, version, protocol;
  QDateTime since = QDateTime::currentDateTime();
  QSet<int> keys; // usages this client pressed and has not released
  int buttons = 0, x = 0, y = 0;
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
  if (!env.isEmpty())
    return env;
  auto dir = qEnvironmentVariable("XDG_RUNTIME_DIR");
  if (dir.isEmpty())
    dir = QDir::tempPath() + "/oldipmi-" + QString::number(getuid());
  return dir + "/oldipmi/agent.sock";
}
AgentApi::AgentApi(Controller *c, QString path, QObject *parent)
    : QObject(parent), m_c(c), m_path(path.isEmpty() ? defaultSocketPath() : path) {
  m_server.setSocketOptions(QLocalServer::UserAccessOption);
  connect(&m_server, &QLocalServer::newConnection, this, &AgentApi::accept);
  connect(m_c, &Controller::profileActivated, this, [this](QVariantMap p) { m_active = p; });
  connect(m_c, &Controller::passwordRequired, this, [this] {
    if (!m_requesting.isEmpty())
      m_passwordFor = m_requesting;
  });
  connect(m_c, &Controller::changed, this, [this] {
    if (m_c->state() != "disconnected")
      m_passwordFor.clear();
    if (m_c->state() != "connected") // a new session starts with nothing held
      for (auto &cl : m_clients) {
        cl->keys.clear();
        cl->buttons = 0;
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
  QDir().mkpath(QFileInfo(m_path).absolutePath());
  QFile::setPermissions(QFileInfo(m_path).absolutePath(),
                        QFile::ReadOwner | QFile::WriteOwner | QFile::ExeOwner);
  // A live instance owns the socket; only remove a stale file.
  QLocalSocket probe;
  probe.connectToServer(m_path);
  bool ok = false;
  if (probe.waitForConnected(200)) {
    m_error = "Another OldIPMI instance is already serving " + m_path + ".";
  } else {
    QLocalServer::removeServer(m_path);
    ok = m_server.listen(m_path);
    m_error = ok ? QString()
                 : m_server.errorString().isEmpty()
                       ? "Cannot listen on " + m_path + " (path too long? max ~107 bytes)."
                       : m_server.errorString();
  }
  log(label(nullptr), "server", ok ? "Agent control started" : "Agent control unavailable: " + m_error);
  emit listeningChanged();
  return ok;
}
void AgentApi::stop() {
  bool was = m_enabled || m_server.isListening();
  if (m_enabled) {
    m_enabled = false;
    emit enabledChanged();
  }
  std::vector<Client *> all;
  for (auto &c : m_clients)
    all.push_back(c.get());
  for (auto *c : all) {
    if (c->sock)
      c->sock->abort();
    dropClient(c);
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
QVariantList AgentApi::clients() const {
  QVariantList out;
  for (auto &c : m_clients)
    out << QVariantMap{{"name", c->name.isEmpty() ? c->label() : c->name}, {"version", c->version},
                       {"protocol", c->protocol}, {"since", c->since}};
  return out;
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
    m_clients.push_back(std::make_unique<Client>());
    Client *c = m_clients.back().get();
    c->sock = s;
    connect(s, &QLocalSocket::readyRead, this, [this, c] { readClient(c); });
    connect(s, &QLocalSocket::disconnected, this, [this, c] { dropClient(c); });
    log(label(c), "client", "Agent connected");
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
  for (auto it = m_clients.begin(); it != m_clients.end(); ++it)
    if (it->get() == c) {
      // Deleted later: this may run inside one of the client's own signal handlers.
      auto owned = std::make_shared<std::unique_ptr<Client>>(std::move(*it));
      m_clients.erase(it);
      QTimer::singleShot(0, this, [owned] {});
      break;
    }
  emit clientsChanged();
}
void AgentApi::releaseHeld(Client *c) {
  int n = c->keys.size() + (c->buttons ? 1 : 0);
  for (int u : std::as_const(c->keys))
    m_c->key(u, false);
  if (c->buttons)
    m_c->pointer(c->x, c->y, 0, 0);
  c->keys.clear();
  c->buttons = 0;
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
      send({{"id", 0}, {"ok", false}, {"error", "Invalid JSON request."}});
      continue;
    }
    auto o = doc.object();
    int id = o.value("id").toInt();
    dispatch(o.value("op").toString(), o.value("args").toObject(),
             [send, id](bool ok, QJsonValue result) {
               QJsonObject r{{"id", id}, {"ok", ok}};
               r[ok ? "result" : "error"] = result;
               send(r);
             },
             c);
  }
  if (!c->closing && c->buf.size() > MaxLine) {
    send({{"id", 0}, {"ok", false}, {"error", "Request line exceeds 1 MiB; closing."}});
    c->buf.clear();
    sock->disconnectFromServer();
  }
}
void AgentApi::note(const QString &what) { m_c->agentNote("Agent: " + what); }
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
void AgentApi::dispatch(const QString &op, const QJsonObject &a, Reply reply, Client *client) {
  auto fail = [&](const QString &m) { reply(false, m); };
  auto needConnected = [&]() {
    if (m_c->state() == "connected")
      return true;
    fail("Not connected (state: " + m_c->state() + "). Use connect first.");
    return false;
  };
  auto done = [reply](bool ok, QString m) { reply(ok, ok ? QJsonValue(QJsonObject{{"message", m}}) : QJsonValue(m)); };
  QString who = label(client);
  if (op == "hello") {
    if (client) {
      client->name = clean(a.value("name").toString());
      client->version = clean(a.value("version").toString(), 32);
      client->protocol = clean(a.value("protocol").toString(), 16);
      log(label(client), "client", "Agent identified itself");
      emit clientsChanged();
    }
    reply(true, QJsonObject{{"app", "OldIPMI"}, {"version", QCoreApplication::applicationVersion()}});
  } else if (op == "list_profiles") {
    QJsonArray list;
    for (const auto &v : m_c->hosts()) {
      auto p = v.toMap();
      auto o = profileJson(p);
      o["username"] = p["username"].toString();
      o["passwordSaved"] = p["remember"].toBool();
      list.append(o);
    }
    log(label(client), "profiles", "Listed profiles ×%1", true);
    reply(true, QJsonObject{{"profiles", list}});
  } else if (op == "status") {
    auto img = m_c->lastFrame();
    QJsonObject st{{"state", m_c->state()}, {"message", m_c->message()},
                   {"mounted", m_c->mounted()},
                   {"width", img.width()}, {"height", img.height()},
                   {"frameSeq", double(m_c->frameSeq())},
                   {"changeSeq", double(m_c->changeSeq())},
                   {"msSinceChange", double(m_c->msSinceChange())},
                   {"passwordRequired", !m_passwordFor.isEmpty()},
                   {"consoleInfo", QJsonObject::fromVariantMap(m_c->consoleInfo())}};
    if (!m_passwordFor.isEmpty())
      st["passwordProfile"] = m_passwordFor;
    if (m_c->state() != "disconnected" && !m_active.isEmpty())
      st["profile"] = profileJson(m_active);
    reply(true, st);
  } else if (op == "connect") {
    auto key = a.value("profile").toString();
    auto p = findProfile(key);
    if (p.isEmpty()) {
      QStringList names;
      for (const auto &v : m_c->hosts())
        names << v.toMap().value("name").toString();
      return fail("No profile named '" + key + "'. Available: " + names.join(", ") + ".");
    }
    auto name = p.value("name").toString();
    if (m_c->state() != "disconnected") {
      if (m_active.value("id") == p.value("id"))
        return reply(true, QJsonObject{{"state", m_c->state()}, {"profile", profileJson(p)}, {"already", true}});
      return fail("The console is busy (" + m_c->state() + " for '" + m_active.value("name").toString() +
                  "'). Disconnect first.");
    }
    if (m_passwordFor == name) // prompt still open from an earlier call: keep waiting for the user
      return reply(true, QJsonObject{{"state", m_c->state()}, {"passwordRequired", true}, {"profile", profileJson(p)}});
    note("opening " + name);
    log(label(client), "connect", "Open console '" + name + "'");
    m_requesting = name;
    m_c->connectHost(p, "");
    m_requesting.clear();
    if (m_c->state() == "disconnected") {
      if (m_passwordFor == name) {
        log(label(client), "connect", "Waiting for the user to enter the password for '" + name + "'");
        return reply(true, QJsonObject{{"state", m_c->state()}, {"passwordRequired", true}, {"profile", profileJson(p)}});
      }
      return fail(m_c->message());
    }
    reply(true, QJsonObject{{"state", m_c->state()}, {"profile", profileJson(p)}});
  } else if (op == "disconnect") {
    m_passwordFor.clear();
    note("disconnect");
    log(label(client), "connect", "Disconnect");
    m_c->disconnectHost();
    reply(true, QJsonObject{{"state", m_c->state()}});
  } else if (op == "screenshot") {
    auto img = m_c->lastFrame();
    if (img.isNull())
      return fail("No frame yet (state: " + m_c->state() + "). Connect first, or wait_for_change after connecting.");
    int fw = img.width(), fh = img.height();
    int x = a.value("x").toInt(0), y = a.value("y").toInt(0);
    int w = a.value("width").toInt(fw - x), h = a.value("height").toInt(fh - y);
    if (x < 0 || y < 0 || w <= 0 || h <= 0 || x + w > fw || y + h > fh)
      return fail(QString("Region outside the %1x%2 framebuffer.").arg(fw).arg(fh));
    double scale = a.value("scale").toDouble(1.0);
    if (scale < 0.1 || scale > 1.0)
      return fail("scale must be between 0.1 and 1.0.");
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
      return fail("Could not encode the screenshot.");
    log(label(client), "screenshot", "Screenshot ×%1", true);
    reply(true, QJsonObject{{"png", QString::fromLatin1(png.toBase64())}, {"width", out.width()},
                            {"height", out.height()}, {"fullWidth", fw}, {"fullHeight", fh},
                            {"x", x}, {"y", y}, {"regionW", w}, {"regionH", h}});
  } else if (op == "key") {
    if (!needConnected())
      return;
    int u = a.value("usage").toInt();
    if (u < 4 || u > 231)
      return fail("Invalid key usage.");
    bool down = a.value("down").toBool();
    m_c->key(u, down);
    if (client) {
      if (down)
        client->keys.insert(u);
      else
        client->keys.remove(u);
    }
    if (down) // never log which key: the agent may be typing a password
      log(label(client), "keys", "Keyboard input: %1 key presses", true);
    reply(true, QJsonObject{});
  } else if (op == "pointer") {
    if (!needConnected())
      return;
    auto img = m_c->lastFrame();
    if (img.isNull())
      return fail("No frame yet.");
    int x = a.value("x").toInt(), y = a.value("y").toInt();
    if (x < 0 || y < 0 || x >= img.width() || y >= img.height())
      return fail(QString("Point %1,%2 is outside the %3x%4 framebuffer.").arg(x).arg(y).arg(img.width()).arg(img.height()));
    int buttons = a.value("buttons").toInt() & 7;
    m_c->pointer(x, y, buttons, qBound(-1, a.value("wheel").toInt(), 1));
    if (client) {
      client->buttons = buttons;
      client->x = x;
      client->y = y;
    }
    log(who, "pointer", "Pointer input ×%1, last at " + QString::number(x) + "," + QString::number(y), true);
    reply(true, QJsonObject{});
  } else if (op == "release") {
    if (a.value("scope").toString() == "client") {
      if (client)
        releaseHeld(client);
    } else {
      m_c->releaseInput();
      for (auto &cl : m_clients) {
        cl->keys.clear();
        cl->buttons = 0;
      }
      log(who, "release", "Released all keys and buttons");
    }
    reply(true, QJsonObject{});
  } else if (op == "power") {
    if (!needConnected())
      return;
    auto action = a.value("action").toString();
    if (!QStringList{"on", "off", "reset", "soft"}.contains(action))
      return fail("Unknown power action.");
    m_c->releaseInput();
    note("power " + action);
    log(label(client), "power", "Power " + action + " requested");
    m_c->command({{"op", "power"}, {"action", action}}, [this, action, done, who](bool ok, QString m) {
      note(ok ? "power " + action + " accepted" : "power " + action + " failed: " + m);
      log(who, "power", ok ? "Power " + action + " accepted" : "Power " + action + " failed");
      done(ok, ok ? "Power action '" + action + "' accepted." : m);
    });
  } else if (op == "mount") {
    if (!needConnected())
      return;
    QFileInfo f(a.value("path").toString());
    if (!f.isAbsolute() || !f.isFile() || !f.isReadable() ||
        f.suffix().compare("iso", Qt::CaseInsensitive) != 0)
      return fail("path must be an absolute, readable .iso file.");
    note("mount " + f.fileName());
    log(label(client), "media", "Mount ISO " + f.fileName());
    m_c->command({{"op", "mount"}, {"path", f.canonicalFilePath()}}, done);
  } else if (op == "eject") {
    if (!needConnected())
      return;
    note("eject ISO");
    log(label(client), "media", "Eject ISO");
    m_c->command({{"op", "eject"}}, done);
  } else if (op == "instructions") {
    reply(true, QJsonObject{{"text", mcpInstructions()}});
  } else {
    fail("Unknown operation '" + op + "'.");
  }
}
