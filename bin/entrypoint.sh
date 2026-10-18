#!/bin/bash
# Bring up the headless X stack, then the two services that put it in a browser:
# kvm-session (owns the JViewer process) and kvm-web (noVNC + the connect page).
set -euo pipefail

DISPLAY_NUM="${DISPLAY#:}"
GEOM="${SCREEN_GEOMETRY:-1280x1024x24}"
WIDTH_HEIGHT="${GEOM%x*}"

cleanup() {
    # kvm-session owns the JViewer process now, and it kills java on SIGTERM. Give it
    # time to finish before X goes away: killing JViewer's TCP sessions is what may let
    # MegaRAC release its iKVM slot, and very few of those slots exist.
    if [[ -n "${SESSION_PID:-}" ]]; then
        kill "$SESSION_PID" 2>/dev/null || true
        for _ in $(seq 1 70); do kill -0 "$SESSION_PID" 2>/dev/null || break; sleep 0.1; done
    fi
    # If kvm-session died badly, java is orphaned and the slot leaks.
    pkill -TERM -f 'com\.ami\.kvm\.jviewer' 2>/dev/null || true
    [[ -n "${WEB_PID:-}"     ]] && kill "$WEB_PID"     2>/dev/null || true
    [[ -n "${X11VNC_PID:-}"  ]] && kill "$X11VNC_PID"  2>/dev/null || true
    [[ -n "${XVFB_PID:-}"    ]] && kill "$XVFB_PID"    2>/dev/null || true
}
trap cleanup EXIT INT TERM

echo "[entrypoint] starting Xvfb on ${DISPLAY} (${GEOM})"
Xvfb "$DISPLAY" -screen 0 "$GEOM" -nolisten tcp -ac +extension RANDR >/tmp/xvfb.log 2>&1 &
XVFB_PID=$!

for _ in $(seq 1 50); do
    xdpyinfo -display "$DISPLAY" >/dev/null 2>&1 && break
    sleep 0.1
done
xdpyinfo -display "$DISPLAY" >/dev/null 2>&1 || { echo "[entrypoint] Xvfb failed:"; cat /tmp/xvfb.log; exit 1; }

# A window manager is not optional here: JViewer opens modal dialogs and a
# resizable main frame, and without a WM they come up unmanaged and unmovable.
fluxbox -display "$DISPLAY" >/tmp/fluxbox.log 2>&1 &
sleep 0.5

VNC_AUTH=(-nopw)
if [[ -n "${VNC_PASSWORD:-}" ]]; then
    x11vnc -storepasswd "$VNC_PASSWORD" /tmp/.vncpass >/dev/null 2>&1
    VNC_AUTH=(-rfbauth /tmp/.vncpass)
    echo "[entrypoint] VNC password auth enabled"
else
    echo "[entrypoint] WARNING: no VNC_PASSWORD set - console is unauthenticated"
fi

echo "[entrypoint] starting x11vnc on 127.0.0.1:${VNC_PORT}"
# -xkb is what keeps modifier keys sane across the browser -> X -> Java -> BMC chain.
x11vnc -display "$DISPLAY" -rfbport "$VNC_PORT" -localhost -xkb -noxrecord -noxfixes \
       -forever -shared -repeat -cursor arrow "${VNC_AUTH[@]}" >/tmp/x11vnc.log 2>&1 &
X11VNC_PID=$!
sleep 1

# Check this before kvm-session sees "$@" -- it forwards unrecognised arguments to
# jviewer-launch, and "shell" is not one of them.
if [[ "${1:-}" == "shell" ]]; then
    echo "[entrypoint] starting kvm-session and kvm-web, then dropping to a shell"
    kvm-session --no-autoconnect >/tmp/kvm-session.log 2>&1 &
    SESSION_PID=$!
    kvm-web >/tmp/kvm-web.log 2>&1 &
    WEB_PID=$!
    echo "[entrypoint] shell mode - X stack is up, launch jviewer-launch yourself"
    exec bash
fi

echo "[entrypoint] starting kvm-session on 127.0.0.1:${SESSION_PORT}"
kvm-session "$@" &
SESSION_PID=$!

echo "[entrypoint] starting kvm-web on 0.0.0.0:${NOVNC_PORT}"
kvm-web &
WEB_PID=$!

echo "[entrypoint] connect page at http://localhost:${NOVNC_PORT}/"
echo "[entrypoint] console at http://localhost:${NOVNC_PORT}/vnc.html?autoconnect=1&resize=scale"

# The container's life is the web service's, not JViewer's: it has to outlive a
# disconnect so you can reconnect from the page.
wait "$WEB_PID"
