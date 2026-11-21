#include "controller.h"
#include "backendprocess.h"
#include "families.h"
#include "profilestore.h"
#include <QApplication>
#include <QClipboard>
#include <QFileDialog>
#include <QStandardPaths>
#include <QUuid>

static QString configPath() {
  return QStandardPaths::writableLocation(QStandardPaths::AppConfigLocation) +
         "/hosts.json";
}
Controller::Controller(QObject *p, Credentials *credentials, QString configFile) : QObject(p) {
  if (!credentials)
    m_ownedCredentials = kdeCredentials();
  m_profiles = std::make_unique<ProfileStore>(configFile.isEmpty() ? configPath() : configFile,
                                              credentials ? credentials : m_ownedCredentials.get());
  connect(m_profiles.get(), &ProfileStore::profilesChanged, this, &Controller::hostsChanged);
  connect(m_profiles.get(), &ProfileStore::failed, this, &Controller::fail);
  m_profiles->load();
  connect(qApp, &QGuiApplication::applicationStateChanged, this,
          [this](Qt::ApplicationState s) {
            if (s != Qt::ApplicationActive)
              releaseInput();
          });
}
QVariantList Controller::hosts() const { return m_profiles->profiles(); }
QString Controller::profilesPath() const { return m_profiles->path(); }
Controller::~Controller() {
  m_shown = nullptr;
  const auto all = m_sessions.values();
  for (auto *s : all) {
    disconnect(s, nullptr, this, nullptr);
    delete s; // its core logs out in the background
  }
  // Quitting waits for every core still logging out of its BMC, from the
  // consoles closed now and those closed before; the logouts overlap.
  qDeleteAll(findChildren<BackendProcess *>(Qt::FindDirectChildrenOnly));
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
QString Controller::errorCode() const {
  // A notice written after the session's message has no class.
  return m_shown && m_shown->messageStamp > m_messageStamp ? m_shown->errorCode : QString();
}
QVariantMap Controller::sessions() const {
  QVariantMap out;
  for (auto *s : m_sessions)
    out[s->id] = QVariantMap{{"state", s->stateName()}, {"errorStage", s->errorStageName()},
                             {"message", s->message}, {"startedAt", s->startedAt}};
  return out;
}
QStringList Controller::openIds() const {
  QStringList out;
  for (auto *s : m_sessions)
    if (s->state != Session::State::Disconnected)
      out << s->id;
  out.sort();
  return out;
}
int Controller::openCount() const { return int(openIds().size()); }
bool Controller::isOpen(QString id) const {
  auto *s = m_sessions.value(id);
  return s && s->state != Session::State::Disconnected;
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
  delete s; // its core logs out in the background, while this keeps it
}
void Controller::disconnectAll() {
  cancelCredentials();
  for (auto *s : m_sessions.values()) // none waits for another: the logouts overlap
    removeSession(s);
  emit changed();
}
QVariantMap Controller::saveHost(QVariantMap p, QString password, bool remember) {
  auto saved = m_profiles->save(p, password, remember);
  if (!saved.isEmpty()) {
    m_message = "Profile saved.";
    emit changed();
  }
  return saved;
}
bool Controller::removeHost(QString id) { return m_profiles->remove(id); }
QVariantMap Controller::host(QString id) const { return m_profiles->profile(id); }
QVariantMap Controller::duplicateHost(QString id) {
  auto saved = m_profiles->duplicate(id);
  if (!saved.isEmpty()) {
    m_message = "Duplicated as \"" + saved.value("name").toString() + "\".";
    emit changed();
  }
  return saved;
}
void Controller::sendKeys(QVariantList usages) {
  if (m_shown)
    m_shown->sendKeys(usages);
}
QSize Controller::copyFrame() {
  if (!hasFrame())
    return {};
  const QImage image = m_shown->lastFrame.convertToFormat(QImage::Format_RGB888);
  QGuiApplication::clipboard()->setImage(image);
  return image.size();
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
  // A family without an account (the demo console) needs no password and never touches KWallet.
  const bool account = familyNeedsAccount(p.value("family").toString());
  // Do not reuse a stored credential after changing its host or account.
  if (!entered && account && p.value("remember").toBool())
    password = m_profiles->savedPassword(p);
  if (password.isEmpty() && account) {
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
  m_profiles->stamp(saved.value("id").toString());
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
  if (!promptIsOurs && (!s || s->state == Session::State::Disconnected)) {
    if (s) {
      setMessage(s->message); // the failure text stays visible after its session is gone
      removeSession(s);
      emit changed();
    }
    return;
  }
  bool cancelled = !s || s->state != Session::State::Connected;
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
  // The console in front now gets the ISO. The dialog runs its own event loop,
  // in which an agent may bring another console to the front (C-09).
  const bool any = m_shown;
  const QString target = any ? m_shown->id : QString();
  auto f = QFileDialog::getOpenFileName(nullptr, "Mount read-only ISO", {},
                                        "ISO images (*.iso)");
  if (auto *s = any ? m_sessions.value(target) : nullptr; s && !f.isEmpty())
    s->mount(f);
}
void Controller::ejectISO() {
  if (m_shown)
    m_shown->eject();
}
void Controller::powerFor(QString id, QString action) {
  auto *s = id.isEmpty() ? m_shown : m_sessions.value(id);
  if (!s)
    return;
  s->releaseInput();
  s->power(action);
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
