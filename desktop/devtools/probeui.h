#pragma once

class Controller;
class QApplication;
class QQmlApplicationEngine;

namespace devtools {
// --probe-ui: reads {"profile": {...}, "password": "..."} from stdin, resolves a
// saved profile by its host when the profile has no id, and connects to it in
// the window. Prints every change (state, message, allowlisted console
// information) and the size of the first picture as JSON lines, and makes the
// application exit: Ok once a picture arrives, ProbeFailed when the connection
// fails or takes 30 s, NoPassword when it needs a password (README, "Checks").
// False when stdin cannot be read.
bool startProbeUi(QApplication &app, QQmlApplicationEngine &engine, Controller &controller);
} // namespace devtools
