#!/usr/bin/env python3
"""slop native capture adapter for OldIPMI (see tools/slop/native.json).

Usage: slop native --config tools/slop/native.json --root . --out .slop/native

Runs the self-test screenshot tour once per theme/size/dpr/text-scale variant
(cached, serialized with flock) and copies the requested state to {output}; the
tour's states.json says which picture shows which state. Every run gets a private
home (tools/isolated.sh) with its own test-mode kdeglobals for the colour scheme,
font size and icons, with animations off: your settings and ~/.qttest stay as they
are. The cache follows the oldipmi and oldipmi-core binaries and this script. A
variant whose tour failed is not run again for the same binaries: its error is
repeated (delete its directory in the cache to retry).
"""
import fcntl, hashlib, json, os, shutil, signal, subprocess, sys
from pathlib import Path

state, theme, w, h, dpr, ts, out = sys.argv[1:8]
binary = Path(os.environ["OLDIPMI_BIN"]).resolve()
core = Path(os.environ.get("OLDIPMI_BIN_DIR", binary.parent)).resolve() / "oldipmi-core"
isolated = Path(__file__).resolve().parent.parent / "isolated.sh"
root = Path(os.environ["OLDIPMI_CACHE"]).resolve()
schemes = {"light": "BreezeLight", "dark": "BreezeDark"}


def digest(*paths):
    sha = hashlib.sha256()
    for path in paths:
        with open(path, "rb") as f:
            for block in iter(lambda: f.read(1 << 20), b""):
                sha.update(block)
    return sha.hexdigest()[:16]


cache = root / f"{digest(binary, core, __file__)}-{theme}-{w}x{h}@{dpr}-t{ts}"
root.mkdir(parents=True, exist_ok=True)
with open(root / ".lock", "w") as lock:
    fcntl.flock(lock, fcntl.LOCK_EX)
    states, failed = cache / "states.json", cache / "failed.txt"
    if not states.exists() and not failed.exists():
        shutil.rmtree(cache, ignore_errors=True)
        cache.mkdir(parents=True)
        colors = Path(f"/usr/share/color-schemes/{schemes[theme]}.colors").read_text()
        size = round(10 * float(ts), 1)
        kdeglobals = cache / "kdeglobals"
        kdeglobals.write_text(colors + f"\n[General]\nColorScheme={schemes[theme]}\n"
                              f"font=Noto Sans,{size:g},-1,5,400,0,0,0,0,0,0,0,0,0,0,1\n"
                              f"smallestReadableFont=Noto Sans,{round(8 * float(ts), 1):g},-1,5,400,0,0,0,0,0,0,0,0,0,0,1\n"
                              "[Icons]\nTheme=breeze" + ("-dark" if theme == "dark" else "") + "\n"
                              # Transitions take no time, so no picture catches one halfway.
                              "[KDE]\nAnimationDurationFactor=0\n")
        # The offscreen screen is as large as the window (800x800 by default), so
        # the full-screen state is captured at the same size as every other state.
        screen = cache / "offscreen.json"
        screen.write_text(json.dumps({"screens": [{
            "name": "OffscreenScreen", "x": 0, "y": 0, "logicalDpi": 96, "logicalBaseDpi": 96,
            "width": round(int(w) * float(dpr)), "height": round(int(h) * float(dpr))}]}))
        env = dict(os.environ, QT_QPA_PLATFORM=f"offscreen:configfile={screen}", QT_QPA_PLATFORMTHEME="kde",
                   QT_QUICK_CONTROLS_STYLE="org.kde.desktop", QT_SCALE_FACTOR=dpr, KDEGLOBALS=str(kdeglobals))
        # In Qt's test mode the KDE platform theme reads ~/.qttest/config/kdeglobals.
        install = 'mkdir -p "$HOME/.qttest/config" && cp "$KDEGLOBALS" "$HOME/.qttest/config/" && exec "$@"'
        # Its own process group: a tour that hangs is ended with everything it
        # started, and isolated.sh still removes its private home.
        tour = subprocess.Popen([str(isolated), "sh", "-c", install, "sh", str(binary), "--self-test",
                                 "--screenshots", str(cache), "--size", f"{w}x{h}"],
                                env=env, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE,
                                start_new_session=True)
        try:
            _, err = tour.communicate(timeout=170)
            if tour.returncode != 0:
                failed.write_text(f"the tour exited with {tour.returncode}:\n" + err.decode(errors="replace")[-2000:])
        except subprocess.TimeoutExpired:
            os.killpg(tour.pid, signal.SIGTERM)
            tour.communicate()
            failed.write_text("the tour did not finish within 170 s\n")
    if failed.exists():
        sys.exit(failed.read_text())
    files = json.loads(states.read_text())
    if state not in files:
        sys.exit(f"the tour has no state {state!r} (it has: {', '.join(sorted(files))})")
    shutil.copyfile(cache / files[state], out)
