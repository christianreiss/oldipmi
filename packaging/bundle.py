#!/usr/bin/env python3
"""Build an AppDir and AppImage from the native build, without container tools.
Runtime downloads are pinned by SHA-256. A private libc avoids host ABI coupling.
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
RUNTIME_URL = 'https://github.com/AppImage/type2-runtime/releases/download/continuous/runtime-x86_64'
RUNTIME_SHA = '156f4bdbde9c52d01814600013e0a273f0118dc2de98975f3c8c63427ec79074'

def run(*args):
    return subprocess.check_output(args, text=True)

def fetch(url, sha, name):
    p = CACHE / name
    if not p.exists():
        with urllib.request.urlopen(url, timeout=60) as response:
            p.write_bytes(response.read())
    if hashlib.sha256(p.read_bytes()).hexdigest() != sha:
        raise SystemExit(f'Checksum mismatch: {name}; no artifact produced')
    return p

def copy(src, dst):
    dst.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(src, dst, follow_symlinks=True)

DIST.mkdir(exist_ok=True)
CACHE.mkdir(parents=True, exist_ok=True)
if APP.exists():
    shutil.rmtree(APP)
subprocess.run(['cmake', '--install', str(BUILD), '--prefix', str(APP / 'usr')], check=True)
qml = Path(run('qmake6', '-query', 'QT_INSTALL_QML').strip())
plugins = Path(run('qmake6', '-query', 'QT_INSTALL_PLUGINS').strip())
for name in ('QtQuick', 'QtQml', 'org/kde/kirigami'):
    shutil.copytree(qml / name, APP / 'usr/qml' / name, symlinks=False)
# Do not ship unrelated network/browser/3D plugins merely present on the build host.
for name in ('Scene2D', 'Scene3D', 'VirtualKeyboard', 'Pdf', 'Studio'):
    p = APP / 'usr/qml/QtQuick' / name
    if p.exists(): shutil.rmtree(p)
for group in ('platforms', 'imageformats', 'wayland-shell-integration', 'wayland-graphics-integration-client', 'xcbglintegrations', 'tls'):
    src = plugins / group
    if not src.exists(): continue
    for p in src.glob('*.so'):
        if group == 'platforms' and p.name not in ('libqxcb.so', 'libqwayland.so', 'libqoffscreen.so', 'libqminimal.so'): continue
        copy(p, APP / 'usr/plugins' / group / p.name)
# Qt Quick Controls uses this plugin on a Plasma session.
style = plugins / 'styles/breeze6.so'
if style.exists(): copy(style, APP / 'usr/plugins/styles/breeze6.so')

lib = APP / 'usr/lib'
lib.mkdir(parents=True, exist_ok=True)
queue = [p for p in APP.rglob('*') if p.is_file() and p.read_bytes()[:4] == b'\x7fELF']
seen = set()
license_packages = set()
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
        try: license_packages.add(run('rpm', '-qf', '--qf', '%{NAME}', str(Path(path).resolve())).strip())
        except subprocess.CalledProcessError: pass

copy(Path('/lib64/ld-linux-x86-64.so.2'), lib / 'ld-linux-x86-64.so.2')
license_packages.add('glibc')

# Copy license texts from installed distro packages, plus our provenance.
for package in sorted(license_packages | {'qt6-qtbase', 'qt6-qtdeclarative', 'kf6-kirigami', 'kf6-kwallet', 'dejavu-sans-fonts'}):
    src = Path('/usr/share/licenses') / package
    if src.is_dir(): shutil.copytree(src, APP / 'usr/share/licenses' / package, dirs_exist_ok=True)
(APP / 'usr/share/doc/OldIPMI').mkdir(parents=True,exist_ok=True)
(APP / 'usr/share/doc/OldIPMI/runtime-packages.txt').write_text(run('rpm','-q','--qf','%{NAME} %{VERSION}-%{RELEASE} %{SOURCERPM}\n',*sorted(license_packages)))
shutil.copytree(ROOT / 'licenses', APP / 'usr/share/licenses/OldIPMI', dirs_exist_ok=True)
for name in ('README.md', 'THIRD_PARTY.md', 'CHANGELOG.md'):
    copy(ROOT / name, APP / 'usr/share/doc/OldIPMI' / name)
copy(Path('/usr/share/fonts/dejavu-sans-fonts/DejaVuSans.ttf'), APP / 'usr/share/fonts/DejaVuSans.ttf')
copy(ROOT / 'packaging/net.alpha_labs.OldIPMI.desktop', APP / 'net.alpha_labs.OldIPMI.desktop')
copy(ROOT / 'packaging/oldipmi.svg', APP / 'oldipmi.svg')
copy(ROOT / 'packaging/AppRun', APP / 'AppRun')
(APP / 'AppRun').chmod(0o755)
(APP / 'usr/bin/qt.conf').write_text('[Paths]\nPrefix=..\nLibraries=lib\nPlugins=plugins\nQmlImports=qml\n')
# Make every plugin find the private libraries without relying on global paths.
for p in APP.rglob('*'):
    if p.is_file() and p.read_bytes()[:4] == b'\x7fELF' and p.name not in ('oldipmi-core','ld-linux-x86-64.so.2','libc.so.6'):
        relative = os.path.relpath(lib, p.parent)
        subprocess.run(['patchelf', '--set-rpath', '$ORIGIN/' + relative, str(p)], check=True)
(APP / 'usr/share/fonts/fonts.conf').write_text('<?xml version="1.0"?><!DOCTYPE fontconfig SYSTEM "urn:fontconfig:fonts.dtd"><fontconfig><dir prefix="relative">.</dir><dir>/usr/share/fonts</dir><cachedir prefix="xdg">fontconfig</cachedir></fontconfig>')
manifest = {str(p.relative_to(APP)): hashlib.sha256(p.read_bytes()).hexdigest() for p in APP.rglob('*') if p.is_file()}
(APP / 'manifest.json').write_text(json.dumps(manifest, indent=2)+'\n')
runtime = fetch(RUNTIME_URL, RUNTIME_SHA, 'runtime-x86_64')
squash = CACHE / 'app.squashfs'
if squash.exists(): squash.unlink()
subprocess.run(['mksquashfs', str(APP), str(squash), '-noappend', '-comp', 'gzip', '-all-root', '-no-progress'], check=True)
image = DIST / 'OldIPMI-x86_64.AppImage'
with image.open('wb') as out:
    out.write(runtime.read_bytes())
    with squash.open('rb') as inp: shutil.copyfileobj(inp,out)
image.chmod(0o755)
(DIST / 'SHA256SUMS').write_text(hashlib.sha256(image.read_bytes()).hexdigest()+'  '+image.name+'\n')
print(image)
