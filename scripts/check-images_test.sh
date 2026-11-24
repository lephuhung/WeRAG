#!/usr/bin/env bash
# Tests of scripts/check-images.sh and scripts/image-sources.sh with a stub
# docker and a stub python3 (run_track.py): bash scripts/check-images_test.sh
# The stub docker knows the containers listed in STUB_CONTAINERS (name=label
# pairs, label "-" for none) and answers ps by compose labels with
# STUB_PS (one name).
set -euo pipefail
DIR=$(cd "$(dirname "$0")" && pwd)
T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT
mkdir -p "$T/bin"
PY=$(command -v python3)

cat >"$T/bin/docker" <<'EOF'
#!/usr/bin/env bash
echo "$*" >>"$STUB_LOG"
case "$1" in
version) [ -z "${STUB_DOCKER_DOWN:-}" ] || exit 1; echo 27.0; exit 0 ;;
ps) [ -n "${STUB_PS:-}" ] && echo "$STUB_PS"; exit 0 ;;
inspect)
	name=${!#}
	for pair in ${STUB_CONTAINERS:-}; do
		[ "${pair%%=*}" = "$name" ] || continue
		label=${pair#*=}
		case "$*" in
		*Labels*) [ "$label" = "-" ] && echo "<no value>" || echo "$label" ;;
		*) echo "/$name" ;;
		esac
		exit 0
	done
	echo "Error: No such container: $name" >&2
	exit 1 ;;
esac
exit 0
EOF
cat >"$T/bin/python3" <<'EOF'
#!/usr/bin/env bash
# run_track.py stub: log the call (and the error file), print a run id
shift # -I
shift # script
echo "$WERAG_RUN_WORKER $*" >>"$STUB_TRACK"
for ((i = 1; i <= $#; i++)); do
	if [ "${!i}" = "--error-file" ]; then j=$((i + 1)); cat "${!j}" >>"$STUB_TRACK"; fi
	if [ "${!i}" = "--summary-file" ]; then j=$((i + 1)); cat "${!j}" >>"$STUB_TRACK"; fi
done
[ "$1" = start ] && echo 7
exit 0
EOF
chmod +x "$T/bin/"*
export PATH="$T/bin:$PATH" DOCKER="$T/bin/docker" STUB_LOG="$T/docker.log" STUB_TRACK="$T/track.log" \
	WERAG_IMAGE_CHECK_ENV="$T/none.env" HOME="$T"

fails=0
check() {
	local name=$1
	shift
	if "$@"; then echo "ok   $name"; else echo "FAIL $name"; fails=$((fails + 1)); fi
}
out_has() { grep -qF -- "$1" "$T/out"; }
track_has() { grep -qF -- "$1" "$T/track.log"; }
reset() { : >"$T/docker.log"; : >"$T/track.log"; unset STUB_PS STUB_CONTAINERS STUB_DOCKER_DOWN || true; }
S="$DIR/image-sources.sh"

# image-sources.sh
check "lists the services" bash -c "'$S' list | grep -qx vbpl-sync && '$S' list | grep -qx frontend && '$S' list | grep -qx docreader"
h1=$("$S" stamp --head docreader)
check "a stamp is stable" [ "$h1" = "$("$S" stamp --head docreader)" ]
check "a stamp is 16 hex digits" bash -c "[[ '$h1' =~ ^[0-9a-f]{16}$ ]]"
check "services have their own stamp" [ "$h1" != "$("$S" stamp --head backup)" ]
check "the build-arg name" [ "$("$S" env vbpl-sync)" = WERAG_SRC_STAMP_VBPL_SYNC ]
check "an unknown service is refused" bash -c "! '$S' stamp nope 2>/dev/null"
# a dirty path marks the working-tree stamp, not HEAD's
G="$T/repo"
mkdir -p "$G/deploy/backup"
git -C "$G" init -q
for f in Dockerfile backup.sh entrypoint.sh restore.sh; do echo "$f" >"$G/deploy/backup/$f"; done
git -C "$G" add -A && git -C "$G" -c user.email=t@t -c user.name=t commit -qm init
clean=$(WERAG_REPO=$G "$S" stamp backup)
check "a clean tree's stamp is HEAD's" [ "$clean" = "$(WERAG_REPO=$G "$S" stamp --head backup)" ]
echo changed >>"$G/deploy/backup/backup.sh"
dirty=$(WERAG_REPO=$G "$S" stamp backup)
check "a dirty tree's stamp says so" bash -c "[[ '$dirty' == $clean-dirty-* ]]"
check "HEAD's stamp ignores the dirty tree" [ "$clean" = "$(WERAG_REPO=$G "$S" stamp --head backup)" ]
git -C "$G" -c user.email=t@t -c user.name=t commit -qam change
check "a commit changes the stamp" [ "$clean" != "$(WERAG_REPO=$G "$S" stamp --head backup)" ]
echo x >"$G/unrelated.txt"
check "an unrelated change does not" [ "$(WERAG_REPO=$G "$S" stamp backup)" = "$(WERAG_REPO=$G "$S" stamp --head backup)" ]

# globs and exclusions: vbpl-sync takes internal/types/legal_*.go but no
# *_test.go and no other file of internal/types; frontend leaves out
# next-env.d.ts, at HEAD as in the working tree
commit() { git -C "$G" add -A && git -C "$G" -c user.email=t@t -c user.name=t commit -qm "$1"; }
mkdir -p "$G/internal/types" "$G/frontend-next"
echo a >"$G/internal/types/legal_a.go"
echo t >"$G/internal/types/legal_a_test.go"
echo o >"$G/internal/types/other.go"
echo n >"$G/frontend-next/next-env.d.ts"
echo p >"$G/frontend-next/page.tsx"
commit globs
vs() { WERAG_REPO=$G "$S" stamp --head vbpl-sync 2>/dev/null; }
fe() { WERAG_REPO=$G "$S" stamp --head frontend; }
v0=$(vs) f0=$(fe)
check "a glob selects its files" bash -c "WERAG_REPO='$G' '$S' files vbpl-sync 2>/dev/null | grep -qx internal/types/legal_a.go"
check "an exclusion drops them from the HEAD files" bash -c "! WERAG_REPO='$G' '$S' files vbpl-sync 2>/dev/null | grep -q _test.go"
echo t2 >>"$G/internal/types/legal_a_test.go"
check "an excluded file does not dirty the stamp" bash -c "[[ \"\$(WERAG_REPO='$G' '$S' stamp vbpl-sync 2>/dev/null)\" != *-dirty-* ]]"
echo o2 >>"$G/internal/types/other.go"
commit "test + other"
check "a test or an unselected file does not change the stamp" [ "$v0" = "$(vs)" ]
echo a2 >>"$G/internal/types/legal_a.go"
commit legal
check "a file the glob selects does" [ "$v0" != "$(vs)" ]
echo n2 >>"$G/frontend-next/next-env.d.ts"
commit next-env
check "next-env.d.ts is out of frontend's HEAD stamp" [ "$f0" = "$(fe)" ]
echo n3 >>"$G/frontend-next/next-env.d.ts"
check "and out of its dirty check" [ "$f0" = "$(WERAG_REPO=$G "$S" stamp frontend)" ]
echo p2 >>"$G/frontend-next/page.tsx"
check "another frontend file dirties it" bash -c "[[ \"\$(WERAG_REPO='$G' '$S' stamp frontend)\" == $f0-dirty-* ]]"
# a plain path selecting nothing (a typo) is reported; a glob is not
WERAG_REPO=$G "$S" stamp --head vbpl-sync 2>"$T/err" >/dev/null
check "a plain path selecting nothing is reported" grep -qF "internal/legalcatalog selects no file at HEAD" "$T/err"
check "a glob selecting nothing is not" bash -c "! grep -q 'legal_\*' '$T/err'"

# check-images.sh
want_dr=$("$S" stamp --head docreader)
want_vs=$("$S" stamp --head vbpl-sync)
reset
export STUB_CONTAINERS="WeKnora-docreader=$want_dr WeKnora-vbpl-sync=$want_vs"
rc=0; bash "$DIR/check-images.sh" >"$T/out" 2>&1 || rc=$?
check "all current: exit 0" [ "$rc" = 0 ]
check "says ok" out_has "docreader      ok"
check "a service without container is not running" out_has "backup         not running"

reset
export STUB_CONTAINERS="WeKnora-docreader=0123456789abcdef WeKnora-vbpl-sync=$want_vs WeKnora-sherpa-asr=- WeKnora-frontend=unknown"
rc=0; bash "$DIR/check-images.sh" >"$T/out" 2>&1 || rc=$?
check "stale or unstamped: exit 1" [ "$rc" = 1 ]
check "an older stamp is stale" out_has "docreader      stale      image 0123456789abcdef, repo $want_dr"
check "no label is unstamped" out_has "sherpa-asr     unstamped"
check "an \"unknown\" label is unstamped" out_has "frontend       unstamped"
check "the gist names them" out_has "image cũ hơn mã nguồn: docreader (chưa có nhãn: frontend, sherpa-asr)"

rc=0; bash "$DIR/check-images.sh" --json >"$T/out" 2>/dev/null || rc=$?
check "--json parses" "$PY" -I -c "import json,sys; d=json.load(open('$T/out')); sys.exit(0 if d['stale']==['docreader'] and d['unstamped']==['frontend','sherpa-asr'] else 1)"
check "--json exits 1 too" [ "$rc" = 1 ]

# a compose-named container (no container_name) is found by its labels
reset
export STUB_PS="spec-airag-refactor-tvpl-run-1" STUB_CONTAINERS="spec-airag-refactor-tvpl-run-1=$("$S" stamp --head tvpl)"
bash "$DIR/check-images.sh" >"$T/out" 2>&1 || true
check "finds tvpl by its compose labels" out_has "tvpl           ok"
check "by project and service" grep -qF "label=com.docker.compose.project=spec-airag-refactor --filter label=com.docker.compose.service=tvpl" "$T/docker.log"

# skipped services are not judged
reset
export STUB_CONTAINERS="WeKnora-sherpa-asr=-"
rc=0; WERAG_IMAGE_CHECK_SKIP=sherpa-asr,app bash "$DIR/check-images.sh" >"$T/out" 2>&1 || rc=$?
check "WERAG_IMAGE_CHECK_SKIP: exit 0" [ "$rc" = 0 ]
check "and not listed" bash -c "! grep -q sherpa-asr '$T/out'"

# --report records the run
reset
export STUB_CONTAINERS="WeKnora-docreader=$want_dr"
bash "$DIR/check-images.sh" --report >"$T/out" 2>&1 || true
check "report: opens a run of image_check" track_has 'image_check start check --summary {"schedule":"17 6 * * *"}'
check "report: closes it ok" track_has "image_check finish 7 ok --summary-file"
check "report: the counts" track_has '{"check":{"ok":1,"stale":0,"unstamped":0,"not_running":'

reset
export STUB_CONTAINERS="WeKnora-docreader=0123456789abcdef"
echo "WERAG_API_KEY=sk-from-file" >"$T/key.env"
chmod 600 "$T/key.env"
rc=0; WERAG_IMAGE_CHECK_ENV="$T/key.env" bash "$DIR/check-images.sh" --report >"$T/out" 2>&1 || rc=$?
check "report: a stale image fails the run" track_has "image_check finish 7 failed --error-file"
check "report: its error ends with the gist" bash -c "grep -B0 -A0 'image cũ hơn mã nguồn: docreader' '$T/track.log' >/dev/null"
check "report: still exits 1" [ "$rc" = 1 ]
check "the key is never printed" bash -c "! grep -q sk-from-file '$T/out' '$T/track.log'"

reset
export STUB_DOCKER_DOWN=1
rc=0; bash "$DIR/check-images.sh" >"$T/out" 2>&1 || rc=$?
check "docker unavailable: exit 2" [ "$rc" = 2 ]

# build-image.sh: the stamp goes in as the service's build arg
cat >"$T/bin/dockerb" <<'STUB'
#!/usr/bin/env bash
echo "$* | ${WERAG_SRC_STAMP_DCS_SYNC:-} | ${GIT_COMMIT:-}" >>"$STUB_LOG"
exit 0
STUB
chmod +x "$T/bin/dockerb"
reset
DOCKER="$T/bin/dockerb" bash "$DIR/build-image.sh" dcs-sync --up >"$T/out" 2>&1
want=$("$S" stamp dcs-sync)
check "build: project, profile, service, stamp, commit" grep -qF "compose -p spec-airag-refactor -f $(cd "$DIR/.." && pwd)/docker-compose.yml --profile dcs-sync build dcs-sync | $want | $(git -C "$DIR" rev-parse --short HEAD)" "$T/docker.log"
check "build --up: recreates the service alone" grep -qF -- "--profile dcs-sync up -d --no-deps dcs-sync" "$T/docker.log"
check "build: a bad call is refused" bash -c "! bash '$DIR/build-image.sh' dcs-sync --bogus 2>/dev/null"

[ "$fails" = 0 ] && echo "all passed" || { echo "$fails failed"; exit 1; }
