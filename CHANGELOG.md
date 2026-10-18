# Changelog

## Profile workflow (2026-10-18)

* Add Lab-AMI, Lab-Avocent and Lab-Cisco profiles once, preserving existing profiles and
  wallet IDs; deleting a default profile no longer causes it to reappear.
* Open a console in one click with KWallet passwords, automatic login/discovery,
  visible progress, setup cancellation and stage-specific failures.
* Display nonsecret console endpoints, transport, protocol, features and resolution.
* Add profile migration and Wallet failure tests, IPC cancellation tests and fresh
  session/metadata tests. Lab-AMI and Lab-Avocent desktop connections and KWallet
  reconnects were verified live; Lab-Cisco still requires a working account.

## 0.1.0 — native rewrite (2026-10-18)

* Replace the container, Python launcher, browser interface and virtual desktop
  with a Go backend and Qt 6/Kirigami desktop frontend.
* Add native legacy MegaRAC VSC and newer ASPEED/IVTP adapters, plus the ATEN
  Hermon/bit-plane adapter, bounded binary IPC and direct framebuffer display.
* Add saved host profiles, optional KWallet passwords, read-only ISO streaming,
  power controls, fresh-JNLP import and explicit per-host legacy transport settings.
* Add a native Avocent/iDRAC adapter with verified Lab-Avocent video; Cisco IMC
  negotiation is present but Lab-Cisco authentication remains pending.
* Add portable AppDir/AppImage packaging, decoder fuzz tests and native GUI checks.
* Hardware acceptance and firmware limitations are recorded in README.md.
