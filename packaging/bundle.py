#!/usr/bin/env python3
"""Build an AppDir and AppImage from the native build, without container tools.
Runtime downloads are pinned by SHA-256. A private libc avoids host ABI coupling.
Every bundled library, plugin and QML module must come from an installed RPM
whose license texts are installed too; they go into the bundle, and without
them no AppImage is produced. Timestamps in the image are SOURCE_DATE_EPOCH, or
the time of the last commit.
"""
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
BUILD = ROOT / 'build'
DIST = ROOT / 'dist'
APP = DIST / 'OldIPMI.AppDir'
CACHE = BUILD / 'bundle-cache'
VERSION = (ROOT / 'VERSION').read_text().strip()
RUNTIME_URL = 'https://github.com/AppImage/type2-runtime/releases/download/continuous/runtime-x86_64'
RUNTIME_SHA = '156f4bdbde9c52d01814600013e0a273f0118dc2de98975f3c8c63427ec79074'

def run(*args):
    return subprocess.check_output(args, text=True)

def fetch(url, sha, name):
    """The download, from the cache if it matches; cached only once it matched."""
    p = CACHE / name
    if p.exists() and hashlib.sha256(p.read_bytes()).hexdigest() == sha:
        return p
    with urllib.request.urlopen(url, timeout=60) as response:
        data = response.read()
    if hashlib.sha256(data).hexdigest() != sha:
        raise SystemExit(f'Checksum mismatch: {name}; no artifact produced')
    p.write_bytes(data)
    return p

def copy(src, dst):
    dst.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(src, dst, follow_symlinks=True)

def packages_of(paths):
    """The installed RPMs that own these files. One that no package owns stops
    the build: its license would be unknown."""
    names = set()
    paths = sorted({str(Path(p).resolve()) for p in paths})
    for i in range(0, len(paths), 200):
        r = subprocess.run(['rpm', '-qf', '--qf', '%{NAME}\n', *paths[i:i + 200]], capture_output=True, text=True)
        if r.returncode != 0:
            raise SystemExit('Not from an installed package, so its license is unknown:\n' +
                             ''.join(l + '\n' for l in r.stdout.splitlines() if 'not owned' in l) + r.stderr)
        names.update(r.stdout.split())
    return names

def license_texts(packages):
    """The license texts of each package: its %license files, else the license
    files among its documentation. A package without either has those of a
    package built from the same source RPM, as many subpackages do, else those
    that packaging/licenses keeps for its source package; one without any stops
    the build."""
    siblings = {}
    for line in run('rpm', '-qa', '--qf', '%{SOURCERPM}\t%{NAME}\n').splitlines():
        source, name = line.split('\t')
        siblings.setdefault(source, set()).add(name)
    def files(query, package):
        return [Path(f) for f in run('rpm', query, package).splitlines() if f.startswith('/') and Path(f).is_file()]
    texts, missing = {}, []
    for package in sorted(packages):
        # One line per installed architecture; they come from the same source.
        source = run('rpm', '-q', '--qf', '%{SOURCERPM}\n', package).split()[0]
        for candidate in [package, *sorted(siblings.get(source, set()) - {package})]:
            found = files('-qL', candidate) or [f for f in files('-qd', candidate)
                                                if re.match(r'(copying|licen[cs]e|copyright)', f.name, re.I)]
            if found:
                texts[package] = sorted(set(found))
                break
        else:
            kept = ROOT / 'packaging/licenses' / source.removesuffix('.src.rpm').rsplit('-', 2)[0]
            if kept.is_dir():
                texts[package] = sorted(kept.iterdir())
            else:
                missing.append(package)
    if missing:
        raise SystemExit('No license text installed for: ' + ', '.join(missing) + '; no artifact produced')
    return texts

DIST.mkdir(exist_ok=True)
CACHE.mkdir(parents=True, exist_ok=True)
if APP.exists():
    shutil.rmtree(APP)
subprocess.run(['cmake', '--install', str(BUILD), '--prefix', str(APP / 'usr')], check=True)
qml = Path(run('qmake6', '-query', 'QT_INSTALL_QML').strip())
plugins = Path(run('qmake6', '-query', 'QT_INSTALL_PLUGINS').strip())
sources = []  # what comes from the system, for its packages and their licenses
# Kirigami Addons (form cards, settings window) and the modules it imports,
# without the 3D, PDF, virtual keyboard and Studio modules of the build host.
for name in ('QtQuick', 'QtQml', 'org/kde/kirigami', 'org/kde/kirigamiaddons', 'org/kde/coreaddons',
             'org/kde/kitemmodels', 'org/kde/config', 'org/kde/sonnet'):
    src = qml / name
    if not src.exists(): continue
    skip = lambda d, names, top=src, module=name: [n for n in names if module == 'QtQuick' and Path(d) == top and
                                                   n in ('Scene2D', 'Scene3D', 'VirtualKeyboard', 'Pdf', 'Studio')]
    shutil.copytree(src, APP / 'usr/qml' / name, symlinks=False, ignore=skip)
    sources += [src / p.relative_to(APP / 'usr/qml' / name) for p in (APP / 'usr/qml' / name).rglob('*') if p.is_file()]
# Only the plugins the application uses (None: the whole group). Its pictures
# are PNG, which Qt reads itself, and SVG; it makes no TLS connection itself.
PLUGINS = {
    'platforms': ('libqxcb.so', 'libqwayland.so', 'libqoffscreen.so', 'libqminimal.so'),
    'imageformats': ('libqsvg.so', 'libqjpeg.so'),
    'iconengines': ('libqsvgicon.so',),  # SVG theme icons
    'styles': ('breeze6.so',),  # the widget style of the file dialog on Plasma
    'wayland-shell-integration': None,
    'wayland-graphics-integration-client': None,
    'wayland-decoration-client': None,  # title bars where the compositor draws none, as on GNOME
    'xcbglintegrations': None,
}
for group, names in PLUGINS.items():
    for p in sorted((plugins / group).glob('*.so')):
        if names is None or p.name in names:
            copy(p, APP / 'usr/plugins' / group / p.name)
            sources.append(p)

lib = APP / 'usr/lib'
lib.mkdir(parents=True, exist_ok=True)
queue = [p for p in APP.rglob('*') if p.is_file() and p.read_bytes()[:4] == b'\x7fELF']
seen = set()
while queue:
    binary = queue.pop()
    if str(binary) in seen: continue
    seen.add(str(binary))
    try: output = run('ldd', str(binary))
    except subprocess.CalledProcessError: continue  # static Go executable
    dependencies = re.findall(r'\s+(\S+) => (/\S+)', output)
    if re.search(r'=> not found', output): raise SystemExit(f'Missing runtime dependency for {binary}')
    for name, path in dependencies:
        dest = lib / name
        if dest.exists(): continue
        source = Path(path)
        copy(source, dest)
        queue.append(dest)
        sources.append(source)

copy(Path('/lib64/ld-linux-x86-64.so.2'), lib / 'ld-linux-x86-64.so.2')
sources.append(Path('/lib64/ld-linux-x86-64.so.2'))
font = Path('/usr/share/fonts/dejavu-sans-fonts/DejaVuSans.ttf')
sources.append(font)
license_packages = packages_of(sources)

# The license texts of every package something was taken from, plus our provenance.
for package, texts in license_texts(license_packages).items():
    for text in texts:
        licenses = Path('/usr/share/licenses')
        copy(text, APP / 'usr/share/licenses' / (text.relative_to(licenses) if text.is_relative_to(licenses)
                                                 else Path(package) / text.name))
(APP / 'usr/share/doc/OldIPMI').mkdir(parents=True,exist_ok=True)
(APP / 'usr/share/doc/OldIPMI/runtime-packages.txt').write_text(run('rpm','-q','--qf','%{NAME} %{VERSION}-%{RELEASE} %{SOURCERPM}\n',*sorted(license_packages)))
shutil.copytree(ROOT / 'licenses', APP / 'usr/share/licenses/OldIPMI', dirs_exist_ok=True)
copy(ROOT / 'LICENSE', APP / 'usr/share/licenses/OldIPMI/LICENSE')
for name in ('README.md', 'THIRD_PARTY.md', 'CHANGELOG.md'):
    copy(ROOT / name, APP / 'usr/share/doc/OldIPMI' / name)
copy(font, APP / 'usr/share/fonts/DejaVuSans.ttf')
copy(ROOT / 'packaging/io.uggs.OldIPMI.desktop', APP / 'io.uggs.OldIPMI.desktop')
copy(ROOT / 'packaging/oldipmi.svg', APP / 'oldipmi.svg')
copy(ROOT / 'packaging/AppRun', APP / 'AppRun')
(APP / 'AppRun').chmod(0o755)
(APP / 'usr/bin/qt.conf').write_text('[Paths]\nPrefix=..\nLibraries=lib\nPlugins=plugins\nQmlImports=qml\n')
# Make every plugin find the private libraries without relying on global paths.
for p in APP.rglob('*'):
    if p.is_file() and p.read_bytes()[:4] == b'\x7fELF' and p.name not in ('oldipmi-core','oldipmi-mcp','ld-linux-x86-64.so.2','libc.so.6'):
        relative = os.path.relpath(lib, p.parent)
        subprocess.run(['patchelf', '--set-rpath', '$ORIGIN/' + relative, str(p)], check=True)
(APP / 'usr/share/fonts/fonts.conf').write_text('<?xml version="1.0"?><!DOCTYPE fontconfig SYSTEM "urn:fontconfig:fonts.dtd"><fontconfig><dir prefix="relative">.</dir><dir>/usr/share/fonts</dir><cachedir prefix="xdg">fontconfig</cachedir></fontconfig>')
manifest = {str(p.relative_to(APP)): hashlib.sha256(p.read_bytes()).hexdigest() for p in APP.rglob('*') if p.is_file()}
(APP / 'manifest.json').write_text(json.dumps(manifest, indent=2, sort_keys=True)+'\n')
runtime = fetch(RUNTIME_URL, RUNTIME_SHA, 'runtime-x86_64')
squash = CACHE / 'app.squashfs'
if squash.exists(): squash.unlink()
epoch = os.environ.get('SOURCE_DATE_EPOCH') or run('git', '-C', str(ROOT), 'log', '-1', '--format=%ct').strip()
subprocess.run(['mksquashfs', str(APP), str(squash), '-noappend', '-comp', 'gzip', '-all-root', '-no-progress',
                '-mkfs-time', epoch, '-all-time', epoch], check=True)
image = DIST / f'OldIPMI-{VERSION}-x86_64.AppImage'
with image.open('wb') as out:
    out.write(runtime.read_bytes())
    with squash.open('rb') as inp: shutil.copyfileobj(inp,out)
image.chmod(0o755)
(DIST / 'SHA256SUMS').write_text(hashlib.sha256(image.read_bytes()).hexdigest()+'  '+image.name+'\n')
print(image)
