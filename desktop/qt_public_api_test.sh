#!/bin/sh
# Fails when the program calls Qt's private API. Qt gives its private symbols
# the version Qt_6.<minor>_PRIVATE_API, so a program that uses one runs only
# with the Qt version it was built with: after the next Qt update it no longer
# starts, and its RPM package would hold that update back. qmlcachegen's C++
# output calls such symbols; CMakeLists.txt has it compile to bytecode only.
# Usage: qt_public_api_test.sh NM PROGRAM
set -eu
symbols=$("$1" -D --undefined-only "$2")
if ! printf '%s\n' "$symbols" | grep -q '@Qt_6'; then
  echo "qt_public_api: $1 shows no Qt symbol versions for $2" >&2
  exit 1
fi
private=$(printf '%s\n' "$symbols" | grep PRIVATE_API || true)
if [ -n "$private" ]; then
  printf 'qt_public_api: %s calls private Qt API:\n%s\n' "$2" "$private" >&2
  exit 1
fi
echo "qt_public_api: $2 calls only public Qt API"
