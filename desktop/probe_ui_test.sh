#!/bin/sh
# What oldipmi --probe-ui prints and how it ends (C-18): the connection stages
# with their texts and the picture size, then 0; 7 when the connection needs a
# password; 6 while another OldIPMI runs, which it leaves alone. Run through
# tools/isolated.sh: the settings and the single-instance lock are private.
# Usage: probe_ui_test.sh OLDIPMI
set -u
app=$1
out=$XDG_RUNTIME_DIR/probe.out err=$XDG_RUNTIME_DIR/probe.err
fail() {
  echo "probe_ui: $*" >&2
  sed 's/^/  stdout: /' "$out" >&2
  sed 's/^/  stderr: /' "$err" >&2
  exit 1
}
probe() {
  echo "$1" | "$app" --probe-ui --no-agent-api >"$out" 2>"$err"
}

probe '{"profile":{"name":"Demo","host":"demo","family":"demo"}}'
rc=$?
[ "$rc" -eq 0 ] || fail "demo console: exit $rc, want 0"
grep -q '"message":"Connected — click the console to control it."' "$out" || fail "demo console: no stage text"
grep -qx '{"frame":{"height":768,"width":1024}}' "$out" || fail "demo console: no picture size"

probe '{"profile":{"name":"Locked","host":"bmc.invalid","family":"ami","username":"x"}}'
rc=$?
[ "$rc" -eq 7 ] || fail "without a password: exit $rc, want 7"
grep -q 'Enter the password for x@bmc.invalid' "$err" || fail "without a password: no message"

"$app" --preview --no-agent-api >/dev/null 2>&1 &
other=$!
trap 'kill "$other" 2>/dev/null' EXIT
i=0
until [ -S "$XDG_RUNTIME_DIR/oldipmi/instance.sock" ]; do
  i=$((i + 1))
  [ "$i" -lt 200 ] || fail "the other OldIPMI did not start"
  sleep 0.1
done
probe '{"profile":{"name":"Demo","host":"demo","family":"demo"}}'
rc=$?
[ "$rc" -eq 6 ] || fail "with OldIPMI running: exit $rc, want 6"
grep -q 'already running' "$err" || fail "with OldIPMI running: no message"
kill -0 "$other" 2>/dev/null || fail "the other OldIPMI ended"
