# OldIPMI interface design

**Subject:** a desktop tool that opens the remote console of old server BMCs.
**User:** one administrator on KDE Plasma, opening a known server quickly, often under
pressure (server down, BIOS prompt, boot failure).
**The one job:** pick a server, see its screen, type into it.

## Register and tone

Product UI, not brand. It must look like a KDE application next to Dolphin or
System Settings: Breeze, Kirigami and Kirigami Addons components, system font,
system colours, system light/dark. No custom palette, gradients or decorative
accents. Copy is plain and specific ("Login failed", "Check the BMC address"), never
marketing.

## Layout

```
┌ Connections  [+] [≡] ┬ [◧] Server name            [Send key][ISO][Power][⤢][ⓘ][Disconnect] ┐
│ [Filter…]            │   host · type / protocol · resolution                                │
│ ▣ Lab-AMI       ▶     ├──────────────────────────────────────────────────────────────────────┤
│   host · type        │                                                                      │
│ ▣ Lab-ATEN                │        remote screen (black letterbox)  |  console info panel        │
│   Connected          │                                                                      │
│                      ├──────────────────────────────────────────────────────────────────────┤
│                      │ ⌨ Keyboard and mouse go to the server. Tap Right Ctrl…   [Release]   │
└──────────────────────┴──────────────────────────────────────────────────────────────────────┘
```

* **Connection manager** (left): sorted list, filter, per-row state as emblem *and* text,
  context menu (Connect, Connect with JNLP, Edit, Duplicate, Delete), keyboard
  (Enter connects, F2 edits, Del deletes).
* **New / Edit connection** is a modal dialog with grouped form cards (Server,
  Account, collapsed Advanced). Security exceptions sit behind Advanced and show a
  warning when enabled.
* **Console pane** (right) has exactly one body state at a time: first run, selected
  connection, setup progress (three stages), stage-specific failure with Try again,
  or the live console.
* **Settings** use the standard Kirigami Addons configuration window: General,
  AI agents (MCP), About.

## Rules

* One primary action per state (Connect, Try again, New connection…).
* Destructive or interrupting actions (power, delete) always confirm, naming the
  server and the consequence.
* State is never colour alone: emblem + text in the list, text in the status bar.
* The remote screen is the content. Chrome stays neutral; the only accent is the
  focus frame that shows keyboard capture.
* While the console is captured, every key goes to the server; app shortcuts do not
  fire. Release: tap Right Ctrl (configurable) or the Release button.
* Secrets never appear in the UI, logs or profile files.

## Verification

`oldipmi --self-test --screenshots DIR` renders every state above offscreen (no BMC
contact) for review. The `slop` source scanner does not parse QML; review uses the
screenshots against `slop rubric` and its P0/P1 checklists.
