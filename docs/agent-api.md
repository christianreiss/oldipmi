# The agent socket

OldIPMI serves a local socket through which `oldipmi-mcp` drives the app for an AI agent. This page
describes that socket, version 1 of the agent API, for anyone who writes another client or changes
either side. The MCP tools that agents see are in the README and in
[`internal/mcp/instructions.md`](../internal/mcp/instructions.md).

## Where it is

* **Path:** `$OLDIPMI_AGENT_SOCKET` when it is set, otherwise `$XDG_RUNTIME_DIR/oldipmi/agent.sock`,
  or `<temp>/oldipmi-<uid>/oldipmi/agent.sock` without `XDG_RUNTIME_DIR`.
* **Access:** The directory belongs to the user, with mode 0700. OldIPMI does not serve the socket
  from a directory that is a link or that another user owns, and says why in Settings → AI agents
  (MCP). The socket accepts only the user's own processes. `oldipmi-mcp` also refuses a socket that
  another user's process serves. There is no further authentication: every process of the user may
  connect.
* **When:** The socket is served while "Allow AI agents to control OldIPMI" is on in Settings → AI
  agents (MCP), unless the app was started with `--no-agent-api` or `--self-test`. A second start
  of OldIPMI shows the window that runs, so one OldIPMI serves the socket.

## Messages

* **Framing:** one JSON object per line, in UTF-8, ending in `\n`.
* **Request:** `{"id": 1, "op": "status", "args": {}}`. The client chooses `id`, an integer, and
  the reply repeats it. `args` may be left out.
* **Success:** `{"id": 1, "ok": true, "result": {...}}`.
* **Failure:** `{"id": 1, "ok": false, "error": "Not connected (state: disconnected). Use connect
  first.", "code": "not_connected"}`. `error` is a sentence for people and agents; `code` is the
  kind of error (see [Errors](#errors)).
* **No request:** A line that is not a JSON object gets
  `{"id": 0, "ok": false, "error": "Invalid JSON request.", "code": "bad_request"}`.
* **Limits:** A line may be at most 1 MiB long. A longer one gets a `bad_request` reply, and the
  connection is closed. A client that stops reading is disconnected once 64 MiB of replies wait
  for it.
* **Order:** Most operations answer at once, in the order of the requests. `power`, `mount` and
  `eject` answer when the console has carried them out, possibly after the replies to later
  requests. A client should match replies by `id`.
* **Clients:** Several clients may be connected at once. When a client disconnects, OldIPMI lets
  go of the keys and mouse buttons that it holds down.

## Versions

The first request of a client is `hello`:

```json
{"id": 1, "op": "hello", "args": {"name": "claude-code", "version": "2.1.0",
  "protocol": "2025-06-18", "mcpVersion": "0.5.0"}}
```

* `name` and `version` are the MCP client's (the agent's program), and `protocol` is its MCP
  protocol version. Settings shows the name.
* `mcpVersion` is the version of `oldipmi-mcp`. Settings warns while it differs from OldIPMI's own
  version, or is missing, as from an `oldipmi-mcp` of OldIPMI 0.4.0 or older.

The reply names the app, its version, the version of the agent API and the operations it offers:

```json
{"id": 1, "ok": true, "result": {"app": "OldIPMI", "version": "0.5.0", "api": 1,
  "ops": ["hello", "list_profiles", "status", "connect", "disconnect", "screenshot", "key",
          "pointer", "release", "power", "mount", "eject"]}}
```

`api` changes only with a change that an older client would misread. A new operation or a new key
in a reply leaves it as it is. A client should not send an operation that is missing from `ops`.
It should also refuse an app whose `api` is higher than the one it knows. `oldipmi-mcp` does both,
and names both versions in the error. OldIPMI 0.4.0 and older answer `hello` without `api` and
`ops`. `oldipmi-mcp` then sends every operation and warns once on stderr.

## Choosing a console

Several consoles can be open at once. An operation on a console takes an optional `profile`: a
profile's id, or its name or host, either without regard to case. A name or host that several
profiles share names none of them: the operation fails as `ambiguous`, and the error lists those
profiles with their ids. Without `profile`, the operation acts on the only open console, and fails
as `ambiguous` when several are open. It fails as `no_profile` when no profile matches.

## Operations

### `hello`

See [Versions](#versions).

### `list_profiles`

No arguments. `profiles` lists the saved connections: `id`, `name`, `host`, `family` (`ami`,
`aten`, `avocent`, `cisco` or `demo`), `username` and `passwordSaved`.

### `status`

`profile` is optional. The result always has:

* `sessions`: the open consoles, and failed ones until they are closed or opened again, sorted by
  name, each with `id`, `name`, `host`, `family` and `state`.
* `passwordRequired`: whether OldIPMI's window asks the user for the password of the chosen
  profile, the one named or the only open console's, after a `connect` asked for it; with no
  profile named and no console open, whether it asks for any.
* `passwordProfile`, while the window asks for a password after a `connect`: the name of the
  profile it asks for, whichever console the request is about.

For the chosen console, or with none open, it also has:

* `state`: `logging_in`, `discovering` or `connecting` while the console is set up, then
  `connected`; `disconnected` when it is closed or failed.
* `message`: what the window shows, such as why the console failed.
* `mounted`: whether an ISO is mounted.
* `width` and `height`: the size of the last picture, or 0 before the first.
* `frameSeq` and `changeSeq`: the number of pictures, and of pictures that changed the screen.
* `msSinceChange`: the milliseconds since the screen last changed.
* `consoleInfo`: what the console reported about itself, such as `protocol` and `resolution`;
  empty while it is disconnected.
* `profile` (`id`, `name`, `host`, `family`), while the console is not disconnected.

### `connect`

`profile` is required. The result has `state` and `profile`, and `already: true` when the console
was open already. OldIPMI asks for the password in its window when none is saved; then the result
has `passwordRequired: true` and `state` `disconnected`; once the user has entered it, `status`
follows the setup. Errors: `no_profile`; `busy` while the window asks for another profile's
password; `failed` when the console could not be started, with the reason.

`connect` answers once the console is being set up. A client then asks `status` until `state` is
`connected` or `disconnected`.

### `disconnect`

Closes the chosen console, or cancels one that is being set up; a password prompt for it is
dismissed, and with no profile named and no console open, the open prompt. A prompt for another
profile stays open. With `all: true` it closes every console and dismisses any prompt.

The result has `state` (`disconnected`) and `stillOpen`, the profiles of the consoles that are
still open. With `all` it also has `closed` (the profiles closed) and `promptDismissed`. Otherwise
it has `previous` (the state before), `promptDismissed`, and `profile` when a console was open.

### `screenshot`

A PNG of the chosen console's screen. Arguments, all optional:

* `x`, `y`, `width` and `height`: the region in framebuffer pixels, by default the whole screen.
* `scale`: 0.1 to 1, by default 1.
* `maxSize`: the longest side of the image in pixels, which scales it down further; 0 or none for
  no limit.

The result has `png` (base64), `width` and `height` (of the PNG), `fullWidth` and `fullHeight`
(of the framebuffer), and `x`, `y`, `regionW` and `regionH` (the region). Errors: `no_frame` before
the first picture; `bad_args` for a region outside the framebuffer or a scale out of range;
`failed` when the PNG could not be made.

### `key`

`usage` is the key's USB HID usage, 4 to 231, and `down` whether it goes down or up. The result is
empty. OldIPMI only counts key presses, and never records which key. Errors: `not_connected`,
`bad_args`.

### `pointer`

`x` and `y` are framebuffer pixels. `buttons` holds the buttons down: 1 left, 2 right, 4 middle.
`wheel` is -1, 0 or 1 steps; 1 scrolls up. The result is empty. Errors: `not_connected`;
`no_frame` before the first picture; `bad_args` for a point outside the framebuffer.

### `release`

Lets go of every held key and button, of the console named in `profile` or else of every console,
whichever client pressed them. The result is empty.

### `power`

`action` is `on`, `off` (at once), `reset` or `soft` (graceful shutdown). Held keys and buttons
are let go first. The reply comes when the BMC has taken the command, within 15 seconds; its
result has a `message`. Errors: `not_connected`, `bad_args`, `failed`.

### `mount`

`path` is the absolute path of a readable `.iso` file. The reply comes when the BMC has mounted
it, within 30 seconds; its result has a `message`, which may be empty. Errors: `not_connected`,
`bad_args`, `failed`.

### `eject`

Ejects the ISO. The result has a `message`, which may be empty. Errors: `not_connected`, `failed`.

## Errors

| `code` | Meaning |
| --- | --- |
| `bad_request` | The line is no request: not a JSON object, or longer than 1 MiB. |
| `unknown_op` | OldIPMI does not offer the operation. |
| `bad_args` | An argument is out of range or of the wrong kind. |
| `no_profile` | No profile has the id, name or host given in `profile`. |
| `ambiguous` | Several profiles have the name or host given in `profile`; or several consoles are open, and the request names none of them. |
| `busy` | OldIPMI's window asks for the password of another profile. |
| `not_connected` | The console is not connected. |
| `no_frame` | The console has not sent a picture yet. |
| `failed` | OldIPMI, the console or the BMC could not carry it out; `error` says why. |

New codes may come; a client should treat an unknown one like `failed`.

## Time limits

OldIPMI answers most operations at once. `power` and `mount` wait for the BMC, at most 15 and 30
seconds; `eject` has no limit of its own. `oldipmi-mcp` waits for a reply at most 10 seconds, 20 for
`power`, 35 for `mount` and `eject`, and 60 for `connect`, which may wait while the wallet asks the
user to unlock it. When that time runs out, it tells the agent that the operation may or may not
have run. When the agent cancels the tool call, it stops waiting at once and sends nothing more for
it, except what lets go of the keys and buttons the call pressed. Either way it keeps the
connection and skips the late reply by its `id`.

## Changing the agent API

* [`testdata/agent/names.json`](../testdata/agent/names.json) lists the operations, the codes and
  the API version. `desktop/agentops.h` spells them for the app and `internal/mcp/agentops.go` the
  operations for `oldipmi-mcp`; `ipc_tests` and the Go tests check both against the file.
* [`testdata/agent/exchanges.json`](../testdata/agent/exchanges.json) records each operation as
  the app answers it against the demo console. `agent_tests` replays it, and the Go tests read its
  replies. After a change of a reply, record it again with
  `QT_QPA_PLATFORM=offscreen UPDATE_GOLDEN=1 tools/isolated.sh ./build/agent_tests repliesMatchTheRecordedExchanges`
  and review the diff.
* A new operation or key goes into both sides and the recording; `api` stays. A change that an
  older side would misread raises `api` in `names.json`, `agentops.h` and `agentops.go`.
