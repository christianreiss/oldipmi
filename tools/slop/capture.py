#!/usr/bin/env python3
"""slop native capture adapter for OldIPMI (see tools/slop/native.json).

Usage: slop native --config tools/slop/native.json --root . --out .slop/native

Runs the self-test screenshot tour once per theme/size/dpr/text-scale variant
(cached, serialized with flock) and copies the requested state to {output}.
Theme and font size go through ~/.qttest/config/kdeglobals, which is what the
KDE platform theme reads in Qt test mode; the previous file is restored.
"""
import fcntl, os, shutil, subprocess, sys
from pathlib import Path

state, theme, w, h, dpr, ts, out = sys.argv[1:8]
binary = Path(os.environ["OLDIPMI_BIN"]).resolve()
# A rebuilt binary gets a fresh cache entry.
cache = Path(os.environ["OLDIPMI_CACHE"]).resolve() / f"{int(binary.stat().st_mtime)}-{theme}-{w}x{h}@{dpr}-t{ts}"
files = {
    "selected": "01-selected", "new-connection": "02-new-connection",
    "edit-connection": "03-edit-connection", "settings-general": "04-settings-general-2",
    "settings-agents": "05-settings-agents-2", "connecting": "06-connecting",
    "error": "07-error", "console": "08-console", "power-confirm": "09-power-confirm",
    "switch": "10-switch", "empty": "11-empty",
}
schemes = {"light": "BreezeLight", "dark": "BreezeDark"}
kdeglobals = Path.home() / ".qttest/config/kdeglobals"

Path(os.environ["OLDIPMI_CACHE"]).mkdir(parents=True, exist_ok=True)
with open(Path(os.environ["OLDIPMI_CACHE"]) / ".lock", "w") as lock:
    fcntl.flock(lock, fcntl.LOCK_EX)
    if not (cache / "11-empty.png").exists():
        shutil.rmtree(cache, ignore_errors=True)
        cache.mkdir(parents=True)
        colors = Path(f"/usr/share/color-schemes/{schemes[theme]}.colors").read_text()
        size = round(10 * float(ts), 1)
        previous = kdeglobals.read_bytes() if kdeglobals.exists() else None
        kdeglobals.parent.mkdir(parents=True, exist_ok=True)
        kdeglobals.write_text(colors + f"\n[General]\nColorScheme={schemes[theme]}\n"
                              f"font=Noto Sans,{size:g},-1,5,400,0,0,0,0,0,0,0,0,0,0,1\n"
                              f"smallestReadableFont=Noto Sans,{round(8 * float(ts), 1):g},-1,5,400,0,0,0,0,0,0,0,0,0,0,1\n"
                              "[Icons]\nTheme=breeze" + ("-dark" if theme == "dark" else "") + "\n")
        env = dict(os.environ, QT_QPA_PLATFORM="offscreen", QT_QPA_PLATFORMTHEME="kde",
                   QT_QUICK_CONTROLS_STYLE="org.kde.desktop", QT_SCALE_FACTOR=dpr)
        try:
            r = subprocess.run([str(binary), "--self-test", "--screenshots", str(cache),
                                "--size", f"{w}x{h}"], env=env, capture_output=True, timeout=120)
        finally:
            if previous is None:
                kdeglobals.unlink(missing_ok=True)
            else:
                kdeglobals.write_bytes(previous)
        if r.returncode != 0:
            sys.stderr.write(r.stderr.decode(errors="replace")[-2000:])
            sys.exit(r.returncode)
    src = cache / (files[state] + ".png")
    if not src.exists():
        sys.exit(f"tour produced no {src.name}")
    shutil.copyfile(src, out)
