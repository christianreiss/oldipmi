# UI slop review — 2026-11-06

Tool: `slop` (slop-detector). `slop scan desktop` checked **0 files**: the scanner
and its browser capture only understand web sources, not QML. The review was
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

## Remaining (not fixed)

* Kirigami Dialog/ConfigurationView emit harmless internal QML warnings on close.
* No import/export of connections.
* Visual review only; no automated accessibility audit exists for Qt Quick here.
