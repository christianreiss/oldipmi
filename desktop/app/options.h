#pragma once
#include <QSize>
#include <QString>
#include <QStringList>

// The command line of oldipmi. The README's "Checks" section describes the
// options for developers.
struct Options {
  bool test = false;     // --self-test: the demo console with test-mode paths, checked, then exit
  bool preview = false;  // --preview: the demo console, kept open
  bool probe = false;    // --probe-ui: connects to the connection on stdin (devtools/probeui.h)
  bool agentApi = true;  // false with --no-agent-api
  // --screenshots DIR, only with --self-test: the screenshot tour (devtools/tour.h).
  bool tour = false;
  QString tourDir;
  // --size WIDTHxHEIGHT for the tour; badSize when it is given but is not that.
  QSize tourSize;
  bool badSize = false;
};

// --self-test, read from argv: test mode must be on before the QApplication exists.
bool selfTestRequested(int argc, char **argv);
// The other options, from QCoreApplication::arguments(); test is selfTestRequested().
Options parseOptions(const QStringList &arguments, bool test);
