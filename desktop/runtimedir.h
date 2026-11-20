#pragma once
#include <QString>

// The directory of OldIPMI's agent socket and single-instance lock:
// $XDG_RUNTIME_DIR/oldipmi or, without XDG_RUNTIME_DIR, <temp>/oldipmi-<uid>/oldipmi
// (internal/mcp's SocketPath finds the agent socket there). Only the path.
QString runtimeDirPath();

// The same directory, created where missing with mode 0700. Each level OldIPMI
// creates must be a directory of this user (not a link); one that others can
// enter is tightened to 0700. In the shared temp directory another user could
// have made oldipmi-<uid> first and then serve or watch the sockets in it
// (B-12). Empty, with *error set, when a level fails that check.
QString runtimeDir(QString *error = nullptr);
