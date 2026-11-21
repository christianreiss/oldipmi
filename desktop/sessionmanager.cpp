#include "sessionmanager.h"
#include "backendprocess.h"
#include <QUuid>

SessionManager::SessionManager(QObject *parent) : QObject(parent) {}

SessionManager::~SessionManager() {
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

void SessionManager::track(Session *s) {
  connect(s, &Session::changed, this, &SessionManager::changed);
  connect(s, &Session::frameReady, this, [this, s](QImage im) {
    if (s == m_shown)
      emit frameReady(im);
  });
}

Session *SessionManager::start(const QVariantMap &p, const QString &password) {
  auto id = p.value("id").toString();
  if (id.isEmpty())
    id = QUuid::createUuid().toString(QUuid::WithoutBraces);
  if (auto *old = m_sessions.value(id))
    remove(old); // a failed or ended session of this profile makes way for the new one
  auto *s = new Session(id, p, this);
  track(s);
  m_sessions[id] = s;
  show(s);
  s->begin(password);
  return s;
}

void SessionManager::preview() {
  auto *s = new Session("", {{"id", ""}, {"name", "Preview"}, {"host", "synthetic framebuffer"}}, this);
  track(s);
  if (auto *old = m_sessions.value(s->id))
    remove(old);
  m_sessions[s->id] = s;
  show(s);
  s->preview();
}

void SessionManager::remove(Session *s) {
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

void SessionManager::show(Session *s) {
  if (s == m_shown)
    return;
  if (m_shown)
    m_shown->releaseInput(); // nothing stays held on a console that is no longer in front
  m_shown = s;
  emit frameReady(s ? s->lastFrame : QImage());
  emit changed();
}

QStringList SessionManager::openIds() const {
  QStringList out;
  for (auto *s : m_sessions)
    if (s->state != Session::State::Disconnected)
      out << s->id;
  out.sort();
  return out;
}

bool SessionManager::isOpen(const QString &id) const {
  auto *s = m_sessions.value(id);
  return s && s->state != Session::State::Disconnected;
}
