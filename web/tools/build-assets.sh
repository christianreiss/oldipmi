#!/bin/sh
# Builds the website's generated assets from the sources in this repository
# and the fonts installed on a Fedora machine: font subsets (WOFF2), the
# screenshots and family pictures as WebP, the icons and the social preview.
#
# Usage: web/tools/build-assets.sh
# Needs ImageMagick 7 (magick, with WebP), fontTools with brotli (pyftsubset)
# and the packages redhat-display-fonts, redhat-text-fonts and
# adobe-source-code-pro-fonts. Run tools/screenshots.sh first for new pictures.
set -eu
cd "$(dirname "$0")/../.."
site=web/site
assets=$site/assets
mkdir -p "$assets/fonts" "$assets/img"

# Latin text, typographic punctuation and the few symbols the pages use.
unicodes='U+0020-007E,U+00A0-00FF,U+2010-2027,U+2030-203A,U+20AC,U+2190-2193,U+2212,U+2318'
font() { # SOURCE TARGET
  pyftsubset "$1" --unicodes="$unicodes" --layout-features='kern,liga,calt,tnum' \
    --flavor=woff2 --output-file="$assets/fonts/$2.woff2"
}
font /usr/share/fonts/redhat/RedHatDisplay-Bold.otf display-700
font /usr/share/fonts/redhat/RedHatDisplay-Black.otf display-900
font /usr/share/fonts/redhat/RedHatText-Regular.otf text-400
font /usr/share/fonts/redhat/RedHatText-Medium.otf text-500
font /usr/share/fonts/redhat/RedHatText-Bold.otf text-700
font /usr/share/fonts/adobe-source-code-pro-fonts/SourceCodePro-Regular.otf mono-400
font /usr/share/fonts/adobe-source-code-pro-fonts/SourceCodePro-Semibold.otf mono-600
{
  echo "The fonts of this website, subset to the characters it uses, are under the"
  echo "SIL Open Font License 1.1:"
  echo
  echo "* display-*.woff2: Red Hat Display, text-*.woff2: Red Hat Text (Red Hat, Inc.)"
  echo "* mono-*.woff2: Source Code Pro (Adobe)"
  for license in redhat-display-fonts/LICENSE redhat-text-fonts/LICENSE adobe-source-code-pro-fonts/LICENSE.md; do
    printf '\n\n==== %s ====\n\n' "${license%%/*}"
    cat "/usr/share/licenses/$license"
  done
} > "$assets/fonts/LICENSES.txt"

# Screenshots (tools/screenshots.sh, 2880x1800) in two widths, without metadata.
for png in docs/screenshots/*.png; do
  name=$(basename "$png" .png)
  for width in 1280 2560; do
    magick "$png" -strip -resize "${width}x" -quality 82 -define webp:method=6 \
      "$assets/img/$name-$width.webp"
  done
done
# The pictures of the four BMC families, shown at 132 px.
for family in aten ami avocent cisco; do
  magick "desktop/images/family-$family.png" -strip -resize 320x320 -quality 85 \
    -define webp:method=6 "$assets/img/family-$family.webp"
done

# Icons from the app icon.
cp packaging/oldipmi.svg "$assets/logo.svg"
cp packaging/oldipmi.svg "$site/favicon.svg"
magick -background none -density 384 packaging/oldipmi.svg -resize 180x180 -strip \
  "$site/apple-touch-icon.png"

# Social preview, 1200x630: the console screenshot beside the name.
title=/usr/share/fonts/redhat/RedHatDisplay-Black.otf
text=/usr/share/fonts/redhat/RedHatText-Medium.otf
mono=/usr/share/fonts/adobe-source-code-pro-fonts/SourceCodePro-Semibold.otf
magick -size 1200x630 gradient:'#1c2234-#0d1018' \
  \( -background none -density 384 packaging/oldipmi.svg -resize 112x112 \) \
  -geometry +72+92 -composite -density 72 \
  -font "$title" -fill '#ffffff' -pointsize 84 -annotate +72+300 'OldIPMI' \
  -font "$text" -fill '#c8cde0' -pointsize 34 -annotate +72+364 'The console of older servers,' \
  -annotate +72+410 'native on the Linux desktop.' \
  -font "$mono" -fill '#1fc6f8' -pointsize 26 -annotate +72+520 '> no java · no browser · no vnc_' \
  \( docs/screenshots/console-dark.png -resize 620x -bordercolor '#2a3148' -border 2 \) \
  -geometry +640+150 -composite -strip "$assets/img/og.png"

ls -l "$assets/fonts" "$assets/img" | awk 'NF > 4 { print $5, $NF }'
