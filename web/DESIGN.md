# Design of oldipmi.uggs.io

The website introduces the app to people who run older servers. It follows the
app (see the repository's `DESIGN.md`) and its icon, and it needs nothing from
other servers.

## Constraints

* **Static and self-contained:** HTML, one stylesheet, fonts and pictures from
  this directory. No scripts, no external fonts, CDNs, embeds, cookies or
  trackers. The server's Content-Security-Policy (`default-src 'none'`, `img-src`,
  `style-src` and `font-src` `'self'`) refuses anything else, inline styles
  included.
* **Light and dark** follow `prefers-color-scheme`, and so do the screenshots
  (`<picture>` with a dark source).
* **Readable without motion:** `prefers-reduced-motion` turns every animation
  off, and none runs longer than five seconds.
* **Keyboard:** the focus ring is the accent colour on the page and the console
  cyan on the bezel, at least 3:1 against both.

## The motif: a rack server's front

OldIPMI is the console of a server in a rack, and its icon is a server front
with a glowing console prompt. The page header and footer are that front:

* **The bezel** is the icon's navy (`#1c2234` to `#0d1018`, a vertical gradient
  for the depth of the metal) with a faint cyan glow from the top right, where
  the console light falls.
* **Vent slots** along its top edge are a repeating gradient: the only way to
  draw them in CSS without pictures.
* **Rack ears** left and right are flat, each with two screws.
* **A green status LED** beside the name pulses twice after the page loads and then
  stays lit: the server is on.
* **The console line** below the headline is the icon's cyan prompt in the
  monospaced font, with a block cursor that blinks four times and then stays.
  Both animations end within five seconds (WCAG 2.2.2, there is no script for a
  pause button).

The content between header and footer is plain: the app's Breeze-like light or
dark surfaces, cards with a 12 px radius (the app's grouped panels), one accent.

## Sections as rack units

Each section is labelled like a height unit in a rack (`U01 Features`,
`U02 Supported BMCs` …), in the monospaced font, the number on a filled tag.

## Colour

| Role | Light | Dark |
|---|---|---|
| Page | `#f2f4f8` | `#0f121a` |
| Surface (cards) | `#ffffff` | `#171b27` |
| Text | `#141824` | `#e8ebf4` |
| Secondary text | `#4a546d` | `#a6aec5` |
| Accent (links, card edges) | `#086a96` | `#38c8f6` |
| Console glow (bezel, buttons) | `#1fc6f8` | `#1fc6f8` |
| Verified / limited (chips) | `#1f7a4a` / `#8a5a00` | `#5fd39a` / `#f0bf5a` |

Text colours reach at least 4.5:1 on their backgrounds.

## Type

* **Red Hat Display** (700, 900): headings and the name.
* **Red Hat Text** (400, 500, 700): body text.
* **Source Code Pro** (400, 600): the console line, unit labels, commands,
  checksums.

All three are under the SIL Open Font License; the subsets (Latin, typographic
punctuation, arrows) and their licenses are in `site/assets/fonts`.

## Pictures

* Screenshots: `tools/screenshots.sh` (the self-test tour, example.net names and
  the demo console only), as WebP in 1280 and 2560 px.
* Family pictures: the app's own illustrations (`desktop/images`), no vendor
  logos.
* Social preview `og.png` and the icons: `web/tools/build-assets.sh`.
