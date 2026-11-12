#!/usr/bin/env bash
# Tests of sync.sh's run tracking with stub binaries (no database, MinIO or
# vbpl.vn): bash deploy/vbpl-sync/sync_test.sh
# Stubs: legal-catalog-import logs its arguments (a -run-start prints 42
# unless START_FAILS names a file that exists, then it is removed: the next
# start works), mc does nothing, python3 (the crawler) sleeps CRAWL_SLEEP
# seconds, records its pid and writes a summary.
set -euo pipefail
SRC=$(cd "$(dirname "$0")" && pwd)/sync.sh
T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT
mkdir -p "$T/bin" "$T/work"
sed -e "s#^WORK=/work#WORK=$T/work#" -e "s#/app/crawl_vbpl.py#$T/crawl.py#" "$SRC" >"$T/sync.sh"

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
esac
exit 0
EOF
printf '#!/usr/bin/env bash\nexit 0\n' >"$T/bin/mc"
cat >"$T/bin/python3" <<EOF
#!/usr/bin/env bash
# -I -u script WORK --summary FILE …
echo "crawling"
sleep "\${CRAWL_SLEEP:-0}" &
echo \$! >"$T/crawl.pid"
wait
echo '{"fetched":3}' >"\$6"
EOF
chmod +x "$T/bin/"*
export PATH="$T/bin:$PATH" VBPL_S3_ENDPOINT=x VBPL_S3_BUCKET=b VBPL_S3_ACCESS_KEY=a VBPL_S3_SECRET_KEY=s DB_HOST=h DB_NAME=n

fails=0
check() { # check NAME CONDITION…
	local name=$1
	shift
	if "$@"; then echo "ok   $name"; else echo "FAIL $name"; fails=$((fails + 1)); fi
}
has() { grep -qF -- "$1" "$T/calls"; }
lacks() { ! grep -qF -- "$1" "$T/calls"; }
reset() { : >"$T/calls"; rm -f "$T/crawl.pid" "$T/work/.sync.error"; }

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

echo
[ "$fails" = 0 ] && echo "all passed" || { echo "$fails failed"; exit 1; }
