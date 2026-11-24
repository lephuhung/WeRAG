#!/usr/bin/env bash
# Does every container built from this repo run the code of HEAD? For each
# service of scripts/image-sources.sh with a container (running or not), the
# label werag.src-stamp of the image the container was created from is
# compared with the stamp of HEAD:
#
#   ok           same stamp
#   stale        another stamp: the image is older (or newer, or built from a
#                dirty tree) than HEAD; rebuild with scripts/build-image.sh
#   unstamped    no label: built before the stamp existed; rebuild
#   not running  no container of the service on this host (not judged)
#
#   check-images.sh [--json] [--report]
#
# Exit 0 when nothing is stale or unstamped, 1 otherwise, 2 on a usage or
# docker error. --json prints {"services": [...], "stale": [...],
# "unstamped": [...]} instead of the lines. --report also records the check
# as a run of worker image_check in the app (SuperAdmin page "Thu thập dữ
# liệu", alert legal_sync_alert) through deploy/dcs-sync/run_track.py:
# WERAG_API (default http://127.0.0.1:18080/api/v1) and WERAG_API_KEY (an
# API key of the platform organization with ingest), read from the
# environment or from the file WERAG_IMAGE_CHECK_ENV (default
# ~/.werag-secrets/image-check.env, sourced when present; keep it 0600).
#
# DOCKER (default: docker, or "sudo -n docker" when plain docker is not
# permitted), WERAG_REPO and WERAG_IMAGE_CHECK_SKIP (comma list of services
# not to judge, e.g. a container started from an image pulled elsewhere)
# may be overridden.
set -uo pipefail

HERE=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
export WERAG_REPO=${WERAG_REPO:-$(cd "$HERE/.." && pwd)}
SOURCES="$HERE/image-sources.sh"
PROJECT=spec-airag-refactor
LABEL=werag.src-stamp
SCHEDULE=${WERAG_IMAGE_CHECK_SCHEDULE:-17 6 * * *}

json=0 report=0
for a in "$@"; do
	case "$a" in
	--json) json=1 ;;
	--report) report=1 ;;
	*) echo "usage: check-images.sh [--json] [--report]" >&2; exit 2 ;;
	esac
done

env_file=${WERAG_IMAGE_CHECK_ENV:-$HOME/.werag-secrets/image-check.env}
if [ -f "$env_file" ]; then
	if [ -n "$(find "$env_file" -perm /077 2>/dev/null)" ]; then
		echo "check-images.sh: $env_file is readable by others (chmod 600 it)" >&2
	fi
	set -a
	# shellcheck disable=SC1090
	. "$env_file"
	set +a
fi

if [ -z "${DOCKER:-}" ]; then
	if docker version --format '{{.Server.Version}}' >/dev/null 2>&1; then
		DOCKER=docker
	else
		DOCKER="sudo -n docker"
	fi
fi
read -r -a docker_cmd <<<"$DOCKER"

skip=",${WERAG_IMAGE_CHECK_SKIP:-},"

# container_of SERVICE: its container (by container_name, else by the
# compose labels of the project), "" when there is none
container_of() {
	local name
	name=$("$SOURCES" container "$1")
	if [ -n "$name" ]; then
		"${docker_cmd[@]}" inspect --type container --format '{{.Name}}' "$name" >/dev/null 2>&1 && echo "$name"
		return 0
	fi
	"${docker_cmd[@]}" ps -a --filter "label=com.docker.compose.project=$PROJECT" \
		--filter "label=com.docker.compose.service=$1" --format '{{.Names}}' 2>/dev/null | head -n 1
}

if ! services=$("$SOURCES" list); then
	exit 2
fi
if ! "${docker_cmd[@]}" version >/dev/null 2>&1; then
	echo "check-images.sh: cannot run '$DOCKER'" >&2
	exit 2
fi

lines=() stale=() unstamped=() rows=()
counts_ok=0 counts_nr=0
for svc in $services; do
	[ "${skip#*,"$svc",}" != "$skip" ] && continue
	c=$(container_of "$svc")
	if [ -z "$c" ]; then
		lines+=("$(printf '%-14s not running' "$svc")")
		rows+=("{\"service\":\"$svc\",\"state\":\"not_running\"}")
		counts_nr=$((counts_nr + 1))
		continue
	fi
	want=$("$SOURCES" stamp --head "$svc")
	have=$("${docker_cmd[@]}" inspect --type container --format "{{index .Config.Labels \"$LABEL\"}}" "$c" 2>/dev/null || true)
	[ "$have" = "<no value>" ] && have=""
	if [ -z "$have" ] || [ "$have" = "unknown" ]; then
		state=unstamped
		unstamped+=("$svc")
	elif [ "$have" = "$want" ]; then
		state=ok
		counts_ok=$((counts_ok + 1))
	else
		state=stale
		stale+=("$svc")
	fi
	lines+=("$(printf '%-14s %-10s image %s, repo %s (%s)' "$svc" "$state" "${have:-none}" "$want" "$c")")
	rows+=("{\"service\":\"$svc\",\"state\":\"$state\",\"container\":\"$c\",\"image\":\"$have\",\"repo\":\"$want\"}")
done

join() { local IFS=,; echo "$*"; }
if [ "$json" = 1 ]; then
	sj=$(printf '"%s",' "${stale[@]+"${stale[@]}"}")
	uj=$(printf '"%s",' "${unstamped[@]+"${unstamped[@]}"}")
	[ "$sj" = '"",' ] && sj=""
	[ "$uj" = '"",' ] && uj=""
	echo "{\"services\":[$(join "${rows[@]}")],\"stale\":[${sj%,}],\"unstamped\":[${uj%,}]}"
else
	printf '%s\n' "${lines[@]}"
fi

rc=0
[ ${#stale[@]} -gt 0 ] || [ ${#unstamped[@]} -gt 0 ] && rc=1

# the Vietnamese gist the alert shows (the last line of the run's error):
# "image cũ hơn mã nguồn: docreader, frontend (chưa có nhãn: sherpa-asr)"
comma() { local IFS=,; echo "$*" | sed 's/,/, /g'; }
problem=""
if [ ${#stale[@]} -gt 0 ]; then
	problem="image cũ hơn mã nguồn: $(comma "${stale[@]}")"
	[ ${#unstamped[@]} -gt 0 ] && problem="$problem (chưa có nhãn: $(comma "${unstamped[@]}"))"
elif [ ${#unstamped[@]} -gt 0 ]; then
	problem="image chưa có nhãn nguồn: $(comma "${unstamped[@]}")"
fi

if [ "$report" = 1 ]; then
	track() { WERAG_RUN_WORKER=image_check WERAG_API=${WERAG_API:-http://127.0.0.1:18080/api/v1} \
		python3 -I "$WERAG_REPO/deploy/dcs-sync/run_track.py" "$@"; }
	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' EXIT
	printf '{"check":{"ok":%d,"stale":%d,"unstamped":%d,"not_running":%d}}\n' \
		"$counts_ok" "${#stale[@]}" "${#unstamped[@]}" "$counts_nr" >"$tmp/summary.json"
	if ! id=$(track start check --summary "{\"schedule\":\"$SCHEDULE\"}"); then
		echo "check-images.sh: could not record the check (run start)" >&2
		exit 2
	fi
	if [ "$rc" = 0 ]; then
		track finish "$id" ok --summary-file "$tmp/summary.json" || exit 2
	else
		{ printf '%s\n' "${lines[@]}"; echo "$problem"; } >"$tmp/error.txt"
		track finish "$id" failed --error-file "$tmp/error.txt" --summary-file "$tmp/summary.json" || exit 2
	fi
fi
[ "$rc" = 1 ] && [ "$json" = 0 ] && echo "$problem" >&2
exit "$rc"
