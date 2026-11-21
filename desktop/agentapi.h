#pragma once
#include "controller.h"
#include <QDateTime>
#include <QJsonObject>
#include <QLocalServer>
#include <QTimer>
#include <QObject>
#include <QPointer>
#include <QSet>
#include <QVariantList>
#include <memory>

// Local, authentication-less control socket used by oldipmi-mcp. It only calls
// Controller and Session methods on the GUI thread; the socket file is owner-only.
//
// Protocol (docs/agent-api.md): one JSON object per line, {"id":N,"op":"...","args":{...}} ->
// {"id":N,"ok":true,"result":{...}} or {"id":N,"ok":false,"error":"...",
// "code":"..."}, where code is the kind of error (agentops::Err*).
// Zero or more console sessions can be open at once; session-bound operations take an
// optional args.profile (id, name or host) and otherwise act on the only open session.
// Several clients may be connected; each one's held keys and mouse buttons are
// released when it disconnects.
class AgentApi : public QObject {
  Q_OBJECT
  // Desired state: true while the socket should be served. Writing it starts or stops the server.
  Q_PROPERTY(bool enabled READ enabled WRITE setEnabled NOTIFY enabledChanged)
  // Actual state: true while the socket is bound and accepting clients.
  Q_PROPERTY(bool listening READ listening NOTIFY listeningChanged)
  // Why the last start() failed (empty when listening).
  Q_PROPERTY(QString errorString READ errorString NOTIFY listeningChanged)
  Q_PROPERTY(QString socketPath READ socketPath CONSTANT)
  Q_PROPERTY(QString mcpPath READ mcpPath CONSTANT)
  // Number of connected agent clients (oldipmi-mcp processes).
  Q_PROPERTY(int clientCount READ clientCount NOTIFY clientsChanged)
  // "name version" of the connected MCP client(s) as announced on initialize, comma-separated;
  // "unidentified client" for one that did not say hello; empty when none is connected.
  Q_PROPERTY(QString clientName READ clientName NOTIFY clientsChanged)
  // A warning while a connected agent's oldipmi-mcp, as its hello says, is not
  // of this OldIPMI's version or from before the version check; empty otherwise.
  Q_PROPERTY(QString versionNote READ versionNote NOTIFY clientsChanged)
  // Last 20 agent actions, newest first: {time (QDateTime), client, text}. Never contains
  // typed characters, key names or passwords; keyboard input is only counted.
  Q_PROPERTY(QVariantList activity READ activity NOTIFY activityChanged)
public:
  explicit AgentApi(Controller *controller, QString socketPath = {}, QObject *parent = nullptr);
  ~AgentApi() override;
  Q_INVOKABLE bool start();
  Q_INVOKABLE void stop();
  bool enabled() const { return m_enabled; }
  void setEnabled(bool on);
  bool listening() const { return m_server.isListening(); }
  QString errorString() const { return m_error; }
  QString socketPath() const { return m_path; }
  QString mcpPath() const;
  // Replaces the oldipmi-mcp path that is shown and registered (the self-test shows
  // the one of an installed OldIPMI).
  void setMcpPath(QString path) { m_mcpPath = std::move(path); }
  int clientCount() const { return int(m_clients.size()); }
  QString clientName() const;
  QString versionNote() const;
  QVariantList activity() const;
  Q_INVOKABLE void clearActivity();
  Q_INVOKABLE QString mcpInstructions() const;
  static QString defaultSocketPath();
  static constexpr int MaxActivity = 20;
  struct Client;
  // Runs one operation; `reply` is called exactly once (possibly later): with
  // the result, or with the error message and its code (agentops::Err*; empty
  // when ok). `client` is null for direct calls (tests); it scopes held-input
  // tracking and activity entries.
  using Reply = std::function<void(bool ok, QJsonValue result, QString code)>;
  void dispatch(const QString &op, const QJsonObject &args, Reply reply, Client *client = nullptr);
signals:
  void enabledChanged();
  void listeningChanged();
  void clientsChanged();
  void activityChanged();

private:
  struct Entry {
    QDateTime time;
    QString client, text, kind;
    int count = 1;
  };
  Controller *m_c;
  QString m_path, m_error, m_mcpPath;
  bool m_enabled = false;
  QLocalServer m_server;
  QTimer m_retry; // takes the socket over once another instance lets go of it
  // Shared with the handler that runs for a client, which keeps it while it
  // runs: a request can wait in a nested event loop (the wallet), during which
  // the client may leave (B-06).
  std::vector<std::shared_ptr<Client>> m_clients;
  QList<Entry> m_activity;
  // The profile whose password the window asks for after an agent's connect
  // opened the prompt; empty when it does not.
  QVariantMap m_prompt;
  QString promptId() const { return m_prompt.value("id").toString(); }
  QString m_requesting; // the profile id while an agent's connectHost() call runs
  void accept();
  void readClient(Client *c);
  void dropClient(Client *c);
  void releaseHeld(Client *c);
  static QString label(const Client *c);
  void log(const QString &who, const QString &kind, const QString &text, bool coalesce = false);
  // An action the user should notice: the note "Agent: <status>" in the status
  // bar, and the entry `activity` under `who` in the activity list.
  void audit(const QString &who, const QString &kind, const QString &activity, const QString &status);
  QVariantMap findProfile(const QString &key) const;
  // A request that cannot be done: its code (agentops::Err*) and the message.
  struct Failure {
    const char *code = nullptr;
    QString message;
  };
  // That no profile has the id, name or host key, with the names there are.
  Failure noProfile(const QString &key) const;
  // The session a request targets: args.profile, else the only open one. Null
  // when that console or every console is closed; null with *failure set
  // (ErrNoProfile, ErrAmbiguous) when no profile matches args.profile, or when
  // args names none and several consoles are open.
  Session *resolve(const QJsonObject &args, Failure *failure) const;

  // One request while dispatch runs it: its arguments, the client that sent
  // it (null for direct calls) and its reply.
  struct Call {
    const QJsonObject &args;
    Client *client;
    Reply reply;
    QString who() const { return label(client); }
    void succeed(const QJsonValue &result) const { reply(true, result, {}); }
    void fail(const char *code, const QString &message) const { reply(false, message, code); }
  };
  // The session c targets (resolve), which must be connected; null after
  // answering c with why not.
  Session *connected(const Call &c) const;
  // The ops dispatch answers and the method that answers each, in the order
  // the reply to hello lists them: that of testdata/agent/names.json.
  struct Op {
    const char *name;
    void (AgentApi::*run)(Call &);
  };
  static const Op Ops[];
  void opHello(Call &c);
  void opListProfiles(Call &c);
  void opStatus(Call &c);
  void opConnect(Call &c);
  void opDisconnect(Call &c);
  void opScreenshot(Call &c);
  void opKey(Call &c);
  void opPointer(Call &c);
  void opRelease(Call &c);
  void opPower(Call &c);
  void opMount(Call &c);
  void opEject(Call &c);
};
