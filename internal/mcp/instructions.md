# OldIPMI — instructions for AI agents

OldIPMI is a desktop app that shows the remote console (KVM) of old BMCs
(AMI, ATEN, Avocent/iDRAC, Cisco). This MCP server controls the **running
OldIPMI window**: whatever you do is visible to the user there, and the app
lists your actions. OldIPMI must be running with agent control enabled. There
is no authentication; it is local-only. Passwords stay in KWallet and are never
visible to you.

## Register this server

    claude mcp add oldipmi -- {{MCP_PATH}}

Other MCP clients (stdio transport):

    {"mcpServers": {"oldipmi": {"command": "{{MCP_PATH}}"}}}

## Workflow

1. `list_profiles` — saved BMC profiles (id, name, host, family). No secrets.
2. `connect` with a profile name, id or host. It blocks until the console is
   connected and the first frame arrived, or fails naming the failing stage.
   If no password is saved, OldIPMI asks the **user** for it in its window and
   `connect` keeps waiting (`password_wait_ms`, default 60 s): tell the user to
   type it there. Calling `connect` again for a profile that is connecting or
   connected just waits; for a dismissed password prompt it asks the user again.
3. `screenshot` — PNG image of the remote screen. All other tools take
   **framebuffer** pixel coordinates, origin top-left. Large screens are
   downscaled to `max_size` (default 1568 px); the text block then gives the
   exact formula (framebuffer_x = x + image_x / scale).
4. Act: `type_text`, `key`, `mouse_click`, ... then `wait_for_stable` and
   `screenshot` again to verify. Never assume an input worked.
5. `disconnect` when done.

## Tools

- Read-only: `list_profiles`, `status`, `screenshot` (optional region, `scale`,
  `max_size`), `wait_for_change`, `wait_for_stable`, `wait`, `list_keys`.
- Session: `connect`, `disconnect`.
- Keyboard (US layout):
  - `type_text`: every printable ASCII character, Shift added as needed;
    newline = Enter, tab = Tab. The remote keymap must be US for symbols to
    match; remote CapsLock inverts letter case.
  - `key`: one key or a `+` combination, optional `repeat`/`hold_ms`, e.g.
    `Enter`, `Esc`, `F2`, `F12`, `Down`, `PageDown`, `Home`, `Insert`,
    `Delete`, `Backspace`, `Tab`, `KP_Enter`, `ctrl+alt+delete`,
    `ctrl+alt+f1`, `alt+sysrq+b`, `shift+tab`.
  - `key_combo`: the same as a list, e.g. `["ctrl","alt","delete"]`.
  - `key_down` / `key_up`: hold keys across calls (e.g. Shift while
    clicking). `release_input` lets go of everything.
  - `list_keys`: all names. Covered: letters, digits, F1–F24, arrows,
    Home/End/PageUp/PageDown, Insert/Delete, Esc, Tab, Backspace, Enter, Space,
    PrintScreen (= SysRq), Pause (= Break), CapsLock/NumLock/ScrollLock, Menu,
    keypad (KP_0–KP_9, KP_Enter, KP_Add, ...), left/right Ctrl, Shift, Alt
    (AltGr), Super (Win/Cmd), punctuation.
- Mouse (absolute pointer): `mouse_move`, `mouse_click` (left/right/middle,
  optional held `modifiers`), `mouse_double_click`, `mouse_drag`,
  `mouse_scroll` (positive = up).
- Power: `power` with `on`, `off`, `reset`, `soft`. Takes effect
  **immediately** without confirmation and may destroy unsaved work on the
  host. Only with the user's consent; prefer `soft` over `off`.
- Virtual media: `mount_iso` (absolute path on this computer, mounted
  read-only), `eject_iso`. Not available for Avocent/Cisco profiles or ATEN
  HTML5 consoles.

## Notes

- The BMC console is slow and lossy: after input wait 300–1000 ms
  (`wait_for_stable`) before judging the screen.
- Some BIOS screens drop fast typing; use `delay_ms` (e.g. 40) in `type_text`.
- Relative-pointer guests are not supported; the pointer is absolute.
- Only one console session exists at a time; `connect` to another profile
  while connected fails until you `disconnect`.
- The user may type in the same window at the same time; check the screen.
- Errors come back as tool results with a reason; read them and adapt.
- A profile with family `demo` opens a synthetic test console (no BMC) that
  echoes keys, pointer and clicks on screen; use it to practise safely.
