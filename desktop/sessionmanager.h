#pragma once
#include "session.h"
#include <QHash>
#include <QImage>
#include <QObject>

// The consoles the app holds, open, failed or ended, at most one per profile
// id, and the one the console view shows. A console that goes lets its core
// log out in the background (Session::stop) and this keeps the core until it
// has ended: destroying the manager waits for those logouts.
class SessionManager : public QObject {
  Q_OBJECT
public:
  explicit SessionManager(QObject *parent = nullptr);
  ~SessionManager() override;
  // Opens a console for profile under its id, a new one for a profile
  // without, in place of a failed or ended console of that id, and shows it.
  Session *start(const QVariantMap &profile, const QString &password);
  // Shows a console without a core, which draws a synthetic picture.
  void preview();
  // Closes s and deletes it; the caller signals the change. If s was shown,
  // none is, and frameReady gets a null image. nullptr does nothing.
  void remove(Session *s);
  // Shows s in the console view; nullptr shows none.
  void show(Session *s);
  Session *shown() const { return m_shown; }
  Session *session(const QString &id) const { return m_sessions.value(id); }
  QList<Session *> all() const { return m_sessions.values(); }
  // Profile ids of the consoles being set up or connected (not failed or
  // ended ones), sorted.
  QStringList openIds() const;
  bool isOpen(const QString &id) const;

signals:
  // A console changed, or another one is shown.
  void changed();
  // The shown console's picture: each new frame, and the last one of a
  // console that comes to the front; a null image when none is shown.
  void frameReady(QImage image);

private:
  QHash<QString, Session *> m_sessions;
  Session *m_shown = nullptr;
  void track(Session *s);
};
