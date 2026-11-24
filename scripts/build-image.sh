#!/usr/bin/env bash
# Build the image of a compose service with its source stamp (label
# werag.src-stamp, scripts/image-sources.sh), and with --up recreate its
# container alone:
#
#   scripts/build-image.sh SERVICE [--up]
#
# Always project spec-airag-refactor and --no-deps; the service's compose
# profile is passed. GIT_COMMIT (vbpl-sync's WERAG_IMAGE_COMMIT) is the
# short HEAD. A build from a tree with uncommitted changes in the service's
# paths gets a "-dirty-…" stamp, which scripts/check-images.sh reports as
# stale until the image is rebuilt from a clean tree.
#
# frontend: other sessions' work in progress in frontend-next/ would go into
# the image; build it from a clean checkout of the commit to ship, and run
# scripts/warm-frontend.sh after --up.
#
# DOCKER overrides the docker command (default docker, or "sudo -n docker"
# when plain docker is not permitted).
set -euo pipefail

HERE=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO=${WERAG_REPO:-$(cd "$HERE/.." && pwd)}
export WERAG_REPO=$REPO
svc=${1:-}
up=0
[ "${2:-}" = "--up" ] && up=1
if [ -z "$svc" ] || { [ $# -gt 1 ] && [ "$up" = 0 ]; }; then
	echo "usage: build-image.sh SERVICE [--up]" >&2
	exit 2
fi
stamp=$("$HERE/image-sources.sh" stamp "$svc")
var=$("$HERE/image-sources.sh" env "$svc")
profile=$("$HERE/image-sources.sh" profile "$svc")

if [ -z "${DOCKER:-}" ]; then
	if docker version --format '{{.Server.Version}}' >/dev/null 2>&1; then
		DOCKER=docker
	else
		DOCKER="sudo -n docker"
	fi
fi
read -r -a docker_cmd <<<"$DOCKER"

export "$var=$stamp"
GIT_COMMIT=$(git -C "$REPO" rev-parse --short HEAD)
export GIT_COMMIT
echo "build-image.sh: $svc stamp $stamp (commit $GIT_COMMIT)"
case "$stamp" in *-dirty-*)
	echo "build-image.sh: warning: uncommitted changes in $svc's paths: the image will be reported stale" >&2
	;;
esac

compose=("${docker_cmd[@]}")
# sudo drops the environment: pass the stamp and the commit explicitly
if [ "${docker_cmd[0]}" = sudo ]; then
	compose=(sudo -n env "$var=$stamp" "GIT_COMMIT=$GIT_COMMIT" "${docker_cmd[@]:2}")
fi
compose+=(compose -p spec-airag-refactor -f "$REPO/docker-compose.yml")
[ -n "$profile" ] && compose+=(--profile "$profile")
cd "$REPO"
"${compose[@]}" build "$svc"
if [ "$up" = 1 ]; then
	"${compose[@]}" up -d --no-deps "$svc"
	if [ "$svc" = frontend ]; then
		echo "build-image.sh: now run scripts/warm-frontend.sh" >&2
	fi
fi
