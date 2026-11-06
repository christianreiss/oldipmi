# UI slop review — 2026-11-06

Tool: `slop` (slop-detector). In the first pass `slop scan desktop` checked
**0 files**: that release only understood web sources, not QML (see the second
pass below for the native run). The review was
therefore done by hand with `slop rubric` and the P0/P1 checklists
(`04-checklisten.md`) against offscreen screenshots of the 0.1 interface, then
repeated on the redesign (`oldipmi --self-test --screenshots DIR`).
These are review findings (kind: review), not measured results.

## Findings on the 0.1 interface

| # | Prio | Location | Finding | Resolution in 0.2 |
|---|---|---|---|---|
| 1 | P1 | `Main.qml` sidebar | List, editor, password, compatibility and certificate controls (20+) always visible at once; editing and connecting conflated (Open Console silently saved edits). | Connection manager list + modal New/Edit dialog; Advanced collapsed. |
| 2 | P1 | sidebar buttons | Three stacked full-width buttons (Save profile, Open Console, Open fresh JNLP) of equal weight: no clear primary action. | One primary action per state; JNLP in the context menu. |
| 3 | P0 | toolbar/bottom bar | Disabled controls (Cancel, Mount ISO, Ctrl+Alt+Del, Power) permanently visible while disconnected. | Actions appear only in the state where they work; Power/ISO only when the console reports the capability. |
| 4 | P0 | console area | Idle state is an empty dark rectangle; no first-run guidance; errors are one plain label at the bottom with no next step. | Placeholder states: first run, selected connection, three-stage progress, stage-specific failure with cause, hint, Try again and Edit. |
| 5 | P1 | profile list | Rows show only a name; selection vs. the play button ambiguous; no host, type or live state. | Rows show host · type, connection state as emblem + text, hover connect button, context menu. |
| 6 | P0 | compatibility checkboxes | Security-weakening options look identical to harmless ones; no explanation. | Switches with consequences spelled out; warning message when any is on. |
| 7 | P1 | certificate buttons | "Client certificate selected" without the file, no way to clear. | Path shown, Choose… and Clear. |
| 8 | P1 | remove dialog | Did not name the profile. | Names the connection and the KWallet consequence. |
| 9 | P1 | power dialog | Generic "Send 'reset' to host?". | Title names action and server; explains the consequence; verb button. |
| 10 | P1 | toolbar | Jargon "MCP" as a top-level button. | Main menu → AI agents (MCP) settings page. |
| 11 | P0 | keyboard | No shortcuts; app shortcuts would also have been stolen from the remote host once added; no visible capture state or release path. | Ctrl+N/F/D/Q, Ctrl+,, F9, F11; console swallows shortcuts while captured; status bar states capture; Right Ctrl tap or Release button. |
| 12 | P1 | `console.cpp` | Hard-coded colours (`#101318`, `#3daee9`) instead of theme. | Black letterbox (content), theme highlight for the focus frame. |
| 13 | P1 | window | Title never names the server; no settings, no about. | Title follows the connection; Settings window (General, AI agents, About). |

## Second pass with slop native (same day, 0.2.0 interface)

slop-detector gained QML/C++ scanning and `slop native` later the same day.
`slop scan desktop packaging tools` (20 files) leaves one signal: the preview
framebuffer fill in `controller.cpp`, suppressed as synthetic content.
`slop native --config tools/slop/native.json` captured 160/160 required
screenshots (10 states × light/dark × 1200x800/720x480 × dpr 1/2 × text 1/1.5).

| # | Prio | Evidence | Finding | Resolution |
|---|---|---|---|---|
| 14 | P1 | all dark captures | Monochrome icons dark-on-dark. Cause: the self-test enabled Qt test mode after `QApplication`, so the platform theme read the real `kdeglobals` while colours came from the test config. Capture artefact, not a product defect. | Test mode is now enabled before `QApplication`. |
| 15 | P1 | console, 720x480 | Info panel kept 16 grid units and left the console ~140 px wide; `WrapAnywhere` split words ("Po wer", "I SO"); text overflowed to the right at text scale 1.5. | Panel at most 40% of the pane, word wrap, content width bound to the panel. |
| 16 | P1 | error, 720x480 text 1.5 | Error placeholder overflowed: icon over the toolbar, Try again and Edit cut off. | Error state scrolls. |
| 17 | P1 | all, text 1.5 | Toolbar title + subtitle clipped at the bottom edge with larger fonts. | Toolbar height grows with its content; both panes stay aligned. |
| 18 | P1 | light, `slop measure` | Secondary text in `disabledTextColor` measured 2.5–2.8:1 (below 4.5:1). | Text colour mixed 15% to background: 5.0–5.2:1 light, 7.3–7.7:1 dark. |
| 19 | P2 | empty state | Themed `oldipmi` icon missing when not installed: generic file icon. | Bundled `qrc:/oldipmi.svg`. |
| 20 | P2 | error state | Raw Go error chain before the actionable hint; lowercase start. | Hint first, detail below, capitalised. |
| 21 | P2 | dialogs, 720x480 | `ConnectionDialog` binding loop on `y` when as tall as the window. | `maximumHeight` leaves room for the open animation. |
| 22 | P3 | AI agents page in self-test | Said "started with --no-agent-api" when the self-test had disabled it. | Names both causes. |
| — | P1 | console, power-confirm | `render-suspect`: ~16% pure black. | Intentional letterbox around the remote screen; `maxBlack` 0.25 (documented in DESIGN.md). |

Capture notes: offscreen Qt put the pointer at the window origin and rendered
false hover tooltips; the tour now parks it. The adapter sets colours, icon
theme and both font sizes through `~/.qttest/config/kdeglobals` and restores the
previous file.

## Remaining (not fixed)

* Kirigami ScrollablePage/ScrollBar emit two internal `TypeError`s when the
  settings window closes (upstream; also present before this pass).
* No import/export of connections.
* Accessibility tree not measured: slop has no AT-SPI exporter; keyboard order,
  screen reader names and zoom need a manual check with Accerciser or Orca.
* Not checked on a real Plasma session (only offscreen with the KDE platform theme).
