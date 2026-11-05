# Changelog

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
