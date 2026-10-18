# Third-party code and protocol sources

The native application contains no vendor Java code, JARs or shared libraries.

* `internal/ami/codec`, `internal/ami/vmedia`, `internal/ami/ivtp.go`, and
  `internal/ami/usbhid.go` originate in Anton Musin's MIT-licensed
  [rd450x-console](https://github.com/BadCoder1337/rd450x-console), revision
  `8a24282cf6411d8cbd72138c3cf5c49a367cd05b`.
  Local changes remove trust-all dialing and add bounded, cancellable transport.
  License: `licenses/rd450x-MIT.txt`.
* The ATEN adapter and codec are Go adaptations of MIT-licensed
  [ATENtion](https://github.com/tjone270/ATENtion), revision
  `0ace71a6ae1cb12e8dd1ee802fb751822d650aed`.
  Local changes add strict bounds, explicit transport policy, private IPC and Go
  lifecycle management. License: `licenses/ATENtion-MIT.txt`.
* The older MegaRAC VSC adapter is an independent implementation based on
  protocol facts observed from the owner's BMC and local inspection of its
  publicly served JViewer. Neither vendor binaries nor disassembly are included.
* AMI iUSB protocol facts were cross-checked with
  https://github.com/james-crowley/ansible-collection-asmb8-ikvm . No code from
  that GPL project is incorporated.
* `internal/avocent/apcp` and `internal/avocent/video` derive from MIT-licensed
  [beefkvm](https://github.com/zheng1/beefkvm), revision
  `dd9886392c3fd6e21fd2e06772cc068b38d6ef7c`. Local changes enforce TLS before
  credentials, validate login responses, and correct frame reassembly and pixel
  pattern continuation using observations from the owner's iDRAC.
  License: `licenses/beefkvm-MIT.txt`.
* Go IPMI dependency versions are locked in `go.mod` / `go.sum`. No external
  `ipmitool` process is used.
* Qt 6 and KDE Frameworks 6 are dynamically linked. Bundle license texts are
  under `usr/share/licenses`; the bundle's libraries can be replaced. Obtain
  corresponding distro source packages for bundled Qt/KDE libraries from
  https://src.fedoraproject.org/ . The bundle also uses dynamically linked GNU
  libc and its loader; their license is included. `runtime-packages.txt` identifies
  the precise source RPMs for the bundled libraries.
* AppImage type-2 runtime: https://github.com/AppImage/type2-runtime . Build inputs
  downloaded by `packaging/bundle.py` are checksum-pinned. An upstream replacement
  at the continuous URL fails closed until its checksum is explicitly reviewed.
