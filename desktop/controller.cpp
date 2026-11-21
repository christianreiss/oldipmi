#include "controller.h"
#include "families.h"
#include "filedialogs.h"
#include "profilestore.h"
#include <QApplication>
#include <QClipboard>
#include <QStandardPaths>

static QString configPath() {
  return QStandardPaths::writableLocation(QStandardPaths::AppConfigLocation) +
         "/hosts.json";
}
Controller::Controller(QObject *p, Credentials *credentials, QString configFile, FileDialogs *dialogs)
    : QObject(p) {
  m_sessions = std::make_unique<SessionManager>();
  connect(m_sessions.get(), &SessionManager::changed, this, &Controller::changed);
  connect(m_sessions.get(), &SessionManager::frameReady, this, &Controller::frameReady);
  if (!credentials)
    m_ownedCredentials = kdeCredentials();
  m_profiles = std::make_unique<ProfileStore>(configFile.isEmpty() ? configPath() : configFile,
                                              credentials ? credentials : m_ownedCredentials.get());
  connect(m_profiles.get(), &ProfileStore::profilesChanged, this, &Controller::hostsChanged);
  connect(m_profiles.get(), &ProfileStore::failed, this, &Controller::fail);
  m_profiles->load();
  if (!dialogs)
    m_ownedDialogs = std::make_unique<FileDialogs>();
  m_dialogs = dialogs ? dialogs : m_ownedDialogs.get();
  connect(qApp, &QGuiApplication::applicationStateChanged, this,
          [this](Qt::ApplicationState s) {
            if (s != Qt::ApplicationActive)
              releaseInput();
          });
}
QVariantList Controller::hosts() const { return m_profiles->profiles(); }
QString Controller::profilesPath() const { return m_profiles->path(); }
// The sessions go first, as m_sessions is the last member: quitting waits
// there for every core still logging out of its BMC.
Controller::~Controller() = default;
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
  return shown() && shown()->messageStamp > m_messageStamp ? shown()->message : m_message;
}
QString Controller::errorCode() const {
  // A notice written after the session's message has no class.
  return shown() && shown()->messageStamp > m_messageStamp ? shown()->errorCode : QString();
}
QVariantMap Controller::sessions() const {
  QVariantMap out;
  for (auto *s : m_sessions->all())
    out[s->id] = QVariantMap{{"state", s->stateName()}, {"errorStage", s->errorStageName()},
                             {"message", s->message}, {"startedAt", s->startedAt}};
  return out;
}
QStringList Controller::openIds() const { return m_sessions->openIds(); }
int Controller::openCount() const { return int(openIds().size()); }
bool Controller::isOpen(QString id) const { return m_sessions->isOpen(id); }
void Controller::show(QString id) { m_sessions->show(m_sessions->session(id)); }
void Controller::disconnectAll() {
  cancelCredentials();
  for (auto *s : m_sessions->all()) // none waits for another: the logouts overlap
    m_sessions->remove(s);
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
  if (shown())
    shown()->sendKeys(usages);
}
QSize Controller::copyFrame() {
  if (!hasFrame())
    return {};
  const QImage image = shown()->lastFrame.convertToFormat(QImage::Format_RGB888);
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
  m_sessions->start(saved, password);
  m_profiles->stamp(saved.value("id").toString());
}
void Controller::continueWithoutWallet() {
  if (m_pendingPassword.isEmpty())
    return;
  auto profile = m_pendingProfile;
  auto password = m_pendingPassword;
  cancelCredentials();
  m_sessions->start(profile, password);
}
void Controller::cancelCredentials() {
  m_pendingPassword.clear();
  m_pendingProfile.clear();
  if (m_message.startsWith("Enter the password")) {
    setMessage({});
    emit changed();
  }
}
void Controller::disconnectHost() { disconnectProfile(shownId()); }
void Controller::disconnectProfile(QString id) {
  auto *s = m_sessions->session(id);
  bool promptIsOurs = !m_pendingProfile.isEmpty() &&
                      (id.isEmpty() || m_pendingProfile.value("id").toString() == id);
  // Nothing open and no password prompt: only dismiss a shown failure and keep
  // the last message, instead of reporting a cancellation that did not happen.
  if (!promptIsOurs && (!s || s->state == Session::State::Disconnected)) {
    if (s) {
      setMessage(s->message); // the failure text stays visible after its session is gone
      m_sessions->remove(s);
      emit changed();
    }
    return;
  }
  bool cancelled = !s || s->state != Session::State::Connected;
  if (promptIsOurs)
    cancelCredentials();
  m_sessions->remove(s);
  setMessage(cancelled ? "Connection cancelled." : "Disconnected.");
  emit changed();
}
void Controller::key(int usage, bool down) {
  if (shown())
    shown()->key(usage, down);
}
void Controller::pointer(int x, int y, int buttons, int wheel) {
  if (shown())
    shown()->pointer(x, y, buttons, wheel);
}
void Controller::releaseInput() {
  if (shown())
    shown()->releaseInput();
}
void Controller::mountISO() {
  releaseInput();
  // The console in front now gets the ISO. The dialog runs its own event loop,
  // in which an agent may bring another console to the front (C-09).
  const bool any = shown();
  const QString target = any ? shown()->id : QString();
  auto f = m_dialogs->isoImage();
  if (auto *s = any ? m_sessions->session(target) : nullptr; s && !f.isEmpty())
    s->mount(f);
}
void Controller::ejectISO() {
  if (shown())
    shown()->eject();
}
void Controller::powerFor(QString id, QString action) {
  auto *s = id.isEmpty() ? shown() : m_sessions->session(id);
  if (!s)
    return;
  s->releaseInput();
  s->power(action);
}
QString Controller::chooseCertificate(bool key) { return m_dialogs->clientCertificate(key); }
void Controller::preview() { m_sessions->preview(); }
