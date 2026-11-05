# OldIPMI — instructions for AI agents

OldIPMI is a desktop app that shows the remote console (KVM) of old BMCs
(AMI, ATEN, Avocent/iDRAC, Cisco). This MCP server controls the **running
OldIPMI window**: whatever you do is visible to the user there. OldIPMI must be
started first. There is no authentication; it is local-only. Passwords stay in
KWallet and are never visible to you.

## Register this server

    claude mcp add oldipmi -- {{MCP_PATH}}

Other MCP clients (stdio transport):

    {"mcpServers": {"oldipmi": {"command": "{{MCP_PATH}}"}}}

## Workflow

1. `list_profiles` — saved BMC profiles (id, name, host, family). No secrets.
2. `connect` with a profile name or id. It waits until the console is
   connected or fails with the failing stage. If no saved password exists, the
   user must enter it once in the OldIPMI window.
3. `screenshot` — PNG of the remote screen. All coordinates are pixels of
   that screenshot (framebuffer), origin top-left.
4. Act: `type_text`, `key`, `key_combo`, `mouse_click`, ... then
   `wait_for_stable` and `screenshot` again to verify. Never assume an input worked.
5. `disconnect` when done.

## Tools

- Read-only: `list_profiles`, `status`, `screenshot` (optional region/scale),
  `wait_for_change`, `wait_for_stable`.
- Session: `connect`, `disconnect`.
- Keyboard: `type_text` (US layout, ASCII, newline = Enter), `key` (e.g. `Enter`,
  `F2`, `Esc`, `Down`), `key_combo` (e.g. `["ctrl","alt","delete"]`), `release_input`.
- Mouse: `mouse_move`, `mouse_click`, `mouse_double_click`, `mouse_drag`, `mouse_scroll`.
- Power: `power` with `on`, `off`, `reset`, `soft`. Takes effect **immediately** and
  may interrupt the host's operating system. Use `soft` instead of `off` when possible.
- Virtual media: `mount_iso` (absolute path, mounted read-only), `eject_iso`.
  Not available for Avocent/Cisco profiles.

## Notes

- The BMC's console is slow and lossy: after input wait 300-1000 ms
  (`wait_for_stable`) before judging the screen.
- Some BIOS screens ignore fast typing; use `delay_ms` (e.g. 40) in `type_text`.
- Mouse pointer mode is absolute; relative-mode guests are not supported.
- Only one console session exists at a time; `connect` while connected is an error.
