# Changelog

## Unreleased

### Added
* **OldIPMI and oldipmi-mcp check each other's version.** oldipmi-mcp tells
  the app its version, and the app answers with the operations it offers. An
  operation the running OldIPMI lacks now fails with both versions named
  instead of "Unknown operation", and Settings → AI agents warns while an
  agent uses an oldipmi-mcp of another version, such as an old copy in
  `~/.local/bin` after an update of the AppImage.
* **Each error of the agent socket names its kind.** An error reply on
  OldIPMI's agent socket carries a `code` beside its message, such as
  `not_connected`, `no_profile` or `ambiguous`, so that a program using the
  socket can tell the errors apart without reading the message.

### Changed
* **oldipmi-mcp waits for each operation only as long as it can take.** It
  waited 60 seconds for any reply of OldIPMI and then closed its connection,
  which also let go of every key the agent held down. It now waits at most 10
  seconds for an operation that OldIPMI answers at once, 20 for `power`, 35 for
  `mount_iso` and `eject_iso` and 60 for `connect`. When that time runs out, or
  the agent cancels the tool, it stops waiting and keeps the connection; a
  cancelled tool sends nothing more, except what lets go of the keys and
  buttons it pressed.

### Fixed
* **The power button says how the power is switched.** Its tooltip said
  "Chassis power over IPMI (UDP 623)" for every console. A Supermicro (ATEN)
  console switches the power through the console connection instead, and the
  demo console only pretends; the tooltip now says so. AMI, Dell iDRAC and
  Cisco IMC consoles do use IPMI, with the connection's account.
* **"Password not saved" appears only when KWallet failed.** Connecting
  showed that warning, with its offer to connect without saving, whenever the
  connection could not be saved: also for an address of only spaces, and for
  a profiles file that OldIPMI cannot read or write. Now only KWallet's own
  failures show it, when it cannot store the password or remove a stored one.
  Any other failure shows its message, such as "Could not save host
  profiles.", and the console does not open. An agent's `connect` fails with
  that message too, instead of leaving the warning open in the window.
* **The toolbar keeps its labels while the information panel is docked.** In
  a narrow window the console's toolbar shows only icons, and the information
  panel floats over the console instead of standing beside it. In a pane of
  exactly the width where that starts, the toolbar already showed only icons
  while the panel was still docked; both now change at the same width.
* **The setting to scale the remote screen works after the fit button.** Once
  the button that switches the console between fitting the window and its
  actual size had been used, the switch "Scale the remote screen to the
  window" in Settings → General no longer changed the console, only what the
  next start would use. The button now switches that setting, and the console
  follows it, whichever of the two changes it.
* **A password prompt that an agent opened belongs to its profile.** While
  OldIPMI asked for the password of a profile after an agent's `connect`,
  `status` reported `passwordRequired` for every console, and `disconnect` of
  another console, or of another profile of the same name, said that it had
  dismissed the prompt, which stayed open in the window. Both now go by the
  profile's id: `status` reports the prompt for its profile, and `disconnect`
  dismisses it for its profile, or, with no profile named, when no console is
  open.
* **A garbled message from the backend says so.** A control message from
  `oldipmi-core` that was not a JSON object ended the console with
  "Incompatible backend version.", which only a backend of another version
  should cause. It now ends it with "Invalid backend control message.", like a
  message of an impossible length.
* **Closing a console no longer freezes the window.** Closing a console
  waited for its backend to log out of the BMC, and the window froze
  meanwhile: up to 1.5 seconds, or 5 for a backend that did not end by
  itself. Now the window goes on at once and the backend logs out in the
  background. A console opened again on the same BMC starts after that
  logout, so a BMC that allows a single session sees the old one end first,
  and quitting still waits for every logout.
* **"Last connected" keeps up with the time.** On the page of a console that
  is not open, "Last connected" stayed at what it said when the page came
  up, such as "just now", until the profile list changed. It now moves on
  once a minute.
* **oldipmi-mcp says when it cannot read the app's reply.** A reply from
  OldIPMI without a field the tools read, or with a field of the wrong type,
  as from an OldIPMI of another version, was read as empty: a wait for a
  screen change then ran into its timeout, and `connect` and `disconnect`
  went on with what they could not read. The tool now fails with a message
  that names the reply and the field.
* **oldipmi-mcp refuses tool arguments of the wrong type.** A tool read some
  arguments of the wrong type as their default: an `all` that was not true or
  false as false, a `button` that was no word as left, `modifiers` that were
  no list as none. `mount_iso` passed any `path` on unchecked, and
  `screenshot` took a region beyond its own schema (x and y up to 4096, a
  width or height of 0). Each is now refused with a message that names the
  argument. An argument that is null counts as not given. Power actions and
  buttons may be written in any case, and the descriptions of `screenshot`'s
  `max_size` and `key`'s `name` give their limits.

## 0.4.0 (2026-11-21)

### Added
* **`packaging/smoke-test.sh`** runs the AppImage's self-test offscreen on the
  build host and in Podman containers with Ubuntu 22.04 and 24.04 and Debian 12
  and 13, which have no Qt or KDE of their own.
* **Copy screen image** in the console toolbar and the full-screen bar, and
  Ctrl+Shift+C while the console is released: puts the remote screen on the
  clipboard as an image at its own resolution, without the stage around it, and
  confirms it with a short note.
* **License:** OldIPMI is free software under the GNU General Public License,
  version 3 or later (`LICENSE`). The About page shows the notice and opens the
  license text; the RPM declares `GPL-3.0-or-later` and ships `LICENSE`, and so
  does the portable bundle.
* **Two more demo addresses:** `demo-hang` keeps the login waiting until you
  cancel it, and `demo-unreachable` fails like a BMC that does not answer, with
  its hint. Like the other demo variants, neither needs a network.
* **Agents can tell by name what a console supports:** `consoleInfo` in
  `status` lists `features` next to the capabilities shown in the window. So
  far it names only `media`, when an ISO can be mounted; power control and the
  rest have no name yet, so the list says nothing about them.

### Changed
* **OldIPMI is also listed under Internet** (the `Network` category) in the
  application menu, next to System, as remote access tools usually are.
* **The AppImage is smaller and names its version:**
  `OldIPMI-<version>-x86_64.AppImage`, 92 MB instead of 119 MB (234 MB instead
  of 296 MB unpacked). It carries only the Qt plugins OldIPMI uses: no readers
  for PDF, AVIF, JPEG XL, HEIF or OpenEXR images any more, and no TLS plugin,
  as the application itself opens no TLS connection. New is Qt's plugin for
  title bars where the compositor draws none, as GNOME does on Wayland.
* **New app id `io.uggs.OldIPMI`** (`net.alpha_labs.OldIPMI` before). The
  desktop file is now `io.uggs.OldIPMI.desktop`, and the window reports this id
  to the desktop. A launcher pinned to the task bar or the favourites with an
  older version points to the old file: pin OldIPMI again once. Profiles,
  settings and saved passwords stay where they are.
* **One version number, in `VERSION`.** CMake, both Go programs and the RPM build
  read it from there; `oldipmi-mcp` kept its own copy before. `oldipmi-core
  --version` prints it, and the User-Agent of the BMC web login names it
  (it said `OldIPMI/0.2`).
* **New app icon:** a server front with a glowing console prompt, in the style
  of the illustrations. It is plain SVG, so it stays sharp from the title bar to
  the welcome page. The About page now shows the bundled icon, like the sidebar,
  instead of an installed theme icon that may be missing or older.
* **A JNLP of the other BMC family always names the family to select.** If an
  AMI connection gets an ATEN JNLP from the BMC, or an ATEN connection an AMI
  one, the error says which family to select, now also when that JNLP is
  incomplete; such a JNLP showed the other family's parsing error before.
* **A connection keeps a console type this version does not know**, such as
  one saved by a newer OldIPMI. Editing it no longer turns it into a Supermicro
  (ATEN) connection: the dialog selects no type and says so, and saving keeps
  the stored type and its console options. The console pane names the stored
  type instead of “Supermicro”. Connecting still fails at login with the list
  of types to select.
* **A console without virtual media says so in the same words for every
  type.** Asked to mount an ISO, an Avocent or Cisco console now answers
  “virtual media is not available for this console type” instead of “Avocent
  virtual media is not implemented yet”, which a Cisco console said too. Only
  agents see this: the desktop offers no ISO action on these consoles.

### Removed
* **Preset connections:** a fresh installation starts with an empty connection
  list; nothing is built in any more. Earlier versions added three preset
  connections on first launch. Connections that already exist, presets included,
  stay as they are.

### Fixed
* **Icons in the AppImage outside Plasma.** Started on GNOME or in a bare
  container, the AppImage showed most buttons without their icons. It now uses
  the Breeze icons it brings there. On Plasma nothing changes, and an installed
  OldIPMI keeps the desktop's icons.
* **The AppImage brings the license of everything it bundles.** Libraries
  whose package keeps its license text in a sibling package, and the packages
  of the bundled plugins and QML modules, came without one. Now every bundled
  file must belong to an installed package with a license text, or no AppImage
  is built, and `runtime-packages.txt` names the source of each package. The
  runtime it downloads is cached only once its checksum matched, and its
  timestamps follow `SOURCE_DATE_EPOCH`.
* **The packages carry every license of the Go programs.** `oldipmi-core` and
  `oldipmi-mcp` contain the Go standard library; its license and the patent
  grant of Go and of the `golang.org/x` modules now come with the RPM and the
  bundle (`licenses/go`). `tools/check.sh` fails if a module linked into
  either program has no license there.
* **The window is ready within a second again, and OldIPMI needs far less
  memory.** While the window was being laid out, five pictures took their size
  from their own width: the sidebar's logo, the family picture of each
  connection, those on the welcome page, and the one in the console's header and
  full-screen bar. Each was loaded again at twice the size, up to 8192 × 8192
  pixels, and those not on screen kept that size. In a measurement with 17
  connections the window took 12 seconds to be ready and OldIPMI held up to
  900 MiB; now it is ready after half a second and holds about 130 MiB. The
  pictures look the same.
* **Quitting with several consoles open no longer waits for them one by one.**
  Each console logs out of its BMC when it closes, which can take up to three
  seconds. Quitting and Disconnect all waited for each logout before starting
  the next; now all consoles log out at the same time, and only the slowest
  counts.
* **A wrong account or password gets its hint on Supermicro (ATEN), Avocent
  and Cisco consoles too.** The error page suggests checking the username and
  password when the BMC rejects them, but it did not recognise how these
  consoles word that. It now goes by the kind of error the backend reports, and
  by the words only when the backend names none.
* **Typing and the picture go on while an ISO is being mounted.** Mounting,
  ejecting and power commands now run beside the console, one at a time and in
  the order they were given. Keys and mouse input no longer wait until the BMC
  has answered a mount, and the picture of an AMI console no longer stands still
  meanwhile. A mount now gives up after 30 seconds and a power command after 15,
  each with an error that says so; a mount had no time limit of its own before.
* **A long message in the status strip can be read whole.** A message that
  does not fit beside the console's picture is cut short with “…”, and nothing
  showed the rest. Hovering it now shows all of it, for example why an ISO went
  away or which time limit a mount ran into.
* **An ISO that goes away by itself says why.** When the BMC ends the virtual
  media or its connection drops, the status strip now says “The ISO is no
  longer mounted:” with the reason, until an ISO is mounted again. Before, only
  the “ISO mounted” note disappeared.
* **An AMI console ends when its keepalive fails.** The native AMI console
  (IVTP) sends a keepalive every 5 seconds but ignored a failed send, so a
  console whose BMC had stopped taking data could stay open with a frozen
  picture. It now ends, and the error page says that the keepalive could not
  be sent and why, as the Supermicro and Avocent consoles already ended when
  theirs failed.
* **`oldipmi-mcp` only talks to an OldIPMI of the same user.** It checks who
  serves the agent socket (`SO_PEERCRED`) and refuses one served by another
  user, who could otherwise see every key an agent types.
* **OldIPMI keeps its sockets in a directory of its own.** Without
  `XDG_RUNTIME_DIR` it created `/tmp/oldipmi-<uid>` with the default permissions
  and used it even if another user had created it first, who could then serve
  or watch the agent socket. Every level is now created with mode 0700, and a
  directory that is not the user's own (or a link) is refused: the agent socket
  then stays off with that reason, and the app starts without the check for a
  running instance.
* **`--probe-ui` no longer reports success while OldIPMI is running.** It ended
  with 0 without probing anything and brought the running window to the front;
  it now leaves that window alone and ends with 6. It also ends with 7 when the
  connection needs a password, instead of waiting 30 seconds for its timeout,
  and prints each stage's text and the size of the first picture.
* **A crashed backend says so.** When the backend process of a console crashed,
  OldIPMI said "The native backend could not run. Check the application
  bundle."; it now says that the backend crashed.
* **The backend's own messages reach the log.** What `oldipmi-core` writes to
  stderr, such as a Go panic or the `OLDIPMI_TRACE_AVPT` trace, now appears on
  OldIPMI's stderr (the terminal or the journal). It was kept in memory unread,
  and with tracing on that memory grew for as long as the console was open.
* **An ISO goes to the console it was chosen for.** If an agent brought another
  console to the front while the file dialog was open, the ISO was attached to
  that console instead.
* **Changing the Right Ctrl setting no longer brings back the full-screen
  note.** The note on taking or giving back the keyboard appeared again,
  although the capture had not changed.
* **A refused command no longer marks a server as failed.** When a console
  refused a command, such as a power action, before it showed its first picture,
  the connection list kept showing that server as "Console connection failed",
  even after it was disconnected normally.
* **The agent instructions name the right path to `oldipmi-mcp`.** Started by
  name from the PATH, `oldipmi-mcp` told the agent to run it from the agent's
  working directory instead of from where it is installed.
* **Agents are told in words that an ISO was attached or detached.** The
  `mount_iso` and `eject_iso` tools showed the app's raw reply, `{"message": ""}`,
  instead of their sentence ("ISO attached read-only; …", "Virtual media
  detached.").
* **A failure without a reason still reaches the agent as text.** When OldIPMI
  reported a failure without a message, for example a console that did not open
  and gave no reason, the agent got a text block without its text, which MCP does
  not allow. It now reads "The operation failed without an error message."
* **Agents are offered only a protocol version they can use.** When an agent
  named an MCP protocol version that `oldipmi-mcp` does not speak in a request's
  metadata, the error listed every version it knows, although such requests
  accept only the newest. It now lists just that one.
* **An agent's batched requests always get all their replies.** When an agent
  sent several requests in one batch and a tool call finished while the quick
  replies were still being collected, `oldipmi-mcp` could lose replies or send a
  damaged answer.
* **A hostile Avocent BMC can no longer stall a console with one picture.**
  For every two-colour pattern in a picture, the backend looked up to a screen
  row back for the second colour. A picture made only of such patterns over a
  plain area kept it busy for up to a minute. All the looking back of one picture
  is now limited to 16 pixels per pixel of the screen, which real pictures stay
  far below; they look exactly as before.
* **A Cisco IMC console no longer crashes when the screen size changes
  mid-picture.** A BMC that reported a new screen size while it was still sending
  a picture made the backend draw that picture past the end of the screen and
  crash. The unfinished picture is now dropped, and the next one is shown.
* **A malformed virtual-media request no longer ends an AMI console.** An AMI
  BMC that sent a cut-short read or write command crashed the backend, and a read
  for an absurd amount of data (up to 8 TiB) made it allocate that much memory.
  Such a request now ends only the virtual media session, as if the ISO had been
  ejected, and the reason goes to the log. Reads are limited to 1 MiB each, as on
  ATEN consoles; hosts ask for far less.
* **Consoles no longer drop when the computer is busy.** The backend gave the
  window only 3 seconds to take each screen update. On a heavily loaded machine,
  or while the window was busy closing another console, open consoles ended with
  "Connection ended". It now waits up to 30 seconds, and still ends a session
  whose window stopped responding altogether.
* **The connecting animation stands still when animations are turned off.** With
  animations turned off in the system settings, the ring around the server picture
  kept turning while a console opened: KDE then reports animations as 1 ms long, not
  0. It now stands still as intended; the stages and the elapsed time show the
  progress.

## 0.3.0 (2026-11-19)

### Changed
* The desktop interface is rebuilt on one design language (DESIGN.md): shared
  tokens in `Theme.qml` derived from the system colour scheme and accent, and shared
  components for buttons, chips, cards and dialogs. The remote screen sits on a
  rounded dark stage with a fixed-height status strip; an accent ring outside the
  picture shows keyboard capture.
* Connection list: open consoles are grouped at the top with a live miniature of
  their screen and their state in words (with the elapsed time while connecting);
  the filter field can start a new connection to the typed address; Settings and
  Disconnect all sit below the list.
* Every console pane state is its own page: first run (with the demo console),
  the selected connection's details with Connect, setup with the three stages and
  elapsed time, and failures with the step, a likely fix and copyable technical
  details. TLS failures tell a certificate that does not name the address (check
  the address first), a certificate that cannot be verified and TLS 1.0 or 1.1
  chosen by the BMC apart; only the last two suggest the legacy TLS option, the
  last saying that it also skips checking the certificate and name, as its switch
  now does too. A bare TLS alert gets a neutral hint.
* New / Edit connection: console type tiles, a two-column form, a KWallet switch and
  collapsible advanced options; Ctrl+Enter saves and connects. Each switch there
  gives its explanation to screen readers too. Power, delete, password and wallet
  prompts use the same dialog frame and name the server.
* Narrow windows and large text: the grid unit follows the font's line height, the
  header's fixed buttons (Quit included) never shrink, the console information
  floats over the console when there is no room next to it, and the status strip
  leaves out what does not fit whole instead of clipping it. In a short window the
  connection dialog shows the console type as a drop-down, so the address field is
  in view, opening **Advanced options** scrolls them into view, the welcome page
  puts its buttons right below the heading, and the selected connection's card
  puts Connect right below the name when the whole card does not fit. The error
  page's buttons come right after the likely fix, before the technical details,
  and wrap instead of widening the page. While connecting, a short window shows
  the current stage as one line with Cancel right below it. The filter field
  leaves out its Ctrl+F key cap when the placeholder would not fit beside it, and
  the connection dialog its Ctrl+Enter hint when the hint would not fit whole;
  the notice there (disconnect to connect with the changes) wraps instead.
* Full screen: the floating bar has **Release** while the console is captured (the
  way back when the Right Ctrl release is switched off) and **Take control** while
  it is released, and Quit stays in the top right corner the whole time. Released,
  Tab reaches Quit and brings the bar, which stays while the keyboard focus is in it
  or on Quit, so the bar works without a mouse; the note shown on releasing then
  points to Take control instead of the Right Ctrl tap, which does not take
  control from there. The bar and the capture note fit
  small screens; a long server name elides instead of pushing the release
  instruction off screen. Without a console (a closed connection selected,
  connecting, failed, or after losing the connection) full screen keeps the header
  with Quit and the connection list, instead of an empty page without a way out.
* **Disconnect all** (Ctrl+Shift+D) asks first and names the consoles when more
  than one is open; confirming closes exactly those sessions. Quitting with more
  than one console open (Quit, Ctrl+Q or closing the window) asks the same way; a
  quit request from the system, as when the session ends, does not ask.
* Settings → AI agents says when the session runs without the agent socket
  (`--no-agent-api`, `--self-test`), and that the switch then applies from the
  next start.
* The setup ring stands still when the system turns animations off.

### Added
* Taking control without a mouse: Tab to the capture chip in the status strip
  (*Click the screen to control the server*), then Enter or Space. With the Right
  Ctrl release switched off, the chip and the setting say that releasing then needs
  the mouse. The menu key or Shift+F10 opens the context menu of the current
  connection, and Tab into the list lands on a row. `oldipmi --self-test` fails when
  either does not work.
* Released, a tap on **Right Ctrl** gives keyboard and mouse back to the server
  (while nothing else has the keyboard focus), so full screen needs no mouse to
  take control again; a short note there says so. Right Ctrl together with another
  key does not count, so typing never moves to the server unnoticed.
  `oldipmi --self-test` also fails when full screen does not show Quit, with or
  without a console, or when its bar cannot be used from the keyboard.
* The screenshot tour (`--self-test --screenshots`) covers the captured console,
  full screen (captured, released and without a console), delete confirmation,
  password prompt, advanced options and the Quit and Disconnect all
  confirmations, waits for each state instead of fixed delays, and fails on any
  QML warning from OldIPMI's own files, when closing the window with two consoles
  open does not ask first, or when confirming Disconnect all leaves a console
  open.

### Fixed
* With the keyboard focus in the connection list, saving a profile or a console
  opening or closing no longer selects the first connection and shows its console.
  `oldipmi --self-test` checks this.
* The remote screen no longer zooms out and back in when the pointer enters and
  leaves the window (focus follows mouse) or when the console takes or releases the
  keyboard. The status bar's Release button, which is taller than the status text,
  now keeps its space and only fades in. In full screen the toolbar is shown over
  the console instead of pushing it down. `oldipmi --self-test` (CTest
  `desktop_smoke`) now fails when the console view changes size or position on
  capture, release, or window deactivation and reactivation.
* A click or wheel turn on the margin around the remote picture is no longer sent
  to the server, where it landed on the edge of the remote screen (on a taskbar or
  a window's close button, say). A drag that starts on the picture still ends at
  its edge. A wheel turn while the button of a click that was not sent (on the
  margin, or the click that takes control) is still held no longer leaves the
  server with that button pressed, and a sideways scroll no longer scrolls the
  remote screen down. `console_tests` checks this.

## 0.2.0-3 (Fedora packaging)

* Install Kirigami Add-ons in the buildroot so the offscreen desktop smoke test
  can resolve its QML imports in a clean Mock build.

## 0.2.0-2 (Fedora packaging)

* Build a single OldIPMI source RPM for multiple Fedora Mock targets, with
  separate results per target. An explicit Mock build fails if Mock is missing.
* Publish signed Fedora 43+ binary RPMs to repo.uggs.io using the package
  headers to choose RepoMan targets.

## 0.2.0 (2026-11-06)

### Added
* Fedora RPM: `packaging/fedora/build-rpm.sh` builds a source and a binary RPM into
  `dist/rpm/` from the committed HEAD, with the Go modules vendored into a second
  source tarball so the build (also in mock/COPR) needs no network. The spec's
  `%check` runs the Go tests, CTest offscreen and `desktop-file-validate`. The
  version comes from `CMakeLists.txt`. See README, "Fedora RPM".

### Single instance
* Only one OldIPMI runs per user session. Starting it again (menu, `oldipmi`) brings the
  running window to the front (passing the launcher's Wayland activation token) and
  exits. Before, a second window kept its own copy of the profiles and the one saved
  last silently overwrote the other's changes.
* A Quit button sits at the right end of the console toolbar (Ctrl+Q); the action
  buttons yield space to it and move to the overflow menu when the window is narrow.
* "Disconnect all" is now visible: it replaces Quit at the bottom of the connection
  list (enabled while any console is open; the tooltip names the count) and is in the
  main menu. Ctrl+Shift+D also works with a single open console. Quit stays in the
  console toolbar, the main menu and on Ctrl+Q.

### Artwork
* Each console family has its own illustration (Supermicro 1U server, AMI mainboard
  with BMC chip, Dell 2U server, Cisco blade, demo terminal) in the connection list
  and on the selected-connection page; the first run and failed connections show
  matching empty-rack and unplugged-server pictures instead of generic theme icons.
  The images are compiled into the binary, so they also show without an installed
  icon theme.

### Fixed
* ATEN / Supermicro HTML5 console on X12/X13-generation firmware connected but then
  timed out ("i/o timeout"): their console expects a client-type byte after the session
  token, which X10/X11 firmware must not get. OldIPMI now reads the BMC's own noVNC
  client (`/novnc/include/rfb.js`) to decide; if it cannot, it keeps the X10/X11 form.
  Verified live on two X12/X13 BMCs (cgi and Redfish discovery) and on X10/X11 HTML5.
* ATEN Java (RFB) console on old firmware (seen on firmware 2.50, iKVM 1.69.13)
  connected but never showed a picture: its mouse-status message (0x37) has a 2-byte
  body instead of 3, which desynchronised the stream. The length is now detected once
  per session from the bytes that follow. Verified live (752x413 video).

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
