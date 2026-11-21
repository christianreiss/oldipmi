# License texts that the build host does not install

Fedora's packages of these libraries install no license text, so the bundle
(`packaging/bundle.py`) takes theirs from here, by source package:

* `kf6-kcoreaddons`: the `LICENSES` directory of KCoreAddons 6.30.0 (without
  the commercial Qt license reference),
  https://invent.kde.org/frameworks/kcoreaddons/-/tree/v6.30.0/LICENSES
* `libglvnd`: the license section of libglvnd 1.7.0's `README.md`, as Fedora
  installs it in `/usr/share/doc/libglvnd`.
* `xcb-util-keysyms`: `COPYING` of xcb-util-keysyms 0.4.1,
  https://gitlab.freedesktop.org/xorg/lib/libxcb-keysyms/-/blob/xcb-util-keysyms-0.4.1/COPYING

Check them when the build host has a newer version of one of these packages.
