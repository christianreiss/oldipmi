# oldipmi.uggs.io

The project website: static HTML and one stylesheet, no scripts, nothing from
other servers. How it looks and why: [DESIGN.md](DESIGN.md).

* `site/`: what the web server serves (`index.html`, `404.html`, `assets/`).
* `tools/build-assets.sh`: regenerates the font subsets, pictures, icons and the
  social preview `og.png` from this repository (Fedora fonts, ImageMagick,
  fontTools).
* `tools/preview.py`: serves `site/` on http://127.0.0.1:8765/ with the headers
  of the real server.

## A new release

In `site/index.html`, replace the version, the release date, the size and the
SHA-256 of the AppImage wherever they appear: search for the old version number
and the old checksum, which the download commands repeat. The new checksum is in
the release's `SHA256SUMS`. For new pictures, run `tools/screenshots.sh` and
then `web/tools/build-assets.sh`.

## The server

Any static web server will do if it

* sends the headers listed in `tools/preview.py` (the Content-Security-Policy
  refuses anything not from the site itself, inline styles included),
* answers unknown paths and directories with `404.html` and status 404, and
  lists no directories,
* redirects HTTP to HTTPS.

To deploy, unpack the committed site into the web root, replacing its content:

```sh
git archive --format=tar.gz -o oldipmi-site.tar.gz HEAD:web/site
```
