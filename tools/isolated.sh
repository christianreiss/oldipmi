#!/bin/sh
# Runs a command with a fresh, private home and runtime directory, removed again
# afterwards: tests never see the user's settings, wallet, ~/.qttest, agent socket
# or single-instance lock, and two test runs never share state.
# Usage: tools/isolated.sh COMMAND [ARGS...]
set -eu
dir=$(mktemp -d "${TMPDIR:-/tmp}/oldipmi-test.XXXXXX")
trap 'rm -rf "$dir"' EXIT INT TERM
mkdir -m 700 "$dir/home" "$dir/runtime"
HOME=$dir/home XDG_RUNTIME_DIR=$dir/runtime XDG_CONFIG_HOME=$dir/home/.config \
  XDG_CACHE_HOME=$dir/home/.cache XDG_DATA_HOME=$dir/home/.local/share \
  XDG_STATE_HOME=$dir/home/.local/state "$@"
