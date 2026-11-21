package mcp

// The names of the agent socket, as testdata/agent/names.json lists them; the
// desktop spells the same in desktop/agentops.h. Both are checked against the
// file both ways.

// Ops: what a request asks. The client says hello first.
const (
	opHello        = "hello"
	opListProfiles = "list_profiles"
	opStatus       = "status"
	opConnect      = "connect"
	opDisconnect   = "disconnect"
	opScreenshot   = "screenshot"
	opKey          = "key"
	opPointer      = "pointer"
	opRelease      = "release"
	opPower        = "power"
	opMount        = "mount"
	opEject        = "eject"
)
