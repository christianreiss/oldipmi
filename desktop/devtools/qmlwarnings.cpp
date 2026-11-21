#include "devtools/qmlwarnings.h"
#include <QStringList>
#include <cstdio>

namespace devtools {
static QStringList s_qmlWarnings;
static QtMessageHandler s_previousHandler = nullptr;
static QString s_qmlRoot; // the directory of Main.qml; empty until the engine loads it
static void recordQmlWarning(QtMsgType type, const QMessageLogContext &context, const QString &message) {
  if (type != QtDebugMsg && type != QtInfoMsg && !s_qmlRoot.isEmpty() &&
      (message.contains(s_qmlRoot) || (context.file && QString::fromUtf8(context.file).startsWith(s_qmlRoot))))
    s_qmlWarnings << message;
  if (s_previousHandler)
    s_previousHandler(type, context, message);
  else
    fprintf(stderr, "%s\n", qPrintable(qFormatLogMessage(type, context, message)));
}
void recordQmlWarnings() { s_previousHandler = qInstallMessageHandler(recordQmlWarning); }
void setQmlRoot(const QString &root) { s_qmlRoot = root; }
ExitCode qmlWarningsResult() {
  if (s_qmlWarnings.isEmpty())
    return ExitCode::Ok;
  fprintf(stderr, "%lld QML warning(s):\n%s\n", static_cast<long long>(s_qmlWarnings.size()),
          qPrintable(s_qmlWarnings.join('\n')));
  return ExitCode::QmlWarnings;
}
} // namespace devtools
