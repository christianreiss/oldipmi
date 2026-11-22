#pragma once
// The C++ types of the QML module OldIPMI (qt_add_qml_module in CMakeLists.txt).
// The console view and the miniature live in oldipmi_desktop, which knows
// nothing of QML modules; declared here, in the application, they are
// registered with the module and described in its type information, which
// qmllint and the QML compiler read.
#include "console.h"
#include "families.h"
#include "framethumbnail.h"
#include <QJsonArray>
#include <QObject>
#include <QtQml/qqmlregistration.h>

// The remote screen: draws the frames, takes the keyboard and the pointer.
struct ConsoleViewForeign {
  Q_GADGET
  QML_FOREIGN(Console)
  QML_NAMED_ELEMENT(ConsoleView)
};

// A live miniature of an open console, for the connection list.
struct FrameThumbnailForeign {
  Q_GADGET
  QML_FOREIGN(FrameThumbnail)
  QML_NAMED_ELEMENT(FrameThumbnail)
};

// The console families (desktop/families.json), which families.js reads.
class FamilyTable : public QObject {
  Q_OBJECT
  QML_ELEMENT
  QML_SINGLETON
  Q_PROPERTY(QJsonArray list READ list CONSTANT)
public:
  QJsonArray list() const { return familyTable(); }
};
