#pragma once
// The names of the agent socket, as testdata/agent/names.json lists them;
// oldipmi-mcp spells the ops the same in internal/mcp/agentops.go. ipc_test.cpp reads
// this file and checks it against names.json both ways, so it holds one
// constant per line, in the form below. The protocol: agentapi.h.

namespace agentops {

// Ops: what a request asks. oldipmi-mcp says hello first.
inline constexpr char OpHello[] = "hello";
inline constexpr char OpListProfiles[] = "list_profiles";
inline constexpr char OpCreateProfile[] = "create_profile";
inline constexpr char OpUpdateProfile[] = "update_profile";
inline constexpr char OpDeleteProfile[] = "delete_profile";
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

// Codes: the kind of error that an error reply names in code, beside its
// message. bad_request: the line is no request (invalid JSON, or over 1 MiB);
// unknown_op: the app does not offer the op; bad_args: an argument is out of
// range or of the wrong kind; no_profile: no profile has the id, name or host
// in profile; ambiguous: several profiles have the name or host in profile, or
// several consoles are open and the request names none; busy: a password
// prompt for another profile is open; not_connected: the console is not
// connected; no_frame: no picture has come yet; failed: the app, the console
// or the BMC could not do it.
inline constexpr char ErrBadRequest[] = "bad_request";
inline constexpr char ErrUnknownOp[] = "unknown_op";
inline constexpr char ErrBadArgs[] = "bad_args";
inline constexpr char ErrNoProfile[] = "no_profile";
inline constexpr char ErrAmbiguous[] = "ambiguous";
inline constexpr char ErrBusy[] = "busy";
inline constexpr char ErrNotConnected[] = "not_connected";
inline constexpr char ErrNoFrame[] = "no_frame";
inline constexpr char ErrForbidden[] = "forbidden";
inline constexpr char ErrFailed[] = "failed";

// The version of the agent API that the reply to hello reports, beside the
// ops the app offers. It changes only with a change that an older
// oldipmi-mcp would misread; new ops and keys leave it as it is.
inline constexpr int Api = 1;

} // namespace agentops
