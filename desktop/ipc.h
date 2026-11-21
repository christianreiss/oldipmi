#pragma once
// The names of the desktop-core IPC, as testdata/ipc/names.json lists them;
// the core spells the same in internal/backend/ipc.go and internal/protocol.
// ipc_test.cpp reads this file and checks it against names.json both ways, so
// it holds one constant per line, in one of the forms below. QML spells the
// states itself.

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

// Codes: the class of an error event without id (the setup or the console
// ended), for the error page's hint; an error of no known class has none.
inline constexpr char CodeDNS[] = "dns";
inline constexpr char CodeUnreachable[] = "unreachable";
inline constexpr char CodeRefused[] = "refused";
inline constexpr char CodeTLSName[] = "tls_name";
inline constexpr char CodeTLSUnverified[] = "tls_unverified";
inline constexpr char CodeTLSOld[] = "tls_old"; // the BMC chose TLS 1.0 or 1.1
inline constexpr char CodeTLS[] = "tls";
inline constexpr char CodeAuth[] = "auth";
inline constexpr char CodeTextMode[] = "text_mode";
inline constexpr char CodePlainConsole[] = "plain_console";

// The largest video frame the core sends (internal/video/raster); the desktop
// refuses a larger one.
inline constexpr unsigned MaxFrameWidth = 4096;
inline constexpr unsigned MaxFrameHeight = 2160;

} // namespace ipc
