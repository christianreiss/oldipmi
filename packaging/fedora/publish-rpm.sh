#!/usr/bin/env bash
# Publish already signed OldIPMI binary RPMs through RepoMan.
set -euo pipefail

usage() {
	cat <<'EOF'
Usage: packaging/fedora/publish-rpm.sh SIGNED_RPM [SIGNED_RPM...]

Each package must be an OldIPMI binary RPM for Fedora 43 or later.
The Fedora release and architecture in its RPM header select the RepoMan target.
Use one RPM per target, with the same version and release across targets.
REPOMAN_TOKEN or REPOMAN_TOKEN_FILE authenticates publication; REPOMAN_URL
selects the server (default https://repo.uggs.io).
Keep the signed files and pass them in any order to retry the same publication.
EOF
}

die() { echo "publish-rpm: $*" >&2; exit 1; }

if [[ ${1:-} == --help || ${1:-} == -h ]]; then
	usage
	exit 0
fi
(($#)) || { usage >&2; exit 2; }
for tool in jq realpath repoman rpm; do
	command -v "$tool" >/dev/null || die "$tool not found"
done
[[ -n ${REPOMAN_TOKEN:-}${REPOMAN_TOKEN_FILE:-} ]] || die "set REPOMAN_TOKEN or REPOMAN_TOKEN_FILE"
export REPOMAN_URL=${REPOMAN_URL:-https://repo.uggs.io}

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
declare -A targets=()
package_version=
for path in "$@"; do
	file=$(realpath -e -- "$path") || die "cannot open $path"
	[[ -f $file ]] || die "not a regular file: $file"
	metadata=$(rpm -qp --qf '%{NAME}|%{VERSION}|%{RELEASE}|%{ARCH}|%{SOURCEPACKAGE}' -- "$file") ||
		die "cannot read RPM header: $file"
	IFS='|' read -r name version release arch source <<<"$metadata"
	[[ $name == oldipmi && $source == '(none)' ]] || die "expected an OldIPMI binary RPM: $file"
	[[ $release =~ ^(.+)\.fc([1-9][0-9]{1,2})$ ]] || die "expected a Fedora release: $file"
	fedora=${BASH_REMATCH[2]}
	build_release=${BASH_REMATCH[1]}
	((fedora >= 43)) || die "Fedora $fedora is older than 43"
	case $arch in
		x86_64) target_arch=x86-64 ;;
		aarch64) target_arch=aarch64 ;;
		*) die "unsupported RPM architecture: $arch" ;;
	esac
	target=fedora-$fedora-$target_arch
	[[ ! ${targets[$target]+set} ]] || die "multiple RPMs for $target"
	targets[$target]=1
	: "${package_version:=$version-$build_release}"
	[[ $package_version == "$version-$build_release" ]] || die "mixed OldIPMI versions or releases"
	if [[ -z $(rpm -qp --qf '%|OPENPGP?{signed}:{%|RSAHEADER?{signed}:{%|DSAHEADER?{signed}:{}|}|}|' -- "$file") ]]; then
		die "sign the RPM before publishing: $file"
	fi
	jq -nc --arg file "$file" --arg target "$target" \
		'{file: $file, targets: [$target]}' >>"$work/artifacts.jsonl"
done

# RepoMan derives its idempotency key from the artifact hashes and target list.
jq -s '{project: "oldipmi", artifacts: sort_by(.targets[0])}' \
	"$work/artifacts.jsonl" >"$work/release.json"
repoman publish --manifest "$work/release.json" --json
