#pragma once
// The names of the agent socket, as testdata/agent/names.json lists them;
// oldipmi-mcp spells the same in internal/mcp/agentops.go. ipc_test.cpp reads
// this file and checks it against names.json both ways, so it holds one
// constant per line, in the form below. The protocol: agentapi.h.

namespace agentops {

// Ops: what a request asks. oldipmi-mcp says hello first.
inline constexpr char OpHello[] = "hello";
inline constexpr char OpListProfiles[] = "list_profiles";
inline constexpr char OpStatus[] = "status";
inline constexpr char OpConnect[] = "connect";
inline constexpr char OpDisconnect[] = "disconnect";
inline constexpr char OpScreenshot[] = "screenshot";
inline constexpr char OpKey[] = "key";
inline constexpr char OpPointer[] = "pointer";
inline constexpr char OpRelease[] = "release";
inline constexpr char OpPower[] = "power";
inline constexpr char OpMount[] = "mount";
inline constexpr char OpEject[] = "eject";

} // namespace agentops
