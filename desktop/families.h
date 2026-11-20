#pragma once
#include <QJSValue>
#include <QJsonArray>
#include <QString>

class QJSEngine;

// The console families oldipmi-core supports: desktop/families.json, compiled in
// as :/families.json. Its "about" says what each field means.
QJsonArray familyTable();
// False for a family without an account (no username, no password and no wallet
// entry: the demo console); true for any other id, known or not.
bool familyNeedsAccount(const QString &id);
// The table for QML, {list: [...]}: the FamilyTable singleton that
// qml/families.js imports.
QJSValue familyTableScript(QJSEngine *engine);
