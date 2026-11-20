package backend

import "oldipmi/internal/protocol"

// The names of the desktop-core IPC, as testdata/ipc/names.json lists them;
// desktop/ipc.h spells the same for the desktop, and names_test.go and the
// desktop's ipc_test check both against the file.

// Ops: what a Command asks.
const (
	OpConnect    = "connect" // the first command; every other one needs a connected console
	OpDisconnect = "disconnect"
	OpKey        = "key"
	OpPointer    = "pointer"
	OpRelease    = "release"
	OpMount      = "mount"
	OpEject      = "eject"
	OpPower      = "power"
)

// Event types.
const (
	EventState = "state" // the console's state changed (State, and Info once known)
	EventOK    = "ok"    // the command with this ID is done
	EventError = "error" // the command with this ID failed or, without an ID, the console ended
	EventMedia = "media" // the ISO was mounted or ejected (Mounted)
)

// States: the setup stages (protocol.Stage*) and these two.
const (
	StateDisconnected = "disconnected"
	StateConnected    = "connected"
)

// stageLabel names a setup stage in the message of the error that ends it.
var stageLabel = map[string]string{
	protocol.StageLoggingIn:   "Login",
	protocol.StageDiscovering: "Console discovery",
	protocol.StageConnecting:  "Console connection",
}
