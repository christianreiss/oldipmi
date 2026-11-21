#pragma once
#include "app/exitcode.h"
#include <QString>

namespace devtools {
// --self-test and its screenshot tour fail on any warning from OldIPMI's own QML
// (binding loops, type errors, unresolved names), so every UI state the tour
// visits is also checked for them.
// recordQmlWarnings() starts recording them; every message still goes where
// it went before. Only warnings from files below the root (the directory of
// Main.qml, empty until the engine loads it) count.
void recordQmlWarnings();
void setQmlRoot(const QString &root);
// Ok if there were none; otherwise lists them on stderr: QmlWarnings.
ExitCode qmlWarningsResult();
} // namespace devtools
