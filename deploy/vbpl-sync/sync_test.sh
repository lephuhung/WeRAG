#!/usr/bin/env bash
# Tests of sync.sh's run tracking and entrypoint.sh's stop forwarding with
# stub binaries (no database, MinIO or vbpl.vn): bash deploy/vbpl-sync/sync_test.sh
# Stubs: legal-catalog-import logs its arguments (a -run-start prints 42
# unless START_FAILS names a file that exists, then it is removed: the next
# start works), mc does nothing, python3 (the crawler) sleeps CRAWL_SLEEP
# seconds, records its pid and writes a summary.
set -euo pipefail
DIR=$(cd "$(dirname "$0")" && pwd)
T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT
mkdir -p "$T/bin" "$T/work"
sed -e "s#^WORK=/work#WORK=$T/work#" -e "s#/app/crawl_vbpl.py#$T/crawl.py#" "$DIR/sync.sh" >"$T/sync.sh"
sed -e "s#/usr/local/bin/sync.sh#$T/sync.sh#" -e "s#/tmp/crontab#$T/crontab#" "$DIR/entrypoint.sh" >"$T/entrypoint.sh"
chmod +x "$T/sync.sh" "$T/entrypoint.sh"

cat >"$T/bin/legal-catalog-import" <<EOF
#!/usr/bin/env bash
echo "\$*" >>"$T/calls"
case " \$* " in
*" -run-start "*)
	if [ -n "\${START_FAILS:-}" ] && [ -f "\$START_FAILS" ]; then
		rm -f "\$START_FAILS"; echo "relation legal_sync_runs does not exist" >&2; exit 1
	fi
	echo 42 ;;
*" -documents "*)
	printf 'import output\033[31m red\033[0m\n'
	if [ -n "\${FAIL_IMPORT:-}" ]; then echo "pq: boom" >&2; exit 1; fi ;;
*" -rebuild-text-relations "*)
	if [ -n "\${FAIL_REBUILD:-}" ]; then echo "rebuild boom" >&2; exit 1; fi ;;
esac
exit 0
EOF
# mc: logs its calls; stat answers MC_STAT (ok, notfound, error); ls prints
# MC_LS_FILE (or fails with MC_LS_FAIL); a mirror into a local html/ makes
# the files MC_MIRROR_HTML names
cat >"$T/bin/mc" <<'STUB'
#!/usr/bin/env bash
echo "$*" >>"$MC_CALLS"
case "$1" in
stat)
	case "${MC_STAT:-ok}" in
	notfound) echo "mc: <ERROR> Unable to stat. Object does not exist." >&2; exit 1 ;;
	error) echo "mc: <ERROR> Unable to stat. dial tcp 10.0.0.1:9000: i/o timeout" >&2; exit 1 ;;
	esac ;;
ls)
	[ -n "${MC_LS_FAIL:-}" ] && { echo "mc: <ERROR> Access Denied." >&2; exit 1; }
	[ -n "${MC_LS_FILE:-}" ] && cat "$MC_LS_FILE" ;;
mirror)
	dest=${!#}
	if [ "${dest##*/}" = html ] && [ -n "${MC_MIRROR_HTML:-}" ]; then
		for f in $MC_MIRROR_HTML; do echo x >"$dest/$f"; done
	fi ;;
esac
exit 0
STUB
cat >"$T/bin/python3" <<EOF
#!/usr/bin/env bash
# -I -u script WORK --summary FILE …
echo "crawling"
sleep "\${CRAWL_SLEEP:-0}" &
echo \$! >"$T/crawl.pid"
wait
echo '{"fetched":3}' >"\$6"
if [ -n "\${CRAWL_HELD:-}" ]; then
	echo "06:00:00 vbpl.vn: 300 văn bản cùng chuyển hết hiệu lực toàn bộ trong một lần: chưa xoá toàn văn"
	exit 3
fi
EOF
# like supercronic: runs the job (CRON_RUNS_JOB=1: at once), never forwards
# a signal to it, on SIGTERM waits for the running job
cat >"$T/bin/supercronic" <<STUB
#!/usr/bin/env bash
trap 'wait; exit 0' TERM
if [ -n "\${CRON_RUNS_JOB:-}" ]; then /bin/sh -c "$T/sync.sh" & fi
while :; do sleep 0.2; done
STUB
chmod +x "$T/bin/"*
export MC_CALLS="$T/mccalls"
export TZ=Asia/Ho_Chi_Minh PATH="$T/bin:$PATH" VBPL_S3_ENDPOINT=x VBPL_S3_BUCKET=b VBPL_S3_ACCESS_KEY=a VBPL_S3_SECRET_KEY=s DB_HOST=h DB_NAME=n

fails=0
check() { # check NAME CONDITION…
	local name=$1
	shift
	if "$@"; then echo "ok   $name"; else echo "FAIL $name"; fails=$((fails + 1)); fi
}
has() { grep -qF -- "$1" "$T/calls"; }
lacks() { ! grep -qF -- "$1" "$T/calls"; }
# (a function: a "bash -c" would match its own command line)
no_sync_left() {
	if pgrep -af "$T/sync\.sh" >"$T/left"; then cat "$T/left"; return 1; fi
}
reset() { : >"$T/calls"; : >"$T/mccalls"; rm -f "$T/crawl.pid" "$T/work/.sync.error"; }
mc_has() { grep -qF -- "$1" "$T/mccalls"; }
mc_lacks() { ! grep -qF -- "$1" "$T/mccalls"; }
err_has() { grep -qF -- "$1" "$T/work/.sync.error"; }

# a good night
reset
rc=0; bash "$T/sync.sh" >"$T/out" 2>&1 || rc=$?
check "success exits 0" [ "$rc" = 0 ]
check "opens the run with the default schedule" has '-run-start -run-worker vbpl_sync -run-step prepare -run-summary {"schedule":"30 2 * * *"}'
check "records each step" has '-run-id 42 -run-step push'
check "the import records its counts in the run" has '-auto-ingest-html -run-id 42'
check "closes the run ok" has '-run-id 42 -run-finish ok'

# a failed import
reset
rc=0; FAIL_IMPORT=1 bash "$T/sync.sh" >"$T/out" 2>&1 || rc=$?
check "failure exits non-zero" [ "$rc" != 0 ]
check "closes the run failed with the step's output" has '-run-id 42 -run-finish failed -run-error-file'
check "the error file holds the step's tail" grep -q 'pq: boom' "$T/work/.sync.error"
check "no push after a failed import" lacks '-run-step push'

# tracking off at the start (database not migrated yet): recorded at the end
reset
touch "$T/start-fails"
rc=0; START_FAILS="$T/start-fails" bash "$T/sync.sh" >"$T/out" 2>&1 || rc=$?
check "untracked sync still succeeds" [ "$rc" = 0 ]
check "the import runs untracked" has '-auto-ingest-html -run-id 0'
check "the run is recorded at the end" has '-run-step push -run-summary {"schedule":"30 2 * * *","untracked":true}'
check "and closed ok" has '-run-id 42 -run-finish ok'

# the ticker beats while a step runs
reset
rc=0; CRAWL_SLEEP=3 VBPL_RUN_HEARTBEAT_S=1.07 bash "$T/sync.sh" >"$T/out" 2>&1 || rc=$?
check "heartbeats during a long crawl" has '-run-id 42 -run-heartbeat'
sleep 0.3
check "the ticker and its sleep are gone after the run" bash -c "! pgrep -fx 'sleep 1.07' >/dev/null"

# every import is followed by a rebuild of the "toàn văn" relations
reset
bash "$T/sync.sh" >"$T/out" 2>&1 || true
check "the import runs" has '-documents '
check "the text relations of every source are rebuilt after it" has '-rebuild-text-relations dangcongsan,tvpl,vbpl'
check "the rebuild comes after the import" awk '/^-documents /{i=NR} /^-rebuild-text-relations /{r=NR} END{exit !(i && r > i)}' "$T/calls"
reset
VBPL_TEXT_RELATION_SOURCES= bash "$T/sync.sh" >"$T/out" 2>&1 || true
check "an empty VBPL_TEXT_RELATION_SOURCES skips the rebuild" lacks '-rebuild-text-relations'
reset
VBPL_TEXT_RELATION_SOURCES=dangcongsan bash "$T/sync.sh" >"$T/out" 2>&1 || true
check "VBPL_TEXT_RELATION_SOURCES picks the sources" has '-rebuild-text-relations dangcongsan'
check "only those" lacks '-rebuild-text-relations dangcongsan,'
reset
rc=0; FAIL_REBUILD=1 bash "$T/sync.sh" >"$T/out" 2>&1 || rc=$?
check "a failed rebuild fails the run" [ "$rc" != 0 ]
check "and closes it failed" has '-run-id 42 -run-finish failed -run-error-file'

# prepare: only "does not exist" is a first run
reset
rm -rf "$T/work/state.sqlite" "$T/work/html"
rc=0; MC_STAT=notfound bash "$T/sync.sh" >"$T/out" 2>&1 || rc=$?
check "no copy on MinIO: a first run crawls" bash -c "[ $rc = 0 ] && grep -qF -- '-documents ' '$T/calls'"
check "and restores nothing" mc_lacks "mirror --quiet store/"
reset
rc=0; MC_STAT=error bash "$T/sync.sh" >"$T/out" 2>&1 || rc=$?
check "MinIO unreadable: the run fails" [ "$rc" != 0 ]
check "without crawling from scratch" bash -c "! grep -q crawling '$T/out' && ! grep -qF -- '-documents ' '$T/calls'"
check "the reason is recorded" err_has "refusing to crawl from scratch"
reset
rc=0; MC_STAT=ok bash "$T/sync.sh" >"$T/out" 2>&1 || rc=$?
check "a copy on MinIO is restored" mc_has "mirror --quiet store/b/werag/vbpl $T/work"

# prepare: state.sqlite without html/ while MinIO has HTML
reset
touch "$T/work/state.sqlite"
rm -rf "$T/work/html"
printf '[2026-11-24 02:30:00 UTC]  12KiB STANDARD 1.html\n' >"$T/ls"
rc=0; MC_LS_FILE="$T/ls" MC_MIRROR_HTML="1.html" bash "$T/sync.sh" >"$T/out" 2>&1 || rc=$?
check "html/ missing: restored from MinIO" bash -c "[ $rc = 0 ] && [ -f '$T/work/html/1.html' ]"
check "with a plain mirror (no --remove, no --overwrite)" mc_has "mirror --quiet store/b/werag/vbpl/html $T/work/html"
reset
rm -rf "$T/work/html"
rc=0; MC_LS_FILE="$T/ls" bash "$T/sync.sh" >"$T/out" 2>&1 || rc=$?
check "html/ not restorable: the run fails before crawling" bash -c "[ $rc != 0 ] && ! grep -q crawling '$T/out'"
check "no push" mc_lacks "--overwrite"
reset
rm -rf "$T/work/html"
rc=0; MC_LS_FAIL=1 bash "$T/sync.sh" >"$T/out" 2>&1 || rc=$?
check "html/ missing and MinIO unlistable: fails" bash -c "[ $rc != 0 ] && grep -q 'cannot list' '$T/work/.sync.error'"
reset
rm -rf "$T/work/html"
: >"$T/ls"
rc=0; MC_LS_FILE="$T/ls" bash "$T/sync.sh" >"$T/out" 2>&1 || rc=$?
check "nothing on MinIO either: goes on" [ "$rc" = 0 ]

# push --remove: a mass deletion is not applied
mkdir -p "$T/work/html"
echo x >"$T/work/html/1.html"
for i in $(seq 1 300); do printf '[2026-11-24 02:30:00 UTC]  12KiB STANDARD %s.html\n' "$i"; done >"$T/ls"
reset
rc=0; VBPL_S3_REMOVE=1 MC_LS_FILE="$T/ls" bash "$T/sync.sh" >"$T/out" 2>&1 || rc=$?
check "299 of 300 to delete: mirrored without --remove" bash -c "grep -q -- '--overwrite' '$T/mccalls' && ! grep -q -- '--remove' '$T/mccalls'"
check "and the run fails" [ "$rc" != 0 ]
check "saying why" err_has "push without --remove: --remove would delete 299 of 300 HTML"
reset
rc=0; VBPL_S3_REMOVE=1 VBPL_S3_REMOVE_FORCE=1 MC_LS_FILE="$T/ls" bash "$T/sync.sh" >"$T/out" 2>&1 || rc=$?
check "VBPL_S3_REMOVE_FORCE=1 applies it" bash -c "[ $rc = 0 ] && grep -q -- '--overwrite --remove' '$T/mccalls'"
reset
rc=0; VBPL_S3_REMOVE=1 VBPL_S3_REMOVE_MAX=500 MC_LS_FILE="$T/ls" bash "$T/sync.sh" >"$T/out" 2>&1 || rc=$?
check "within VBPL_S3_REMOVE_MAX: --remove" bash -c "[ $rc = 0 ] && grep -q -- '--overwrite --remove' '$T/mccalls'"
reset
rc=0; VBPL_S3_REMOVE=1 MC_LS_FAIL=1 bash "$T/sync.sh" >"$T/out" 2>&1 || rc=$?
check "remote unlistable: no --remove, the run fails" bash -c "[ $rc != 0 ] && ! grep -q -- '--remove' '$T/mccalls'"

# a crawl that held back a mass expiry: import and push, then fail
reset
rc=0; CRAWL_HELD=1 bash "$T/sync.sh" >"$T/out" 2>&1 || rc=$?
check "held crawl: the import still runs" has '-documents '
check "and the push" mc_has "--overwrite"
check "the run fails" [ "$rc" != 0 ]
check "closed failed" has '-run-id 42 -run-finish failed'
check "the reason is the last line" bash -c "tail -n 1 '$T/work/.sync.error' | grep -qF 'vbpl-sync: crawl: vbpl.vn: 300 văn bản cùng chuyển hết hiệu lực'"
check "no .sync.ok for the night" bash -c "[ ! -f '$T/work/.sync.ok' ] || [ '$T/work/.sync.ok' -ot '$T/work/.sync.error' ]"

# a stop (docker stop / SIGTERM) during the crawl
reset
CRAWL_SLEEP=60 bash "$T/sync.sh" >"$T/out" 2>&1 &
pid=$!
for _ in $(seq 50); do [ -s "$T/crawl.pid" ] && break; sleep 0.1; done
crawl=$(cat "$T/crawl.pid" 2>/dev/null || echo 0)
start=$(date +%s)
kill -TERM "$pid"
rc=0; wait "$pid" || rc=$?
took=$(($(date +%s) - start))
check "a stop exits 143 at once (${took}s)" [ "$rc" = 143 ] && [ "$took" -le 5 ]
check "the crawl is killed with the sync" bash -c "! kill -0 $crawl 2>/dev/null"
check "the stopped run is closed failed" has '-run-id 42 -run-finish failed'
check "its error says it was stopped" grep -q 'stopped (SIGTERM/SIGINT)' "$T/work/.sync.error"
check "the lock is free again" bash -c "exec 9>'$T/work/.sync.lock'; flock -n 9"

# docker stop: SIGTERM reaches the entrypoint (PID 1), which forwards it to
# the running sync.sh: the start-up run, then a run supercronic started
for mode in on_start cron; do
	reset
	if [ "$mode" = on_start ]; then
		VBPL_SYNC_ON_START=1 CRAWL_SLEEP=60 bash "$T/entrypoint.sh" >"$T/out" 2>&1 &
	else
		CRON_RUNS_JOB=1 CRAWL_SLEEP=60 bash "$T/entrypoint.sh" >"$T/out" 2>&1 &
	fi
	pid=$!
	for _ in $(seq 50); do [ -s "$T/crawl.pid" ] && break; sleep 0.1; done
	crawl=$(cat "$T/crawl.pid" 2>/dev/null || echo 0)
	check "$mode: the crawl runs" [ "$crawl" != 0 ]
	start=$(date +%s)
	kill -TERM "$pid"
	rc=0; wait "$pid" || rc=$?
	took=$(($(date +%s) - start))
	check "$mode: the entrypoint stops at once with 143 (${took}s)" [ "$rc" = 143 ] && [ "$took" -le 5 ]
	check "$mode: the crawl is killed" bash -c "! kill -0 $crawl 2>/dev/null"
	check "$mode: the run is closed failed as stopped" has '-run-id 42 -run-finish failed'
	sleep 0.3
	check "$mode: no sync.sh left" no_sync_left
	pkill -f "$T/bin/supercronic" 2>/dev/null || true
done

echo
[ "$fails" = 0 ] && echo "all passed" || { echo "$fails failed"; exit 1; }
