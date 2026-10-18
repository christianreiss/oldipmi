# OldIPMI — native Linux BMC console

OldIPMI is a Go protocol engine with a Qt 6/Kirigami desktop interface for KDE
Plasma. Video is decoded directly into the native window. It needs no Java,
Docker, Podman, browser, web server, or VNC bridge.

This is a new implementation, not the old launcher repackaged. The previous
container version is recoverable at Git commit `ce0b1a7`.

## Build and run

On Fedora KDE, install Go, CMake, Ninja, a C++20 compiler, `qt6-qtbase-devel`,
`qt6-qtdeclarative-devel`, `kf6-kirigami-devel`, and `kf6-kwallet-devel`.

```sh
cmake -S . -B build -G Ninja -DCMAKE_BUILD_TYPE=Release
cmake --build build
./build/oldipmi
```

Select the correct BMC family, enter a hostname and credentials, and connect.
Save profiles for reuse; saving a password is optional and uses KWallet.
A fresh JNLP can supply the session token, but is never executed or used to
fetch code. Enter the BMC address and select its family before importing it.
Close/disconnect before selecting another host. Reconnect by clicking Connect;
credential login always obtains a new token. A JNLP must be freshly obtained.

Click the console to capture input. Losing focus releases held keys/buttons.
The toolbar provides fit/actual-size, fullscreen, Ctrl+Alt+Del, ISO mount/eject
and confirmed power operations. Mounted ISO files are opened read-only.

## Transport compatibility

TLS verification is enabled by default. HTTP login, unverified/legacy TLS and
plain console/media transport are separate per-host exceptions, off by default.
The legacy TLS exception also allows obsolete RSA/3DES suites when required.
No BMC TLS configuration, clocks or firmware are changed by OldIPMI.

Older AMI systems may have plain video and an always-TLS HID channel. On the
verified Lab-AMI generation, select AMI and explicitly enable HTTP login,
legacy/unverified TLS and plain console/media. Passwords/tokens never appear in
command-line arguments, profile files or diagnostic output.

For verified Lab-Avocent, select Avocent / Dell iDRAC and enable legacy/unverified TLS.
Avocent ISO media, JNLP import and packet-134 video are not implemented; the ISO
button is disabled for that family. Cisco uses port 2068, Dell uses port 5900.

ATEN systems requiring mutual TLS need their client certificate/key selected in
Compatibility. The application does not distribute vendor private keys. It also
does not change the BMC's Java/HTML5 console preference automatically.

## Compatibility and acceptance

| Family / target | Implementation | Current evidence |
|---|---|---|
| Legacy AMI / Lab-AMI, Supermicro X8DTU | 7-byte framing, TLS HID, RGB555 VSC raw and LRLE16/32 | Real native 640x480 video decoded and visually inspected |
| Newer AMI / ASPEED | 8-byte IVTP, VQ/JPEG decoder, HID, iUSB | Imported regression tests; no hardware acceptance yet |
| ATEN / Supermicro | Token login, Hermon and bit-plane video, HID, power, read-only media | Synthetic TCP handshake/video/cancellation tests; no ATEN target supplied |
| Avocent / Lab-Avocent, Dell PowerEdge R420 | APCP, TLS companion channel, RGB555 stream, HID | Real native 640x480 video decoded and visually inspected |
| Cisco IMC / Lab-Cisco (bonus target) | APCP handshake supported | Account rejected by IMC API (551); KVM channel access denied; video unverified |

This is a development release. Both AMI and ATEN hardware acceptance remain
required for a completed release. Live keyboard injection, ISO mounting and
power/boot operations have not been run on production hardware. AMI single-port
HTTP CONNECT mode, encrypted HID at the application layer, non-RGB555 legacy
VSC modes, unsupported ATEN encodings, and relative pointer mode are not yet
implemented. Unsupported formats fail explicitly rather than launch Java.

## Portable bundle

```sh
python3 packaging/bundle.py
./dist/OldIPMI-x86_64.AppImage
```

Packaging additionally uses `mksquashfs`, `patchelf`, `qmake6` and
Python 3. It bundles the native executables, Qt/KDE QML modules, platform plugins,
fonts and shared-library dependencies, including a private glibc and loader.
GPU drivers remain host-provided. The bundle has passed an offscreen rendering
check in a clean Ubuntu 24.04 userspace; a full Ubuntu desktop session has not
been tested. The private loader avoids Fedora Qt libraries requiring a newer
host glibc. The release checksum is in `dist/SHA256SUMS`.

Without FUSE, use `--appimage-extract-and-run`, or `--appimage-extract` and run
`squashfs-root/AppRun`. `dist/OldIPMI.AppDir/AppRun` is the unpacked equivalent.
KWallet requires the host desktop's wallet service; without it use a session-only
password. Runtime license texts and an inventory of file checksums are included.

## Checks

```sh
go test -race ./...
go vet ./...
ctest --test-dir build --output-on-failure
go test ./internal/ami -run '^$' -fuzz FuzzLegacyDecode -fuzztime 10s
go test ./internal/aten -run '^$' -fuzz FuzzDecode -fuzztime 10s
```

`oldipmi --self-test` opens a synthetic framebuffer and exits after checking
rendering. `--preview` keeps that synthetic preview open. Neither contacts a BMC.
`oldipmi-core --probe` accepts `{ "profile": {...}, "password": "..." }` on
stdin and checks live video without injecting keys or changing power/media.
`--capture PATH` saves the first frame to a new mode-0600 PNG, only when explicitly
requested. Never commit real console captures or credential-bearing JNLP files.

## Architecture

The GUI owns one backend child per connection. Two inherited AF_UNIX socketpairs
carry version-1 control messages and RGBA video frames. There are no listening
ports or installed background services. Control records are length-prefixed JSON
(maximum 1 MiB); frame records carry width, height and bounded raw RGBA bytes.
A one-frame queue drops superseded display frames without delaying input.

Profiles live in the application's XDG config directory. Old `.env` files are not
imported or deleted automatically. To roll back, build/run a separate checkout of
`ce0b1a7`; private `.env` configuration remains outside version control.

See [THIRD_PARTY.md](THIRD_PARTY.md) for provenance and licenses.
