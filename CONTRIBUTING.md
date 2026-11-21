# Contributing

Bug reports, reports about BMCs and patches are welcome.

## Reporting a bug or a BMC that does not work

Please use the issue templates. For a BMC, the most useful facts are the
vendor, the board, the BMC firmware version, the console family you chose, the
stage where the connection fails and the message OldIPMI shows. Never post
passwords, real addresses, serial numbers or packet captures of a real BMC; if a
capture would help, we will find a private way.

## Building and checking

The README explains the build. Before you send a change, run

```sh
tools/check.sh
```

It runs gofmt, go vet, the Go tests with the race detector, short fuzzing, the
desktop tests (each with a private home), a build with AddressSanitizer and
UndefinedBehaviorSanitizer, and the license and desktop file checks.

## Changes

* Keep a change focused: one behaviour change per commit, with an entry in
  `CHANGELOG.md` when users notice it.
* The screenshot tour (`oldipmi --self-test --screenshots DIR`) pins how the
  window looks; say so when a change alters a picture.
* Tests run against the demo console, the stand-in core and recorded protocol
  fixtures, never against a real BMC or with real credentials.
* Contributions are licensed under the GPL-3.0-or-later, like the project.
