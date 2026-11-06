#include "settings.h"
#include <QCoreApplication>
#include <QRect>

AppSettings::AppSettings(QObject *parent) : QObject(parent) {}
void AppSettings::set(const char *key, const QVariant &v) {
  if (m_s.value(key) == v)
    return;
  m_s.setValue(key, v);
  emit changed();
}
QString AppSettings::version() const { return QCoreApplication::applicationVersion(); }
