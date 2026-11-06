#pragma once
#include <QObject>
#include <QRect>
#include <QSettings>

// Application preferences (not connection profiles), stored in OldIPMI.conf in
// the XDG config directory. Every property writes through immediately.
class AppSettings : public QObject {
  Q_OBJECT
  Q_PROPERTY(bool rememberPasswords READ rememberPasswords WRITE setRememberPasswords NOTIFY changed)
  Q_PROPERTY(bool fitToWindow READ fitToWindow WRITE setFitToWindow NOTIFY changed)
  Q_PROPERTY(bool rightCtrlReleases READ rightCtrlReleases WRITE setRightCtrlReleases NOTIFY changed)
  Q_PROPERTY(bool agentApiEnabled READ agentApiEnabled WRITE setAgentApiEnabled NOTIFY changed)
  Q_PROPERTY(bool showSidebar READ showSidebar WRITE setShowSidebar NOTIFY changed)
  Q_PROPERTY(QString lastProfile READ lastProfile WRITE setLastProfile NOTIFY changed)
  Q_PROPERTY(QRect windowGeometry READ windowGeometry WRITE setWindowGeometry NOTIFY changed)
public:
  explicit AppSettings(QObject *parent = nullptr);
  bool rememberPasswords() const { return get("rememberPasswords", true).toBool(); }
  bool fitToWindow() const { return get("fitToWindow", true).toBool(); }
  bool rightCtrlReleases() const { return get("rightCtrlReleases", true).toBool(); }
  bool agentApiEnabled() const { return get("agentApiEnabled", true).toBool(); }
  bool showSidebar() const { return get("showSidebar", true).toBool(); }
  QString lastProfile() const { return get("lastProfile", QString()).toString(); }
  QRect windowGeometry() const { return get("windowGeometry", QRect()).toRect(); }
  void setRememberPasswords(bool v) { set("rememberPasswords", v); }
  void setFitToWindow(bool v) { set("fitToWindow", v); }
  void setRightCtrlReleases(bool v) { set("rightCtrlReleases", v); }
  void setAgentApiEnabled(bool v) { set("agentApiEnabled", v); }
  void setShowSidebar(bool v) { set("showSidebar", v); }
  void setLastProfile(const QString &v) { set("lastProfile", v); }
  void setWindowGeometry(const QRect &v) { set("windowGeometry", v); }
  Q_INVOKABLE QString configPath() const { return m_s.fileName(); }
  Q_INVOKABLE QString version() const;
signals:
  void changed();

private:
  mutable QSettings m_s;
  QVariant get(const char *key, const QVariant &def) const { return m_s.value(key, def); }
  void set(const char *key, const QVariant &v);
};
