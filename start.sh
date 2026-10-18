#!/usr/bin/env bash
# Start the browser console from any working directory.
set -euo pipefail

usage() {
    echo "Usage: $0 [--no-browser] [SERVICE]"
    echo "Build and start a Podman Compose service (default: lab-ami), then open its connect page."
}

open_browser=true
service=lab-ami
if [[ "${1:-}" == --help || "${1:-}" == -h ]]; then
    usage
    exit 0
fi
if [[ "${1:-}" == --no-browser ]]; then
    open_browser=false
    shift
fi
if (( $# > 1 )) || [[ "${1:-}" == -* ]]; then
    usage >&2
    exit 2
fi
service="${1:-$service}"

cd -- "$(dirname -- "${BASH_SOURCE[0]}")"
for command in podman podman-compose curl; do
    command -v "$command" >/dev/null || { echo "Required command missing: $command" >&2; exit 1; }
done
podman info >/dev/null
compose=(podman-compose -f "$PWD/docker-compose.yml")

service_container() {
    podman ps --all --quiet \
        --filter "label=com.docker.compose.project.working_dir=$PWD" \
        --filter "label=com.docker.compose.service=$service"
}

# podman-compose tracks configuration changes but does not replace a container
# just because its image was rebuilt. Recreate only when the image changed, so
# opening the app again does not interrupt an active BMC session.
"${compose[@]}" build "$service"
up_args=(up --detach --no-build)
container=$(service_container)
if [[ -n "$container" ]]; then
    current_image=$(podman inspect --format '{{.Image}}' "$container")
    image_name=$(podman inspect --format '{{.ImageName}}' "$container")
    built_image=$(podman image inspect --format '{{.Id}}' "$image_name")
    if [[ "$current_image" != "$built_image" ]]; then
        up_args+=(--force-recreate)
    fi
fi

# 'up' applies Compose's published ports; 'run' would omit them by default.
"${compose[@]}" "${up_args[@]}" "$service"
container=$(service_container)
# Use Podman's full address: podman-compose 'port' can return only the number.
binding=$(podman port "$container" 8080/tcp)
if [[ -z "$binding" ]]; then
    echo "Service $service does not publish container port 8080." >&2
    exit 1
fi
case "$binding" in
    0.0.0.0:*) binding="127.0.0.1:${binding##*:}" ;;
    \[::\]:*) binding="[::1]:${binding##*:}" ;;
esac
url="http://$binding/"

echo "Waiting for $url"
ready=false
for (( attempt=0; attempt<60; attempt++ )); do
    # Basic-auth protected pages return 401 and are ready for the browser too.
    status=$(curl --noproxy '*' --silent --output /dev/null --write-out '%{http_code}' \
        --connect-timeout 1 --max-time 2 "$url") || status=000
    if [[ "$status" == 200 || "$status" == 401 ]]; then
        ready=true
        break
    fi
    sleep 1
done
if [[ "$ready" != true ]]; then
    echo "Connect page did not become ready: $url" >&2
    echo "Inspect with: podman-compose -f '$PWD/docker-compose.yml' logs $service" >&2
    exit 1
fi

echo "OldIPMI is ready: $url"
if [[ "$open_browser" == true ]]; then
    if command -v xdg-open >/dev/null && [[ -n "${DISPLAY:-}${WAYLAND_DISPLAY:-}" ]]; then
        if ! xdg-open "$url"; then
            echo "Could not open the browser; open $url manually." >&2
            exit 1
        fi
    elif command -v open >/dev/null && [[ "$(uname -s)" == Darwin ]]; then
        open "$url"
    else
        echo "No desktop browser opener available; open the URL above manually."
    fi
fi
