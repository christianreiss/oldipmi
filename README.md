# OldIPMI — native Linux BMC console

OldIPMI is a Go protocol engine with a Qt 6/Kirigami desktop interface for KDE
Plasma. Video is decoded directly into the native window. It needs no Java,
Docker, Podman, browser, web server, or VNC bridge.

This is a new implementation, not the old launcher repackaged. The previous
container version is recoverable at Git commit `ce0b1a7`.

## Build and run

On Fedora KDE, install Go, CMake, Ninja, a C++20 compiler, `qt6-qtbase-devel`,
`qt6-qtdeclarative-devel`, `kf6-kirigami-devel`, `kf6-kirigami-addons` and
`kf6-kwallet-devel`.

```sh
cmake -S . -B build -G Ninja -DCMAKE_BUILD_TYPE=Release
cmake --build build
./build/oldipmi
```

## Using OldIPMI

The window has two panes (see [DESIGN.md](DESIGN.md)):

* **Connections** (left) lists every saved BMC with its address, console type and
  live state. Filter with Ctrl+F. Double-click, Enter or the play button connects;
  the context menu offers Connect with JNLP file, Edit (F2), Duplicate and Delete (Del).
  **+** (Ctrl+N) opens **New connection**.
* **New / Edit connection** is a dialog: BMC address (a pasted URL is reduced to
  its host), name, console type, username, password and *Remember password in
  KWallet*. **Advanced options** hold the Supermicro console mode, the per-host
  security exceptions, a JNLP endpoint override and client certificates.
  **Save and Connect** saves and opens the console in one step.
* The **console pane** (right) shows the selected connection, then the setup
  progress **Logging in → Discovering console → Connecting**, then the remote
  screen. Cancel interrupts setup. A failure names the stage, the cause and the
  likely fix, with **Try again** and **Edit connection…**.

Click the remote screen to give it keyboard and mouse; that first click is not sent
to the server. While captured, every key goes to the server, including OldIPMI's own
shortcuts. Tap **Right Ctrl** (the tap is still forwarded; can be switched off in
Settings) or click **Release** in the status bar to get them back. Losing focus
releases held keys and buttons.

While connected, the toolbar offers **Send key** (Ctrl+Alt+Del, Ctrl+Alt+F1/F2/F7,
Alt+Tab, Alt+F4, Print Screen, Super, Right Ctrl), **Mount ISO… / Eject ISO**
(only when the console reports read-only ISO support), **Power** (chassis
control over IPMI LAN, UDP 623, with the connection's account; every action is
confirmed with its consequence),
fit/actual size, full screen (F11, Esc leaves), **Console information** (protocol,
endpoints, transport, features, resolution; APCP also its protocol version) and
**Disconnect** (Ctrl+D). Mounted ISO files are opened read-only. Media endpoints
are advertised parameters, not proof that an ISO is mounted; firmware/model
information is not guessed.

The first connection asks for a password. *Remember in KWallet* is on by default
(Settings → General changes the default); later connections read the password
from the wallet. If KWallet cannot save it, choose **Connect without saving**.
Passwords are never stored in the profile file. Changing a host or username
requires a fresh credential; editing only the name keeps the saved password.
Duplicating a connection does not copy its password.

**Settings** (main menu ☰ or Ctrl+,) has three pages: *General* (KWallet default,
scaling, Right Ctrl release, shortcut list), *AI agents (MCP)* (agent control on/off,
status, setup, recent agent activity) and *About* (version, file locations).
Preferences are stored in `OldIPMI.conf` in the XDG config directory.

Lab-AMI, Lab-Avocent and Lab-Cisco are added once on first launch; deleted defaults stay
deleted. Lab-Cisco is configured for Cisco APCP, but the supplied account remains
rejected: provide a working account in its profile to connect. A fresh JNLP can
supply an AMI/ATEN session token, but is never executed or used to fetch code.
Every credential-based connection obtains a fresh session; imported JNLP files must
also be fresh.

## Transport compatibility

TLS verification is enabled by default. HTTP login, unverified/legacy TLS and
plain console/media transport are separate per-host exceptions, off by default
for new profiles. The three initial profiles carry their previously verified
compatibility settings: Lab-AMI enables all three, Lab-Avocent/Lab-Cisco legacy TLS only.
The legacy TLS exception also allows obsolete RSA/3DES suites when required.
No BMC TLS configuration, clocks or firmware are changed by OldIPMI.

### Supermicro (ATEN) generations

Choose **Supermicro (ATEN)** for X9 and newer boards and most X8 boards; X8 boards
with AMI MegaRAC firmware (such as the X8DTU) use **AMI MegaRAC**. OldIPMI reads the
BMC's login page first and sends the form that firmware expects (plain `name`/`pwd`
on X9-era firmware, base64 with `check=00` on later X10/X11), so it makes exactly one
login attempt. A rejected password is reported in the Login stage.

*Console mode* (Advanced options, profile key `consoleMode`): **Automatic** tries
the Java iKVM (`ikvm`, then `man_ikvm` JNLP; 8-, 10- and 22-argument layouts) and
falls back to the HTML5 console when the JNLP URL answers 404 *or* a web page
because Java is disabled; **Java iKVM** never falls back; **HTML5** skips the JNLP.
The console info then shows "ATEN HTML5"; virtual media is not available there.
When the HTML5 page is missing, or `login.cgi` is gone (X12/X13, AST2600), OldIPMI
uses a Redfish session and the Supermicro IKVM resource (experimental, see below).
OldIPMI never changes the BMC's Java/HTML5 preference.

Older AMI systems may have plain video and an always-TLS HID channel. On the
verified Lab-AMI generation, select AMI and explicitly enable HTTP login,
legacy/unverified TLS and plain console/media. Passwords/tokens never appear in
command-line arguments, profile files or diagnostic output.

For verified Lab-Avocent, select Dell iDRAC (Avocent) and enable legacy/unverified TLS.
Avocent ISO media, JNLP import and packet-134 video are not implemented; the ISO
action is not offered for that family. Cisco uses port 2068, Dell uses port 5900.

ATEN systems requiring mutual TLS need their client certificate/key selected in
the connection's advanced options. The application does not distribute vendor private keys. It also
does not change the BMC's Java/HTML5 console preference automatically.

## Compatibility and acceptance

| Family / target | Implementation | Current evidence |
|---|---|---|
| Legacy AMI / Lab-AMI, Supermicro X8DTU | 7-byte framing, TLS HID, RGB555 VSC raw and LRLE16/32 | Real native 640x480 video decoded and visually inspected; read-only probe re-run 2026-11-06 |
| Newer AMI / ASPEED | 8-byte IVTP, VQ/JPEG decoder, HID, iUSB | Imported regression tests; no hardware acceptance yet |
| ATEN / Supermicro | Token login, Hermon and bit-plane video, HID, power, read-only media (Java iKVM) | Synthetic TCP handshake/video/cancellation tests; no Java-iKVM target supplied |
| ATEN / Supermicro X9–X11 Java iKVM | Plain or base64 (`check=00`) login detected from the login page; 8/10/22-argument JNLP layouts; `ikvm`/`man_ikvm` URLs | Synthetic fixtures only; no Java-iKVM target supplied |
| Supermicro X12/X13 (Redfish iKVM) | Redfish session login, `Oem/Supermicro/IKVM` (X11: `Managers/1/IKVM`) discovery, HTML5 page reuse when it carries the known token | Synthetic only; AST2600 console page/stream unverified, fails with a generation-specific message |
| ATEN HTML5 console (firmware without Java iKVM) | Automatic fallback: WebSocket + ASPEED AST2100 video (shared AMI codec), keyboard, pointer, power; no virtual media | Live: 1600x1200 video, delta updates, keyboard input verified on a LAN ATEN BMC (XCP-ng host); base64 login form and HTML5 fallback re-verified read-only 2026-11-06; pointer and power not run live |
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
