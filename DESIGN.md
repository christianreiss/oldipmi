# OldIPMI interface design

**Subject:** a desktop tool that opens the remote console of old server BMCs.
**User:** one administrator on KDE Plasma, opening a known server quickly, often under
pressure (server down, BIOS prompt, boot failure).
**The one job:** pick a server, see its screen, type into it.

## Register and tone

Product UI with its own calm, precise look that still sits naturally on Plasma:
system font, system colour scheme (light, dark or custom) and the user's accent
colour. The chrome is built from a few shared components (cards, chips, buttons,
dialog frame) on top of Kirigami and Qt Quick Controls; there are no gradients,
glow effects or shader-only effects (screenshots and CTest use the software
renderer). The remote screen sits on a **stage**: a rounded, always-dark backdrop
with a status strip, so the picture reads the same in every scheme.

The only artwork is one set of matching illustrations (`desktop/images`): one per
console family in the connection list, the console type tiles and the selected
connection, plus the first-run and error states. They share one style (flat
Breeze/Papirus look, slate body, one accent colour each, no text or vendor logos)
and must read on light and dark themes at 32 px; new ones are derived from
`family-aten.png` so the set stays uniform. Copy is plain and specific ("Login
failed", "Check the BMC address"), never marketing.

## Tokens

`desktop/qml/Theme.qml` is a singleton with every colour, size and font step. Its
base colours (window, text, view, accent, positive, warning, negative, the fixed
width font) are bound by `Main.qml` from `Kirigami.Theme`, because a singleton is in
no window; everything else is derived from them:

* **Surfaces:** sidebar, card, card footer, raised (tiles, secondary buttons),
  hover/pressed overlays, lines, scrim and shadow. Mixing is per channel in sRGB, so
  a colour keeps its hue when it is darkened or lightened.
* **Accent:** `accentStrong` (filled buttons; darkened until white text reaches
  4.6:1), `accentTint` (selection, tiles), `accentEdge`. `toneText()` moves a state
  colour towards the text colour until it reaches 4.5:1 on its surface (chips,
  warnings); `dangerStrong` is the filled red of destructive confirmations.
* **Stage:** constant dark colours for the stage, its strip, text and controls on it.
* **Sizes:** `gu` is the height of a line of text (the larger of Kirigami's grid unit
  and the font's line height), so a larger system font grows the chrome with it.
  Control and header heights, margins and panel widths are multiples of `gu`; corner
  radii are fixed (6/9/14/16 px).
* **Type:** factors of the system font (small, tiny, title, heading, display).

## Layout

```
┌──────────────────────┬───────────────────────────────────────────────────────────────────┐
│ ▣ OldIPMI     [+][≡] │ [◧] ▣ Server name [Send key][Power][ISO][⤢][⛶][ⓘ] [Disconnect][⏻] │
│ [⌕ Filter, connect…] │       host · type                                                 │
│ OPEN               2 │ ╭─────────────────────────────────────────────╮ ╭───────────────╮ │
│ ▤ Lab-ATEN             ⏏  │ │                                             │ │ Console       │ │
│   ● Connected        │ │       remote screen on the dark stage       │ │ information   │ │
│ CONNECTIONS        3 │ │                                             │ │ (toggled: ⓘ)  │ │
│ ▣ Lab-AMI          ▶  │ ├─────────────────────────────────────────────┤ │               │ │
│   lab-ami.example.net │ │ ⌨ Keyboard and mouse captured   [Release]   │ │               │ │
│ [Settings][Disc. all]│ ╰─────────────────────────────────────────────╯ ╰───────────────╯ │
└──────────────────────┴───────────────────────────────────────────────────────────────────┘
```

* **Connection list** (left): an *Open* group (live miniature of each open console,
  or its family picture until the first frame, with its state as dot *and* text) and
  a *Connections* group (family picture, name, address). The filter field connects:
  Enter opens the first match, or *New connection* to what was typed. Context menu
  (Show/Connect, Disconnect, Edit, Duplicate, Delete; the menu key or Shift+F10 opens
  it for the current row), keyboard (Tab lands on a row, Up/Down select, Enter
  connects, F2 edits, Del deletes). Rebuilding the list (a console opens or closes, a
  profile is saved) keeps the selection. Any number of consoles can be open at once;
  selecting an open row shows its console while the others keep running, and
  connecting an open profile again just shows it. Keyboard and mouse always go to the
  console in front. The footer holds Settings and Disconnect all.
* **Header** (right): sidebar toggle, the shown console's family, name and address,
  the console actions, Disconnect and Quit. Quit is always visible, also in full
  screen (its own button in the top right corner). With more than one console
  open, Quit (also Ctrl+Q or closing the window) asks first and names them, like
  Disconnect all; a quit request from the system (the session ending) does not.
* **Console pane** has exactly one page at a time:
  * *Welcome*: first run (New connection…, Try the demo console, the supported
    families) or "Select a connection".
  * *Selected connection*: a card with family, state chip, name, address (copy),
    account, last connection, password and security facts, **Connect**, Edit… and
    More (Duplicate, Delete…), and the list's keyboard shortcuts.
  * *Setup*: the family inside a turning ring, "Opening *name*…", user@host and the
    elapsed time, the three stages (Logging in → Discovering console → Connecting)
    and Cancel.
  * *Failure*: "Step *n* of 3 · *stage*", a title naming what failed, the likely fix,
    **Try again** and Edit connection…, then the technical details (selectable,
    Copy). A certificate that does not name the address asks to check the address
    first. The legacy TLS option is suggested for a certificate that cannot be
    verified, and when the BMC chose TLS 1.0 or 1.1, saying that it also skips
    checking the certificate and name; any other TLS failure, a bare alert
    included, gets a neutral hint. The option's switch says the same.
  * *Console*: the remote screen on the stage, an accent ring outside the picture
    while it has keyboard and mouse, and the status strip.
* **Console information** docks next to the stage when the pane has room for both
  (≥ 40 `gu`); in a narrower pane it floats over the stage with its own close button.
* **Full screen** hides header and sidebar while a console is shown; a floating bar
  (shown for a moment on entry, when the pointer reaches the top edge, and while the
  keyboard focus is in it or on Quit) holds identity, Release while captured or Take
  control while released, Send key, ISO, Power, fit, leave full screen and
  Disconnect. Quit keeps its own button in the top right corner the whole time;
  released, Tab reaches it and brings the bar, so the bar works without a mouse.
  Capturing shows a short note how to release (Right Ctrl, or the bar when that is
  switched off), releasing one how to take control again (Right Ctrl or a click,
  or the bar's Take control while the keyboard focus is in the bar).
  Without a console (a closed connection selected, connecting, failed, or after
  losing the connection) header and sidebar stay, with Leave full screen next to
  Quit.
* **Dialogs** share one frame (`AppDialog.qml`): heading, optional icon tile tinted
  by the tone, content, and a footer band with a hint on the left and the buttons on
  the right, the verb rightmost. An optional hint (a shortcut) shows only while it
  fits whole; a notice wraps. *New / Edit connection* has console type tiles
  (arrow keys move between them; a drop-down in a short window), address, name,
  username and password, a KWallet switch, and collapsed *Advanced options* (console
  mode, JNLP override, the three security exceptions with a warning, client
  certificate). Ctrl+Enter saves and connects.
* **Settings** use the Kirigami Addons configuration window: General, AI agents
  (MCP), About.

### Narrow windows and large text

Fixed buttons (sidebar toggle, Disconnect, Quit) never shrink; the title elides
first. Below 56 `gu` the ISO action shows only its icon, below 40 `gu` the toolbar
shows icons only and the information panel floats, and what still does not fit moves
to the toolbar's overflow menu. The status strip keeps, in this order, the capture
state, Release, how to release, and the resolution, and leaves out what does not fit
whole. A list narrower than 14 `gu` shows the family picture instead of the live
miniature, so names keep their room, and the filter field shows its Ctrl+F key cap
only while the whole placeholder fits beside it. Below 34 `gu` the full-screen bar
leaves out the family picture and the icons of Release and Take control, shows
Disconnect as an icon and starts at the left edge instead of the centre; the name
gives way first. The capture note keeps clear
of the Quit corner; when it does not fit on one line, the release instruction takes
its own line and the server name elides first. In a window lower than 24 `gu` the
connection dialog shows the console type as a drop-down and leaves out its
explanations, so the address field is in view; opening *Advanced options* scrolls
them into view. A welcome page lower than 20 `gu` has a small picture and heading
and its buttons right below the heading; a failure page that low leaves out its
picture, and a setup page shows a small ring and the current stage as one line
("Step 1 of 3 · Logging in") instead of the stepper, with Cancel right below. When
the selected connection's whole card does not fit, the card leaves out its picture
and puts Connect right below the name and address, before the details. Buttons on a
page that do not fit side by side wrap instead of widening it.

## Rules

* One primary action per state (Connect, Try again, New connection…, Save and
  Connect).
* Destructive or interrupting actions (power, delete, and Disconnect all or Quit
  with more than one console open) always confirm, naming the servers and the
  consequence; the confirming button carries the verb and acts on exactly what
  was named.
* State is never colour alone: emblem + text in the list, chips and status strip.
* The remote screen is the content: nothing is drawn over the picture except the
  floating full-screen bar, its capture note and the console information the user
  opened. The accent marks the primary action, the selection and keyboard capture.
* While the console is captured, every key goes to the server; app shortcuts do not
  fire. Release: tap Right Ctrl (configurable) or the Release button (strip or
  full-screen bar). Released, the next lone Right Ctrl tap takes control again
  while nothing else has the keyboard focus (right after a release, also in full
  screen); that tap is not sent. Right Ctrl pressed while another key is held, or
  with another key before its release, is a chord and does not take control, so
  typing never moves to the server unnoticed. Without a mouse, the capture chip in
  the status strip also takes control: Tab to it, then Enter or Space. With the
  Right Ctrl release switched off, releasing needs the mouse; the setting says so,
  and so does the capture chip (tooltip and accessible description) before the
  keyboard takes control with it.
* Only the picture is the remote screen: a click or wheel turn on the margin
  around it is not sent, since it would land on the edge of the remote screen. A
  drag that starts on the picture goes on to its edge, and moving over the margin
  keeps the remote pointer at the edge.
* Continuous motion (the setup ring) stands still when the system turns animations
  off; the stage names and the elapsed time carry the progress.
* The picture never moves or resizes when capture, focus or the status strip
  change: the strip has one fixed height and its Release button is always laid out.
* Text has at least 4.5:1 contrast on its surface, on light and dark schemes.
* Secrets never appear in the UI, logs or profile files.

## Verification

`oldipmi --self-test --screenshots DIR [--size WxH]` renders every state offscreen
(no BMC contact) for review: selected connection, new and edit dialogs, settings,
setup, failure, console with information, power confirmation, a second console,
captured, full screen (captured, released, and without a console), delete
confirmation, password prompt, advanced options, the Quit and Disconnect all
confirmations and the empty list. Each step waits until its state is reached.
The self-test, with or without the tour, fails on any QML warning from OldIPMI's
own files (exit 6). The tour fails with exit 5 when closing the window with two
consoles open does not ask first, when cancelling that question closes a
console, or when confirming Disconnect all leaves a console open; without
`--screenshots` (CTest
`desktop_smoke`) the self-test fails with exit 5 when the console view moves or
resizes on capture, release or window activation, when Enter on the capture chip
does not capture, when the focused connection list has no current row, when a list
rebuild changes the selection, when the menu key does not open the row's context
menu, when full screen does not show Quit, with or without a console, or when,
released in full screen, Tab does not reach the bar's Take control, the bar hides
while it has the keyboard focus, its note still offers Right Ctrl there, or Enter
there does not capture. The console unit tests (`console_tests`) cover the
capture rules, the Right Ctrl tap and the margin around the picture included.
`slop scan desktop`
checks the QML/C++ sources; `slop native --config tools/slop/native.json`
captures the state matrix in light and dark, two window sizes, scale 1 and 2 and
text scale 1 and 1.5, and `slop measure` checks text contrast on those PNGs. The
console states legitimately contain a large dark area (the stage and the
letterbox around the remote screen), so `renderChecks.maxBlack` is 0.25 instead
of 0.10.

Secondary text (addresses, captions, group headings, key hints) uses `Theme.muted`,
the text colour mixed 28% (dark: 30%) towards the window colour, not
`disabledTextColor`, which measured 2.5–2.8:1 in Breeze Light.
