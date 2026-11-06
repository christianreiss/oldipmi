# Changelog

## 0.2.0 (2026-11-06)

### Fixed
* ATEN / Supermicro HTML5 console on X12/X13-generation firmware connected but then
  timed out ("i/o timeout"): their console expects a client-type byte after the session
  token, which X10/X11 firmware must not get. OldIPMI now reads the BMC's own noVNC
  client (`/novnc/include/rfb.js`) to decide; if it cannot, it keeps the X10/X11 form.
  Verified live on two X12/X13 BMCs (cgi and Redfish discovery) and on X10/X11 HTML5.

### Removed
* Importing a `.jnlp` file ("Connect with JNLP file…", the IPC `jnlp` field and the
  backend's imported-JNLP paths) is gone: every connection logs in and gets a fresh
  session. The Java iKVM launch file the BMC itself serves is still read for AMI and
  ATEN Java consoles, including the per-profile JNLP endpoint override.

### Multiple consoles
* Any number of consoles can be open at once (0..N) instead of one: connecting another
  profile no longer closes the open one. Selecting an open connection shows its console;
  the others keep running. Rows show their own state; the row button disconnects an open
  one; Ctrl+Shift+D disconnects all. The title shows "(+N)" for the other open consoles.
* MCP: all console tools take an optional `profile`; it may be omitted while one console
  is open and is required when several are (the error names them). `status` lists
  `sessions`; `disconnect` takes `profile` or `all`. `connect` no longer fails with
  "console is busy", and held input is tracked per console.

### Interface
* New two-pane layout: a connection manager (filter, live state as emblem and text,
  context menu with Connect, Edit, Duplicate, Delete) and a
  console pane with distinct states for first run, selected connection, three-stage
  setup progress, stage-specific failure (cause, likely fix, Try again, Edit) and the
  live console with a console-information side panel and a capture status bar.
* New/Edit connection is a modal dialog; security exceptions and client certificates
  moved to Advanced options and now explain their consequence and warn when enabled.
  Pasted URLs are reduced to the host. Duplicate never copies a password.
* Settings window (General, AI agents (MCP), About); preferences in `OldIPMI.conf`.
  The agent socket can be switched on and off at runtime.
* Keyboard: Ctrl+N, Ctrl+F, Ctrl+D, Ctrl+Q, Ctrl+,, F9, F11/Esc. While the console is
  captured all keys go to the server and app shortcuts do not fire; a lone Right Ctrl
  tap (configurable) or the Release button releases. The capturing click is no longer
  sent to the server. Send key menu (Ctrl+Alt+Del, Ctrl+Alt+F1/F2/F7, Alt+Tab, Alt+F4,
  Print Screen, Super, Right Ctrl).
* Power dialogs name the server and the consequence; delete names the connection.
  ISO actions appear only when the console reports read-only ISO support.
* Window title follows the connection; window geometry and last selection persist.
* `--self-test --screenshots DIR` renders every UI state offscreen for review; new
  console input tests cover capture, the swallowed capturing click, Right Ctrl release and
  window shortcuts (Ctrl+N, F11) staying with the server while captured.
  DESIGN.md and docs/slop-review-2026-11-06.md document the review.
* Cancelling the password prompt clears its hint from the console pane.
* Cisco IMC (Lab-Cisco): video arrives as Avocent packet 139 (RLE bit-slice, byte-plane
  or raw tiles, palette in packet 136), now decoded, so Lab-Cisco shows its console.
  RLE text mode is not drawn yet and fails with an explicit message. A console that
  connects but never delivers a picture shows "waiting for the first picture" and,
  on error, the connection-failed state. `OLDIPMI_TRACE_AVPT` traces AVPT packets.
* MCP deep dive: misspelt or unknown tool arguments are rejected instead of silently
  falling back to defaults; disconnect says what it did (disconnected from, cancelled,
  dismissed the prompt, nothing open); a second disconnect no longer turns the status
  message into "Connection cancelled."; status no longer reports the console info of a
  closed session; wait_for_stable's timeout message names the quiet time it missed.
* Hovering the connection list no longer makes the rows jump: the row's connect button
  keeps its space and only fades in.
* A second OldIPMI window takes over the agent socket once the first one closes
  (it used to stay without agent control until restarted).
* Settings and Quit buttons at the bottom of the connection list; Quit (also Ctrl+Q
  and the menu) stores the window geometry before exiting.
* Double-click, Enter, the row button or Connect on another connection switches
  consoles: the open one is disconnected first.
* slop native review (160 screenshots, light/dark, two sizes, scale 1/2, text 1/1.5):
  info panel capped at 40% of the pane with word wrap, scrollable error state with
  the hint before the raw error, toolbar grows with larger fonts, secondary text
  contrast 5:1 or better (was 2.5:1 in Breeze Light), bundled empty-state icon,
  no dialog binding loop in small windows. `--screenshots` takes `--size WxH`;
  `tools/slop/` holds the capture adapter.
* Self-test enables Qt test mode before `QApplication`, so it never reads the
  user's KDE colours, icons or fonts.
* Requires Kirigami Addons (`kf6-kirigami-addons`); the AppImage bundles it.

### AI agents (MCP)
* MCP 2025-11-25 and 2026-07-28 protocol support, JSON-RPC error handling, `ping` and
  cancellation during long tools, in-order tool execution (a pipelining bug found live).
* Full key coverage (F1–F24, keypad, SysRq/Break, locks, Menu, left/right modifiers),
  `key_down`/`key_up`/`repeat`, `list_keys`, `wait`; screenshots downscale to `max_size`
  with an exact coordinate mapping; `connect` waits for a password typed by the user.
* App side: several clients, per-client release of held input on failure or disconnect,
  robust line handling, runtime enable/disable, client name, activity list (no typed
  text) and a toolbar indicator while an agent is connected.
* New `demo` console family: a synthetic console for hardware-free tests.
* Verified: unit tests, Qt socket tests and a live offscreen run through `oldipmi-mcp`
  (all printable ASCII, combos, F13–F24, pointer clicks, scaling). Not run on hardware:
  F13–F24, keypad and SysRq on real BMCs.

* `release_input`, `mount_iso` and `eject_iso` answer with a sentence instead of `{}`;
  `eject_iso` says when nothing is mounted instead of reporting a silent no-op.
  `oldipmi-mcp` reports version 0.2.0 (was 0.3.0).

### Supermicro (ATEN)
* Detect the base64 + `check=00` login form of later X10/X11 firmware from the login
  page (one login attempt only); a rejected login is reported in the Login stage.
* Fall back to the HTML5 console when the Java iKVM is disabled (web page instead of
  JNLP), not only on 404; try `man_ikvm` as a second JNLP URL.
* New per-profile console mode (`consoleMode`: `auto`, `java`, `html5`).
* Parse ATEN JNLP layouts from iKVM 1.69.21 through 1.69.39 (8, 10 and 22 arguments);
  send distinct user/password tokens.
* Experimental Redfish discovery for Supermicro X11–X13 (session login, IKVM URI),
  with generation-specific errors when the console format is unsupported.
* Verified live, read-only: lab-aten (HTML5, 1600x1200, base64 login form) and Lab-AMI
  (AMI X8DTU, 640x480). Java iKVM and Redfish paths: synthetic tests only.

## ATEN HTML5 console (2026-11-05)

* Open ATEN BMCs without Java iKVM through their HTML5 console: WebSocket transport,
  session-token authentication and ASPEED AST2100 video decoded with the shared AMI codec
  (resolution limit raised to 1920 for 1600x1200 streams). Keyboard, pointer and power use the
  same messages as the Java protocol; virtual media is refused in this mode.
* Fix ATEN login: the BMC's expired empty `SID` cookie no longer shadows the real session cookie.
* Unchanged frames (the BMC answers every request) are dropped and polling is paced.
* Verified live on a LAN ATEN BMC: video, live updates and keyboard input. Not run live: pointer, power.

## MCP agent control (2026-11-05)

* Add `oldipmi-mcp`, a stdio MCP server with screenshot, keyboard, mouse, power,
  virtual-media, profile and connection tools for the running OldIPMI window.
* Add an owner-only, authentication-free Unix socket in the desktop app
  (`--no-agent-api` disables it) and a toolbar **MCP** button that shows AI instructions.
* Controller keeps the latest frame and reports backend results for power/media commands.
* Not verified on hardware: live keyboard/pointer injection, power and ISO mounting via MCP.
  Verified: unit tests, a Qt socket test and an offscreen end-to-end screenshot.

## Profile workflow (2026-10-18)

* Add Lab-AMI, Lab-Avocent and Lab-Cisco profiles once, preserving existing profiles and
  wallet IDs; deleting a default profile no longer causes it to reappear.
* Open a console in one click with KWallet passwords, automatic login/discovery,
  visible progress, setup cancellation and stage-specific failures.
* Display nonsecret console endpoints, transport, protocol, features and resolution.
* Add profile migration and Wallet failure tests, IPC cancellation tests and fresh
  session/metadata tests. Lab-AMI and Lab-Avocent desktop connections and KWallet
  reconnects were verified live; Lab-Cisco still requires a working account.

## 0.1.0 — native rewrite (2026-10-18)

* Replace the container, Python launcher, browser interface and virtual desktop
  with a Go backend and Qt 6/Kirigami desktop frontend.
* Add native legacy MegaRAC VSC and newer ASPEED/IVTP adapters, plus the ATEN
  Hermon/bit-plane adapter, bounded binary IPC and direct framebuffer display.
* Add saved host profiles, optional KWallet passwords, read-only ISO streaming,
  power controls, fresh-JNLP import and explicit per-host legacy transport settings.
* Add a native Avocent/iDRAC adapter with verified Lab-Avocent video; Cisco IMC
  negotiation is present but Lab-Cisco authentication remains pending.
* Add portable AppDir/AppImage packaging, decoder fuzz tests and native GUI checks.
* Hardware acceptance and firmware limitations are recorded in README.md.
