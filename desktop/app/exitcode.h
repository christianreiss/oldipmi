#pragma once

// What oldipmi exits with. The numbers are part of its interface:
// desktop/probe_ui_test.sh checks 0, 6 and 7, and the README names 5 for the
// screenshot tour. Two meanings share 1 and two share 6, as they always did.
enum class ExitCode : int {
  Ok = 0,             // also when a running OldIPMI shows its window instead
  QmlNotLoaded = 1,   // Main.qml did not load
  BadArgument = 1,    // --size is not WIDTHxHEIGHT
  NoConsoleView = 2,  // Main.qml has no console view
  NoScreenshot = 3,   // --self-test could not grab the window
  ProbeFailed = 4,    // --probe-ui: no input, the connection failed, or it timed out
  CheckFailed = 5,    // --self-test or its tour: a check failed
  QmlWarnings = 6,    // --self-test or its tour: OldIPMI's QML warned
  AlreadyRunning = 6, // --probe-ui while OldIPMI runs
  NoPassword = 7,     // --probe-ui without a password
};

constexpr int code(ExitCode c) { return static_cast<int>(c); }
