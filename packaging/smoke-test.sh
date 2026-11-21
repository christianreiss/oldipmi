#!/bin/sh
# Runs the AppImage's self-test on this host and on other distributions, in
# Podman containers without Qt, KDE or a desktop of their own: the bundle brings
# its libraries, its C library and its loader.
#
# Usage: packaging/smoke-test.sh [APPIMAGE [IMAGE...]]
#   APPIMAGE  default: dist/OldIPMI-<VERSION>-x86_64.AppImage
#   IMAGE     container images to run it in, default Ubuntu 22.04 and 24.04 and
#             Debian 12 and 13; "-" for none, only this host.
set -eu
cd "$(dirname "$0")/.."
appimage=$(realpath "${1:-dist/OldIPMI-$(cat VERSION)-x86_64.AppImage}")
if [ $# -gt 0 ]; then shift; fi
[ $# -gt 0 ] || set -- docker.io/library/ubuntu:22.04 docker.io/library/ubuntu:24.04 \
  docker.io/library/debian:12 docker.io/library/debian:13
# Containers have no FUSE: the runtime unpacks the image and runs it from there.
export APPIMAGE_EXTRACT_AND_RUN=1 QT_QPA_PLATFORM=offscreen
echo "== $(. /etc/os-release && echo "$PRETTY_NAME"), this host"
tools/isolated.sh "$appimage" --self-test
for image; do
  [ "$image" != - ] || continue
  echo "== $image"
  podman run --rm --security-opt label=disable -v "$appimage:/OldIPMI.AppImage:ro" \
    -e APPIMAGE_EXTRACT_AND_RUN -e QT_QPA_PLATFORM "$image" /OldIPMI.AppImage --self-test
done
echo "== the self-test passed everywhere"
