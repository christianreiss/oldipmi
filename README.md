# OldIPMI — native Linux BMC console

OldIPMI is a Go protocol engine with a Qt 6/Kirigami desktop interface for KDE
Plasma. Video is decoded directly into the native window. It needs no Java,
Docker, Podman, browser, web server, or VNC bridge.

This is a new implementation, not the old launcher repackaged. The previous
container version is recoverable at Git commit `ce0b1a7`.

## Build and run

On Fedora KDE, install Go, CMake, Ninja, a C++20 compiler, `qt6-qtbase-devel`,
`qt6-qtdeclarative-devel`, `kf6-kirigami-devel`, and `kf6-kwallet-devel`.

```sh
cmake -S . -B build -G Ninja -DCMAKE_BUILD_TYPE=Release
cmake --build build
./build/oldipmi
```

Lab-AMI, Lab-Avocent and Lab-Cisco appear as profiles on first launch. Use the play
button beside a profile, or select it and click **Open Console**. OldIPMI logs
in, discovers console parameters, and opens the native viewer automatically.
Progress shows **Logging in → Discovering console → Connecting → Connected**;
Cancel interrupts setup. Errors identify the stage that failed.

The first connection asks for a password. **Remember in KWallet** is enabled
by default; subsequent connections retrieve that profile's password from the
wallet. If KWallet cannot save it, choose **Use for this session**. Passwords are
never stored in the profile file. Changing a host or username requires a fresh
credential; editing only its name preserves its saved password.

**Console info** shows the actual protocol, endpoints and transport, available
console features, and the resolution received from the BMC. APCP also reports
its negotiated protocol version. Media endpoints are advertised parameters, not
proof that an ISO is mounted. Firmware/model information is not guessed.

Use **New**, **Save profile**, or **Remove** to manage profiles. Existing entries
and wallet IDs are preserved; missing default profiles are added once and deleted
defaults stay deleted. Lab-Cisco is configured for Cisco APCP, but the supplied
account remains rejected: provide a working account in its profile to connect.

For additional servers, select the BMC family and enter its hostname and account.
A fresh JNLP can supply an AMI/ATEN session token, but is never executed or used to
fetch code. Close/disconnect before selecting another host. Every credential-based
connection obtains a fresh session; imported JNLP files must also be fresh.

Click the console to capture input. Losing focus releases held keys/buttons.
The toolbar provides fit/actual-size, fullscreen, Ctrl+Alt+Del, ISO mount/eject
and confirmed power operations. Mounted ISO files are opened read-only.

## Transport compatibility

TLS verification is enabled by default. HTTP login, unverified/legacy TLS and
plain console/media transport are separate per-host exceptions, off by default
for new profiles. The three initial profiles carry their previously verified
compatibility settings: Lab-AMI enables all three, Lab-Avocent/Lab-Cisco legacy TLS only.
The legacy TLS exception also allows obsolete RSA/3DES suites when required.
No BMC TLS configuration, clocks or firmware are changed by OldIPMI.

ATEN firmware that no longer serves the Java iKVM (JNLP URL answers 404) is opened through
its HTML5 console automatically; the console info shows "ATEN HTML5". Virtual media is not
available in that mode.

Older AMI systems may have plain video and an always-TLS HID channel. On the
verified Lab-AMI generation, select AMI and explicitly enable HTTP login,
legacy/unverified TLS and plain console/media. Passwords/tokens never appear in
command-line arguments, profile files or diagnostic output.

For verified Lab-Avocent, select Avocent / Dell iDRAC and enable legacy/unverified TLS.
Avocent ISO media, JNLP import and packet-134 video are not implemented; the ISO
button is disabled for that family. Cisco uses port 2068, Dell uses port 5900.

ATEN systems requiring mutual TLS need their client certificate/key selected in
Compatibility. The application does not distribute vendor private keys. It also
does not change the BMC's Java/HTML5 console preference automatically.

## Compatibility and acceptance

| Family / target | Implementation | Current evidence |
|---|---|---|
| Legacy AMI / Lab-AMI, Supermicro X8DTU | 7-byte framing, TLS HID, RGB555 VSC raw and LRLE16/32 | Real native 640x480 video decoded and visually inspected |
| Newer AMI / ASPEED | 8-byte IVTP, VQ/JPEG decoder, HID, iUSB | Imported regression tests; no hardware acceptance yet |
| ATEN / Supermicro | Token login, Hermon and bit-plane video, HID, power, read-only media (Java iKVM) | Synthetic TCP handshake/video/cancellation tests; no Java-iKVM target supplied |
| ATEN HTML5 console (firmware without Java iKVM) | Automatic fallback: WebSocket + ASPEED AST2100 video (shared AMI codec), keyboard, pointer, power; no virtual media | Live: 1600x1200 video, delta updates, keyboard input verified on a LAN ATEN BMC (XCP-ng host); pointer and power not run live |
| Avocent / Lab-Avocent, Dell PowerEdge R420 | APCP, TLS companion channel, RGB555 stream, HID | Real native 640x480 video decoded and visually inspected |
| Cisco IMC / Lab-Cisco (bonus target) | APCP handshake supported | Account rejected by IMC API (551); KVM channel access denied; video unverified |

This is a development release. Both AMI and ATEN hardware acceptance remain
required for a completed release. Live keyboard injection, ISO mounting and
power/boot operations have not been run on production hardware. AMI single-port
HTTP CONNECT mode, encrypted HID at the application layer, non-RGB555 legacy
VSC modes, unsupported ATEN encodings, and relative pointer mode are not yet
implemented. Unsupported formats fail explicitly rather than launch Java.

## MCP: let an AI agent drive the console

`oldipmi-mcp` is a stdio [MCP](https://modelcontextprotocol.io) server that
controls the **running OldIPMI window**, so everything the agent does is visible.
Click **MCP** in the toolbar for the full instructions written for the AI (with
the exact path of `oldipmi-mcp` and a copy button); the same text is sent to MCP
clients as the server `instructions`.

```sh
claude mcp add oldipmi -- /path/to/oldipmi-mcp
```

From the AppImage the bundled path changes on every run: copy `oldipmi-mcp` (a static binary) to a
stable place such as `~/.local/bin`; the MCP button then shows that path.

Tools: `list_profiles`, `status`, `connect`, `disconnect`, `screenshot` (PNG, optional
crop/scale), `wait_for_change`, `wait_for_stable`, `type_text`, `key`, `key_combo`,
`release_input`, `mouse_move`, `mouse_click`, `mouse_double_click`, `mouse_drag`,
`mouse_scroll`, `power`, `mount_iso`, `eject_iso`. Passwords stay in KWallet and are never
visible to the agent; `connect` uses the saved password.

The app listens on a Unix socket, `$XDG_RUNTIME_DIR/oldipmi/agent.sock` (owner-only, no
TCP port). **There is no authentication**: any process of your user can control the
console, including `power` (immediate, no confirmation) and ISO mounting. Start OldIPMI
with `--no-agent-api` to disable it; `OLDIPMI_AGENT_SOCKET` overrides the path (Unix socket
paths are limited to about 107 bytes). A second OldIPMI instance does not take over a live socket.

## Portable bundle

```sh
python3 packaging/bundle.py
./dist/OldIPMI-x86_64.AppImage
```

Packaging additionally uses `mksquashfs`, `patchelf`, `qmake6` and
Python 3. It bundles the native executables, Qt/KDE QML modules, platform plugins,
fonts and shared-library dependencies, including a private glibc and loader.
GPU drivers remain host-provided. The bundle has passed an offscreen rendering
check in a clean Ubuntu 24.04 userspace; a full Ubuntu desktop session has not
been tested. The private loader avoids Fedora Qt libraries requiring a newer
host glibc. The release checksum is in `dist/SHA256SUMS`.

Without FUSE, use `--appimage-extract-and-run`, or `--appimage-extract` and run
`squashfs-root/AppRun`. `dist/OldIPMI.AppDir/AppRun` is the unpacked equivalent.
KWallet requires the host desktop's wallet service; without it use a session-only
password. Runtime license texts and an inventory of file checksums are included.

## Checks

```sh
go test -race ./...
go vet ./...
ctest --test-dir build --output-on-failure
go test ./internal/ami -run '^$' -fuzz FuzzLegacyDecode -fuzztime 10s
go test ./internal/aten -run '^$' -fuzz FuzzDecode -fuzztime 10s
```

`oldipmi --self-test` opens a synthetic framebuffer and exits after checking
rendering. `--preview` keeps that synthetic preview open. Neither contacts a BMC.
`oldipmi-core --probe` accepts `{ "profile": {...}, "password": "..." }` on
stdin and checks live video without injecting keys or changing power/media.
`oldipmi --probe-ui` accepts the same stdin JSON, resolves a saved profile by
hostname, and tests the complete desktop flow (including KWallet when enabled);
it prints only connection stages and allowlisted console information.
`oldipmi --preview` with the MCP server gives a BMC-free end-to-end check of `screenshot`.
`--capture PATH` saves the first frame to a new mode-0600 PNG, only when explicitly
requested. Never commit real console captures or credential-bearing JNLP files.

## Architecture

The GUI owns one backend child per connection. Two inherited AF_UNIX socketpairs
carry version-1 control messages and RGBA video frames. There are no listening
ports or installed background services. Control records are length-prefixed JSON
(maximum 1 MiB); setup state events include `logging_in`, `discovering`,
`connecting`, and `connected`, plus optional nonsecret `info` metadata; frame records carry width, height and bounded raw RGBA bytes.
A one-frame queue drops superseded display frames without delaying input.

Profiles live in `hosts.json` in the application's XDG config directory
(usually `~/.config/OldIPMI/OldIPMI`). The versioned object contains `version`,
`defaultsVersion`, and `profiles`; it is saved atomically with owner-only access.
The previous array format is migrated automatically, preserving a
`hosts.json.pre-profiles` backup. To roll back just the profiles feature, use
commit `8dd01fd` and restore that backup, or export the current object's
`profiles` array to the old `hosts.json` format. KWallet entries use unchanged
profile IDs. Removing a profile also removes its saved wallet entry. Old `.env` files are not
imported or deleted automatically. To roll back, build/run a separate checkout of
`ce0b1a7`; private `.env` configuration remains outside version control.

See [THIRD_PARTY.md](THIRD_PARTY.md) for provenance and licenses.
