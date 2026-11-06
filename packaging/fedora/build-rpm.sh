#!/usr/bin/env bash
# Build the OldIPMI source and binary RPMs into dist/rpm/ (not ~/rpmbuild).
#
# Sources are the committed HEAD (git archive) plus a "go mod vendor" tarball of
# the same commit, so the spec builds offline (mock, COPR). The version comes
# from project() in CMakeLists.txt. Each run replaces the previous dist/rpm/.
set -euo pipefail

usage() {
	cat <<'EOF'
Usage: packaging/fedora/build-rpm.sh [options] [-- rpmbuild-args...]

  --allow-dirty      build even if the worktree has uncommitted or untracked
                     files (the package still contains only the committed HEAD)
  --release N        RPM release number (default 1)
  --mock             build the SRPM, then rebuild it in a clean mock chroot
                     instead of a local rpmbuild -ba (falls back to rpmbuild -ba
                     if mock is not installed)
  --mock-root ROOT   mock config (default fedora-<host release>-<arch>)
  -h, --help         show this help

Output: dist/rpm/SRPMS/*.src.rpm and dist/rpm/RPMS/<arch>/*.rpm
        (with --mock: dist/rpm/mock/). rpmlint runs when installed.
EOF
}

allow_dirty=0
use_mock=0
release=1
mock_root=
rpmbuild_args=()
while (($#)); do
	case $1 in
	--allow-dirty) allow_dirty=1 ;;
	--release) release=${2:?--release needs a value}; shift ;;
	--mock) use_mock=1 ;;
	--mock-root) mock_root=${2:?--mock-root needs a value}; shift ;;
	-h | --help) usage; exit 0 ;;
	--) shift; rpmbuild_args=("$@"); break ;;
	*) echo "build-rpm: unknown option: $1" >&2; usage >&2; exit 2 ;;
	esac
	shift
done

die() { echo "build-rpm: $*" >&2; exit 1; }

[[ $release =~ ^[0-9][0-9A-Za-z._]*$ ]] || die "invalid --release: $release"
for tool in git go rpmbuild tar gzip; do
	command -v "$tool" >/dev/null || die "$tool not found (Fedora: sudo dnf install git golang rpm-build tar gzip)"
done

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$ROOT"

version=$(sed -nE 's/^project\(OldIPMI VERSION ([0-9][0-9.]*)[ )].*/\1/p' CMakeLists.txt)
[[ -n $version ]] || die "cannot read the version from project() in CMakeLists.txt"

if [[ -n $(git status --porcelain --untracked-files=normal) ]]; then
	if ((allow_dirty)); then
		echo "build-rpm: WARNING: the worktree is dirty; the package contains only the committed HEAD." >&2
		echo "build-rpm: (the spec template and this script are read from the working tree)" >&2
	else
		git status --short >&2
		die "the worktree has uncommitted or untracked files; commit them or pass --allow-dirty"
	fi
fi

commit=$(git rev-parse HEAD)
short=$(git rev-parse --short=12 HEAD)
commit_time=$(git log -1 --format=%ct HEAD)
packager=$(git log -1 --format='%an <%ae>' HEAD)
name=oldipmi-$version
topdir=$ROOT/dist/rpm

# Start from an empty tree so no stale RPM of another version is left behind.
rm -rf "$topdir"
mkdir -p "$topdir"/{BUILD,BUILDROOT,RPMS,SOURCES,SPECS,SRPMS}

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

echo "build-rpm: oldipmi $version-$release from $short"

# Source0: the committed tree. git archive sets file times to the commit time,
# so the tarball is the same for the same commit.
git archive --format=tar --prefix="$name/" HEAD | gzip -n >"$topdir/SOURCES/$name.tar.gz"

# Source1: vendored Go modules, made from the archived tree (not the working
# tree) so they match go.mod/go.sum of the packaged commit. Uses the module
# cache; downloads only what is missing from it.
tar -xzf "$topdir/SOURCES/$name.tar.gz" -C "$work"
(cd "$work/$name" && env -u GOFLAGS GOWORK=off go mod vendor) ||
	die "go mod vendor failed (are the modules in the cache, or is the network reachable?)"
tar --sort=name --mtime="@$commit_time" --owner=0 --group=0 --numeric-owner \
	-C "$work/$name" -cf - vendor | gzip -n >"$topdir/SOURCES/$name-vendor.tar.gz"

# Render the spec. The changelog entry is generated from the version and the
# commit; CHANGELOG.md (shipped as %doc) has the details.
changelog_date=$(LC_ALL=C date -u -d "@$commit_time" '+%a %b %d %Y')
changelog="* $changelog_date $packager - $version-$release
- OldIPMI $version, built from git commit $short (details in CHANGELOG.md)"
spec=$(<"$ROOT/packaging/fedora/oldipmi.spec.in")
spec=${spec//@VERSION@/"$version"}
spec=${spec//@RELEASE@/"$release"}
spec=${spec//@COMMIT@/"$commit"}
spec=${spec//@CHANGELOG@/"$changelog"}
printf '%s\n' "$spec" >"$topdir/SPECS/oldipmi.spec"

rpmbuild_cmd=(rpmbuild --define "_topdir $topdir")
if ((use_mock)) && ! command -v mock >/dev/null; then
	echo "build-rpm: mock is not installed (sudo dnf install mock); skipping the mock rebuild and building locally with rpmbuild -ba" >&2
	use_mock=0
fi

if ((use_mock)); then
	"${rpmbuild_cmd[@]}" -bs "${rpmbuild_args[@]}" "$topdir/SPECS/oldipmi.spec"
	: "${mock_root:=fedora-$(rpm -E %fedora)-$(uname -m)}"
	mock -r "$mock_root" --resultdir "$topdir/mock" --rebuild "$topdir"/SRPMS/*.src.rpm
	results=("$topdir"/mock/*.rpm)
else
	# Missing BuildRequires make rpmbuild stop and list them; install them with
	# sudo dnf builddep dist/rpm/SPECS/oldipmi.spec
	"${rpmbuild_cmd[@]}" -ba "${rpmbuild_args[@]}" "$topdir/SPECS/oldipmi.spec"
	results=("$topdir"/SRPMS/*.rpm "$topdir"/RPMS/*/*.rpm)
fi

if command -v rpmlint >/dev/null; then
	echo "build-rpm: rpmlint"
	rpmlint "$topdir/SPECS/oldipmi.spec" "${results[@]}" ||
		echo "build-rpm: rpmlint reported problems (see above); the packages were still built" >&2
else
	echo "build-rpm: rpmlint is not installed (sudo dnf install rpmlint); skipping lint" >&2
fi

echo "build-rpm: done"
ls -l "${results[@]}"
