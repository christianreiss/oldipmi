#include "families.h"
#include <QFile>
#include <QJsonDocument>
#include <QJsonObject>

QJsonArray familyTable() {
  static const QJsonArray table = [] {
    QFile f(":/families.json");
    if (!f.open(QIODevice::ReadOnly))
      qFatal("The family table :/families.json is not compiled in");
    QJsonParseError error{};
    const auto families = QJsonDocument::fromJson(f.readAll(), &error).object().value("families");
    if (!families.isArray())
      qFatal("The family table :/families.json has no families: %s", qPrintable(error.errorString()));
    return families.toArray();
  }();
  return table;
}

bool familyNeedsAccount(const QString &id) {
  for (const auto &v : familyTable())
    if (v.toObject().value("id").toString() == id)
      return v.toObject().value("account").toBool(true);
  return true;
}
