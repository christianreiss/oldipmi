#!/bin/sh
# The screenshots of the README and the website, taken by the self-test tour
# (tools/slop/capture.py): its example.net connections and the demo console, in
# the light and the dark colour scheme, 1440x900 at twice the pixel density.
#
# Usage: tools/screenshots.sh [OUT_DIR]   (default: docs/screenshots)
# OLDIPMI_BIN (default build/oldipmi) names the build to photograph.
set -eu
cd "$(dirname "$0")/.."
out=${1:-docs/screenshots}
mkdir -p "$out"
export OLDIPMI_BIN="${OLDIPMI_BIN:-build/oldipmi}" OLDIPMI_CACHE=build/screenshot-cache
for theme in light dark; do
  for state in console selected new-connection error; do
    python3 tools/slop/capture.py "$state" "$theme" 1440 900 2 1 "$out/$state-$theme.png"
  done
done
ls -l "$out"
