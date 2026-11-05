#include "agentapi.h"
#include <QBuffer>
#include <QCoreApplication>
#include <QDir>
#include <QFile>
#include <QFileInfo>
#include <QJsonArray>
#include <QJsonDocument>
#include <unistd.h>

static constexpr int MaxLine = 1 << 20;

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
}
bool AgentApi::start() {
  QDir().mkpath(QFileInfo(m_path).absolutePath());
  QFile::setPermissions(QFileInfo(m_path).absolutePath(),
                        QFile::ReadOwner | QFile::WriteOwner | QFile::ExeOwner);
  // A live instance owns the socket; only remove a stale file.
  QLocalSocket probe;
  probe.connectToServer(m_path);
  if (probe.waitForConnected(200))
    return false;
  QLocalServer::removeServer(m_path);
  bool ok = m_server.listen(m_path);
  emit listeningChanged();
  return ok;
}
QString AgentApi::mcpPath() const {
  return QDir::cleanPath(qEnvironmentVariable("OLDIPMI_BIN_DIR", QCoreApplication::applicationDirPath()) +
                         "/oldipmi-mcp");
}
QString AgentApi::mcpInstructions() const {
  QFile f(":/mcp_instructions.md");
  if (!f.open(QIODevice::ReadOnly))
    return "Instructions are unavailable in this build.";
  return QString::fromUtf8(f.readAll()).replace("{{MCP_PATH}}", mcpPath());
}
void AgentApi::accept() {
  while (auto *s = m_server.nextPendingConnection()) {
    auto *buf = new QByteArray;
    connect(s, &QLocalSocket::disconnected, s, &QObject::deleteLater);
    connect(s, &QObject::destroyed, [buf] { delete buf; });
    connect(s, &QLocalSocket::readyRead, this, [this, s, buf] {
      *buf += s->readAll();
      int nl;
      while ((nl = buf->indexOf('\n')) >= 0) {
        auto line = buf->left(nl);
        buf->remove(0, nl + 1);
        auto o = QJsonDocument::fromJson(line).object();
        int id = o.value("id").toInt();
        QPointer<QLocalSocket> sock(s);
        dispatch(o.value("op").toString(), o.value("args").toObject(),
                 [sock, id](bool ok, QJsonValue result) {
                   if (!sock)
                     return;
                   QJsonObject r{{"id", id}, {"ok", ok}};
                   r[ok ? "result" : "error"] = result;
                   sock->write(QJsonDocument(r).toJson(QJsonDocument::Compact) + "\n");
                 });
      }
      if (buf->size() > MaxLine) {
        s->disconnectFromServer();
        buf->clear();
      }
    });
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
void AgentApi::dispatch(const QString &op, const QJsonObject &a, Reply reply) {
  auto fail = [&](const QString &m) { reply(false, m); };
  auto needConnected = [&]() {
    if (m_c->state() == "connected")
      return true;
    fail("Not connected (state: " + m_c->state() + "). Use connect first.");
    return false;
  };
  auto done = [reply](bool ok, QString m) { reply(ok, ok ? QJsonValue(QJsonObject{{"message", m}}) : QJsonValue(m)); };
  if (op == "list_profiles") {
    QJsonArray list;
    for (const auto &v : m_c->hosts()) {
      auto p = v.toMap();
      list.append(QJsonObject{{"id", p["id"].toString()}, {"name", p["name"].toString()},
                              {"host", p["host"].toString()}, {"family", p["family"].toString()},
                              {"username", p["username"].toString()},
                              {"passwordSaved", p["remember"].toBool()}});
    }
    reply(true, QJsonObject{{"profiles", list}});
  } else if (op == "status") {
    auto img = m_c->lastFrame();
    reply(true, QJsonObject{{"state", m_c->state()}, {"message", m_c->message()},
                            {"mounted", m_c->mounted()},
                            {"width", img.width()}, {"height", img.height()},
                            {"frameSeq", double(m_c->frameSeq())},
                            {"changeSeq", double(m_c->changeSeq())},
                            {"msSinceChange", double(m_c->msSinceChange())},
                            {"consoleInfo", QJsonObject::fromVariantMap(m_c->consoleInfo())}});
  } else if (op == "connect") {
    auto key = a.value("profile").toString();
    auto p = findProfile(key);
    if (p.isEmpty())
      return fail("No profile named '" + key + "'. Use list_profiles.");
    if (m_c->state() != "disconnected")
      return fail("Already " + m_c->state() + "; disconnect first.");
    note("opening " + p.value("name").toString());
    m_c->connectHost(p, "");
    if (m_c->state() == "disconnected")
      return fail(m_c->message());
    reply(true, QJsonObject{{"state", m_c->state()}});
  } else if (op == "disconnect") {
    note("disconnect");
    m_c->disconnectHost();
    reply(true, QJsonObject{});
  } else if (op == "screenshot") {
    auto img = m_c->lastFrame();
    if (img.isNull())
      return fail("No frame yet (state: " + m_c->state() + ").");
    int fw = img.width(), fh = img.height();
    int x = a.value("x").toInt(0), y = a.value("y").toInt(0);
    int w = a.value("width").toInt(fw - x), h = a.value("height").toInt(fh - y);
    if (x < 0 || y < 0 || w <= 0 || h <= 0 || x + w > fw || y + h > fh)
      return fail(QString("Region outside the %1x%2 framebuffer.").arg(fw).arg(fh));
    double scale = a.value("scale").toDouble(1.0);
    if (scale < 0.1 || scale > 1.0)
      return fail("scale must be between 0.1 and 1.0.");
    QImage out = img.copy(x, y, w, h);
    if (scale < 1.0)
      out = out.scaled(qMax(1, int(w * scale)), qMax(1, int(h * scale)), Qt::IgnoreAspectRatio,
                       Qt::SmoothTransformation);
    QByteArray png;
    QBuffer buf(&png);
    buf.open(QIODevice::WriteOnly);
    if (!out.save(&buf, "PNG"))
      return fail("Could not encode the screenshot.");
    reply(true, QJsonObject{{"png", QString::fromLatin1(png.toBase64())}, {"width", out.width()},
                            {"height", out.height()}, {"fullWidth", fw}, {"fullHeight", fh},
                            {"x", x}, {"y", y}, {"regionW", w}, {"regionH", h}});
  } else if (op == "key") {
    if (!needConnected())
      return;
    int u = a.value("usage").toInt();
    if (u < 4 || u > 231)
      return fail("Invalid key usage.");
    m_c->key(u, a.value("down").toBool());
    reply(true, QJsonObject{});
  } else if (op == "pointer") {
    if (!needConnected())
      return;
    auto img = m_c->lastFrame();
    if (img.isNull())
      return fail("No frame yet.");
    int x = qBound(0, a.value("x").toInt(), img.width() - 1),
        y = qBound(0, a.value("y").toInt(), img.height() - 1);
    m_c->pointer(x, y, a.value("buttons").toInt() & 7, qBound(-1, a.value("wheel").toInt(), 1));
    reply(true, QJsonObject{});
  } else if (op == "release") {
    m_c->releaseInput();
    reply(true, QJsonObject{});
  } else if (op == "power") {
    if (!needConnected())
      return;
    auto action = a.value("action").toString();
    if (!QStringList{"on", "off", "reset", "soft"}.contains(action))
      return fail("Unknown power action.");
    m_c->releaseInput();
    note("power " + action);
    m_c->command({{"op", "power"}, {"action", action}}, [this, action, done](bool ok, QString m) {
      note(ok ? "power " + action + " accepted" : "power " + action + " failed: " + m);
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
    m_c->command({{"op", "mount"}, {"path", f.canonicalFilePath()}}, done);
  } else if (op == "eject") {
    if (!needConnected())
      return;
    note("eject ISO");
    m_c->command({{"op", "eject"}}, done);
  } else if (op == "instructions") {
    reply(true, QJsonObject{{"text", mcpInstructions()}});
  } else {
    fail("Unknown operation '" + op + "'.");
  }
}
