#pragma once
#include <QObject>
#include <QVariant>

// Minimal stand-in for KLocalizedContext: Kirigami Addons calls i18n*() from
// QML. OldIPMI is English-only, so these return the source text with %1..%5
// substituted, and pick singular/plural by the count. Avoids a KI18n build dependency.
class LocalizedContext : public QObject {
  Q_OBJECT
public:
  using QObject::QObject;
  Q_INVOKABLE QString i18n(const QString &t, const QVariant &a1 = {}, const QVariant &a2 = {}, const QVariant &a3 = {}, const QVariant &a4 = {}, const QVariant &a5 = {}) const { return sub(t, a1, a2, a3, a4, a5); }
  Q_INVOKABLE QString i18nc(const QString &, const QString &t, const QVariant &a1 = {}, const QVariant &a2 = {}, const QVariant &a3 = {}, const QVariant &a4 = {}, const QVariant &a5 = {}) const { return sub(t, a1, a2, a3, a4, a5); }
  Q_INVOKABLE QString i18np(const QString &s, const QString &p, const QVariant &n, const QVariant &a1 = {}, const QVariant &a2 = {}, const QVariant &a3 = {}, const QVariant &a4 = {}, const QVariant &a5 = {}) const { return plural(s, p, n, a1, a2, a3, a4, a5); }
  Q_INVOKABLE QString i18ncp(const QString &, const QString &s, const QString &p, const QVariant &n, const QVariant &a1 = {}, const QVariant &a2 = {}, const QVariant &a3 = {}, const QVariant &a4 = {}, const QVariant &a5 = {}) const { return plural(s, p, n, a1, a2, a3, a4, a5); }
  Q_INVOKABLE QString i18nd(const QString &, const QString &t, const QVariant &a1 = {}, const QVariant &a2 = {}, const QVariant &a3 = {}, const QVariant &a4 = {}, const QVariant &a5 = {}) const { return sub(t, a1, a2, a3, a4, a5); }
  Q_INVOKABLE QString i18ndc(const QString &, const QString &, const QString &t, const QVariant &a1 = {}, const QVariant &a2 = {}, const QVariant &a3 = {}, const QVariant &a4 = {}, const QVariant &a5 = {}) const { return sub(t, a1, a2, a3, a4, a5); }
  Q_INVOKABLE QString i18ndp(const QString &, const QString &s, const QString &p, const QVariant &n, const QVariant &a1 = {}, const QVariant &a2 = {}, const QVariant &a3 = {}, const QVariant &a4 = {}, const QVariant &a5 = {}) const { return plural(s, p, n, a1, a2, a3, a4, a5); }
  Q_INVOKABLE QString i18ndcp(const QString &, const QString &, const QString &s, const QString &p, const QVariant &n, const QVariant &a1 = {}, const QVariant &a2 = {}, const QVariant &a3 = {}, const QVariant &a4 = {}, const QVariant &a5 = {}) const { return plural(s, p, n, a1, a2, a3, a4, a5); }

private:
  static QString sub(QString t, const QVariant &a1, const QVariant &a2, const QVariant &a3,
                     const QVariant &a4, const QVariant &a5) {
    const QVariant *args[] = {&a1, &a2, &a3, &a4, &a5};
    for (int i = 0; i < 5; ++i)
      if (args[i]->isValid())
        t.replace(QStringLiteral("%") + QString::number(i + 1), args[i]->toString());
    return t;
  }
  static QString plural(const QString &s, const QString &p, const QVariant &n, const QVariant &a1,
                        const QVariant &a2, const QVariant &a3, const QVariant &a4, const QVariant &a5) {
    // KI18n plural forms use %1 for the count; following args shift by one.
    return sub(n.toInt() == 1 ? s : p, n, a1, a2, a3, a4).replace("%6", a5.toString());
  }
};
