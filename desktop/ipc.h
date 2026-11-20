#pragma once
// The names of the desktop-core IPC, as testdata/ipc/names.json lists them;
// the core spells the same in internal/backend/ipc.go and internal/protocol.
// ipc_test.cpp reads this file and checks it against names.json both ways, so
// it holds one constant per line, in the form below. QML spells the states
// itself.

namespace ipc {

// Ops: what a command asks. connect comes first; the others need a console.
inline constexpr char OpConnect[] = "connect";
inline constexpr char OpDisconnect[] = "disconnect";
inline constexpr char OpKey[] = "key";
inline constexpr char OpPointer[] = "pointer";
inline constexpr char OpRelease[] = "release";
inline constexpr char OpMount[] = "mount";
inline constexpr char OpEject[] = "eject";
inline constexpr char OpPower[] = "power";

// Event types.
inline constexpr char EventState[] = "state"; // the console's state changed
inline constexpr char EventOK[] = "ok";       // the command with this id is done
inline constexpr char EventError[] = "error"; // it failed or, without an id, the console ended
inline constexpr char EventMedia[] = "media"; // the ISO was mounted, ejected or failed

// A console's state: one of the setup stages, in order, or one of these two.
inline constexpr char StageLoggingIn[] = "logging_in";
inline constexpr char StageDiscovering[] = "discovering";
inline constexpr char StageConnecting[] = "connecting";
inline constexpr char StateConnected[] = "connected";
inline constexpr char StateDisconnected[] = "disconnected";

// The action of a power command.
inline constexpr char PowerOn[] = "on";
inline constexpr char PowerOff[] = "off"; // at once
inline constexpr char PowerReset[] = "reset";
inline constexpr char PowerSoft[] = "soft"; // graceful shutdown

} // namespace ipc
