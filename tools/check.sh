#!/bin/sh
# The project's checks, the same for development and the RPM's %check.
#
# Usage: tools/check.sh [--package] [BUILD_DIR]
#   BUILD_DIR  the CMake build directory to test (default: build). It is built
#              first, and configured if it does not exist yet.
#   --package  only what an offline package build can run, on its finished
#              build: no build step, no go mod tidy, no fuzzing, no sanitizers.
# FUZZTIME (default 10s) is the time per fuzz target, FUZZPARALLEL (default 8)
# its number of workers and FUZZMEM (default 4G) their memory limit together.
# The sanitizer build goes to BUILD_DIR-asan.
set -eu
cd "$(dirname "$0")/.."
package=false
build=build
for arg; do
  case $arg in
  --package) package=true ;;
  -*) echo "check.sh: unknown option $arg" >&2; exit 2 ;;
  *) build=$arg ;;
  esac
done
step() { printf '\n== %s\n' "$*"; }
# The desktop tests never open a window on the screen.
export QT_QPA_PLATFORM=offscreen

step gofmt
unformatted=$(gofmt -l cmd internal)
if [ -n "$unformatted" ]; then
  printf 'not formatted with gofmt:\n%s\n' "$unformatted" >&2
  exit 1
fi
step go vet
go vet ./...
if ! $package; then
  step go mod tidy
  go mod tidy -diff
fi
step go test -race
go test -race ./...

if ! $package; then
  # Briefly, each target in its own run, and with a memory limit where systemd
  # can set one: a fuzzer can find inputs that make a decoder allocate without
  # bound. The limit ends only the worker that exceeds it; Go does not record
  # that input, but the run fails and says so.
  limit=
  if systemd-run --user --scope -q true 2>/dev/null; then
    limit="systemd-run --user --scope -q -p MemoryMax=${FUZZMEM:-4G} -p OOMPolicy=continue --"
  else
    echo "check.sh: no memory limit for fuzzing (systemd-run --user is not available)" >&2
  fi
  for file in $(grep -rl --include='*_test.go' '^func Fuzz' cmd internal | sort); do
    for target in $(sed -n 's/^func \(Fuzz[A-Za-z0-9_]*\)(.*/\1/p' "$file"); do
      step "fuzz $target (./$(dirname "$file"), ${FUZZTIME:-10s})"
      $limit go test "./$(dirname "$file")" -run '^$' -fuzz "^$target\$" -fuzztime "${FUZZTIME:-10s}" \
        -parallel "${FUZZPARALLEL:-8}"
    done
  done
  step "build $build"
  [ -f "$build/CMakeCache.txt" ] || cmake -S . -B "$build"
  cmake --build "$build" -j"$(nproc)"
fi
# Every test runs with its own private home and runtime directory (tools/isolated.sh).
step "ctest $build"
ctest --test-dir "$build" --output-on-failure

if ! $package; then
  asan=$build-asan
  step "build $asan (AddressSanitizer, UndefinedBehaviorSanitizer)"
  [ -f "$asan/CMakeCache.txt" ] || cmake -S . -B "$asan" -DCMAKE_BUILD_TYPE=Debug \
    -DCMAKE_CXX_FLAGS="-fsanitize=address,undefined -fno-sanitize-recover=undefined -fno-omit-frame-pointer" \
    -DCMAKE_EXE_LINKER_FLAGS="-fsanitize=address,undefined"
  cmake --build "$asan" -j"$(nproc)"
  step "ctest $asan"
  # Leaks are not checked: Qt keeps objects alive until the process ends.
  ASAN_OPTIONS=detect_leaks=0:abort_on_error=1 UBSAN_OPTIONS=print_stacktrace=1 \
    ctest --test-dir "$asan" --output-on-failure
fi

step "Go licenses"
# Every module linked into the Go programs ships its license (licenses/go),
# and so does the standard library they contain.
modules=$(go version -m "$build/oldipmi-core" "$build/oldipmi-mcp" | awk '$1 == "dep" { print $2 }' | sort -u)
if [ -z "$modules" ]; then
  echo "check.sh: no Go modules found in $build/oldipmi-core and $build/oldipmi-mcp" >&2
  exit 1
fi
missing=
for module in std $modules; do
  ls "licenses/go/$module" 2>/dev/null | grep -qi '^licen[cs]e' || missing="$missing $module"
done
if [ -n "$missing" ]; then
  echo "check.sh: no license in licenses/go for:$missing" >&2
  exit 1
fi
step desktop-file-validate
desktop-file-validate packaging/*.desktop
step "all checks passed"
