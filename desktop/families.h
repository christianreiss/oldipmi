#pragma once
#include <QJsonArray>
#include <QString>

// The console families oldipmi-core supports: desktop/families.json, compiled in
// as :/families.json. Its "about" says what each field means.
QJsonArray familyTable();
// False for a family without an account (no username, no password and no wallet
// entry: the demo console); true for any other id, known or not.
bool familyNeedsAccount(const QString &id);
