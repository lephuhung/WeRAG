#!/usr/bin/env bash
# Tests of sync.sh's run tracking and entrypoint.sh's stop forwarding with
# stub scripts (no dangcongsan portals, no WeRAG): bash deploy/dcs-sync/sync_test.sh
# The real run_track.py talks to a stub backend (python http.server) that
# logs every call to $T/calls ("POST /path body") and answers a start with
# run id 42 (500 instead while the file named by START_FAILS exists: it is
# removed, the next start works; 409 while $T/busy exists). A finish gets
# 500 while $T/finish-fails holds a positive count (decremented), and the
# first finish hangs 30 s when $T/finish-hang exists (removed). Stub
# dcs_*.py print what the real ones print; dcs_fetch.py sleeps FETCH_SLEEP
# s, records its pid and prints the API key when PRINT_KEY is set;
# dcs_push.py exits PUSH_RC (4: two pushes failed) and gives a document up
# when GAVE_UP is set.
set -euo pipefail
DIR=$(cd "$(dirname "$0")" && pwd)
T=$(mktemp -d)
SERVER=""
cleanup() {
	[ -n "$SERVER" ] && kill "$SERVER" 2>/dev/null
	rm -rf "$T"
}
trap cleanup EXIT
mkdir -p "$T/bin" "$T/work" "$T/app"
sed -e "s#^APP=/app#APP=$T/app#" "$DIR/sync.sh" >"$T/sync.sh"
sed -e "s#/usr/local/bin/sync.sh#$T/sync.sh#" -e "s#/tmp/crontab#$T/crontab#" "$DIR/entrypoint.sh" >"$T/entrypoint.sh"
chmod +x "$T/sync.sh" "$T/entrypoint.sh"
cp "$DIR/run_track.py" "$T/app/"

cat >"$T/app/dcs_fetch.py" <<EOF
import os, sys, time
site = sys.argv[sys.argv.index("--site") + 1]
open("$T/fetch.pid", "w").write(str(os.getpid()))
time.sleep(float(os.environ.get("FETCH_SLEEP") or 0))
print({"tulieu": "120 records (30 loaded now) → x", "org": "80 records (20 loaded now) → y"}[site], file=sys.stderr, flush=True)
if os.environ.get("PRINT_KEY"):
    print("debug key=" + os.environ["WERAG_API_KEY"], file=sys.stderr, flush=True)
EOF
cat >"$T/app/dcs_classify.py" <<'EOF'
import sys
print("10 records → out", file=sys.stderr)
EOF
cat >"$T/app/dcs_build.py" <<'EOF'
import sys
print("5 documents: text 2, ocr 1, catalogue 1, skip 1", file=sys.stderr)
EOF
cat >"$T/app/dcs_push.py" <<'EOF'
import os, sys
print("200 full      01-QĐ/TW             created", flush=True)
print("200 queue     02-CT/TW             queued", flush=True)
print("409 full      03-NQ/TW             đã có toàn văn", flush=True)
if os.environ.get("GAVE_UP"):
    print("GAVE_UP queue     06-TB/TW             máy chủ lỗi 5 lần chạy liên tiếp (HTTP 500)", flush=True)
rc = int(os.environ.get("PUSH_RC") or 0)
if rc == 4:
    print("0 full      04-KL/TW              transport: <urlopen error [Errno -3] Temporary failure in name resolution>", flush=True)
    print("503 queue     05-HD/TW              upstream unavailable", flush=True)
    print("2 pushes failed (HTTP 404/429/5xx, transport…): retried next run", file=sys.stderr, flush=True)
sys.exit(rc)
EOF

cat >"$T/server.py" <<EOF
import http.server, json, os, sys, time
class H(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        n = int(self.headers.get("Content-Length") or 0)
        body = self.rfile.read(n).decode("utf-8")
        with open("$T/calls", "a", encoding="utf-8") as f:
            f.write(f"POST {self.path} key={self.headers.get('X-API-Key')} {body}\n")
        fail = os.environ.get("START_FAILS", "")
        if self.path.endswith("/legal-sync/runs") and fail and os.path.exists(fail):
            os.remove(fail)
            self.send_response(500); self.end_headers(); self.wfile.write(b'{"error":"relation does not exist"}'); return
        if self.path.endswith("/legal-sync/runs") and os.path.exists("$T/busy"):
            self.send_response(409); self.end_headers(); self.wfile.write(b'{"error":"another run of the worker is in progress"}'); return
        if self.path.endswith("/finish") and os.path.exists("$T/finish-hang"):
            os.remove("$T/finish-hang"); time.sleep(30)
        if self.path.endswith("/finish") and os.path.exists("$T/finish-fails"):
            n = int(open("$T/finish-fails").read() or 0)
            if n > 0:
                open("$T/finish-fails", "w").write(str(n - 1))
                self.send_response(503); self.end_headers(); self.wfile.write(b'{"error":"unavailable"}'); return
        out = {"success": True, "data": {"id": 42}} if self.path.endswith("/legal-sync/runs") else {"success": True}
        raw = json.dumps(out).encode()
        self.send_response(200); self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw))); self.end_headers(); self.wfile.write(raw)
    def log_message(self, *a): pass
s = http.server.ThreadingHTTPServer(("127.0.0.1", 0), H)
open("$T/port", "w").write(str(s.server_address[1]))
s.serve_forever()
EOF
START_FAILS="$T/start-fails" python3 "$T/server.py" &
SERVER=$!
for _ in $(seq 50); do [ -s "$T/port" ] && break; sleep 0.1; done
PORT=$(cat "$T/port")

# like supercronic: runs the job (CRON_RUNS_JOB=1: at once), never forwards
# a signal to it, on SIGTERM waits for the running job
cat >"$T/bin/supercronic" <<STUB
#!/usr/bin/env bash
trap 'wait; exit 0' TERM
if [ -n "\${CRON_RUNS_JOB:-}" ]; then /bin/sh -c "$T/sync.sh" & fi
while :; do sleep 0.2; done
STUB
chmod +x "$T/bin/"*
export TZ=Asia/Ho_Chi_Minh PATH="$T/bin:$PATH" DCS_WORK_DIR="$T/work" WERAG_API="http://127.0.0.1:$PORT/api/v1" \
	WERAG_API_KEY=sk-test-secret-key DCS_RUN_TRACK_TIMEOUT=3 DCS_RUN_RETRY_S=0.2

fails=0
check() { # check NAME CONDITION…
	local name=$1
	shift
	if "$@"; then echo "ok   $name"; else echo "FAIL $name"; fails=$((fails + 1)); fi
}
has() { grep -qF -- "$1" "$T/calls"; }
lacks() { ! grep -qF -- "$1" "$T/calls"; }
no_sync_left() {
	if pgrep -af "$T/sync\.sh" >"$T/left"; then cat "$T/left"; return 1; fi
}
reset() { : >"$T/calls"; rm -f "$T/fetch.pid" "$T/work/.sync.error" "$T/work/.sync.ok" "$T/busy" "$T/finish-fails" "$T/finish-hang"; }
count() { grep -cF -- "$1" "$T/calls" || true; }

# a good run
reset
rc=0; bash "$T/sync.sh" >"$T/out" 2>&1 || rc=$?
check "success exits 0" [ "$rc" = 0 ]
check "opens the run with the default schedule and the key" \
	has 'POST /api/v1/system/legal-sync/runs key=sk-test-secret-key {"worker": "dcs_sync", "step": "fetch", "summary": {"schedule": "15 */4 * * *"}}'
check "the fetch counts go with the move to build" \
	has 'POST /api/v1/system/legal-sync/runs/42/step key=sk-test-secret-key {"step": "build", "summary": {"fetch": {"records": 200, "loaded": 50}}}'
check "the build counts go with the move to upload" \
	has '{"step": "upload", "summary": {"build": {"planned": 5, "text": 2, "ocr": 1, "catalogue": 1, "skip": 1}}}'
check "closes the run ok with the upload counts" \
	has '/runs/42/finish key=sk-test-secret-key {"status": "ok", "summary": {"upload": {"pushed": 1, "queued": 1, "existing": 1, "refused": 0, "failed": 0}}}'
check "healthcheck file touched" [ -f "$T/work/.sync.ok" ]

# pushes failed (dcs_push exit 4: the DNS incident)
reset
rc=0; PUSH_RC=4 bash "$T/sync.sh" >"$T/out" 2>&1 || rc=$?
check "push failures exit 4" [ "$rc" = 4 ]
check "closes the run failed with the counts" has '"status": "failed", "summary": {"upload": {"pushed": 1, "queued": 1, "existing": 1, "refused": 0, "failed": 2}}'
check "the error holds the failed pushes" grep -q 'Temporary failure in name resolution' "$T/work/.sync.error"
check "and the script's verdict" grep -q '2 pushes failed' "$T/work/.sync.error"
check "not the documents that went through" bash -c "! grep -q '01-QĐ/TW' '$T/work/.sync.error'"
check "the error reaches the backend" has '"error": "0 full      04-KL/TW'
check "no healthcheck file after a failure" [ ! -f "$T/work/.sync.ok" ]

# a document given up by dcs_push is counted
reset
rc=0; GAVE_UP=1 bash "$T/sync.sh" >"$T/out" 2>&1 || rc=$?
check "a given-up document does not fail the run" [ "$rc" = 0 ]
check "it is counted" has '"failed": 0, "gave_up": 1}'

# the API key is masked in the step output (docker logs, step log)
reset
rc=0; PRINT_KEY=1 bash "$T/sync.sh" >"$T/out" 2>&1 || rc=$?
check "masked output: the sync succeeds" [ "$rc" = 0 ]
check "the key is not in docker logs" bash -c "! grep -q sk-test-secret-key '$T/out'"
check "the masked line is there" grep -q 'debug key=\*\*\*' "$T/out"

# another dcs-sync run is alive on the backend (409): do nothing, exit 0
reset
touch "$T/busy"
rc=0; bash "$T/sync.sh" >"$T/out" 2>&1 || rc=$?
check "busy backend: exit 0" [ "$rc" = 0 ]
check "it says why" grep -q 'another dcs-sync run is in progress' "$T/out"
check "the fetch did not run" [ ! -e "$T/fetch.pid" ]
check "nothing but the refused start was sent" [ "$(wc -l <"$T/calls")" = 1 ]
check "the lock is free after a skip" bash -c "exec 9>'$T/work/.sync.lock'; flock -n 9"

# closing the run is retried after a failure worth retrying
reset
echo 2 >"$T/finish-fails"
rc=0; bash "$T/sync.sh" >"$T/out" 2>&1 || rc=$?
check "finish retried: the sync succeeds" [ "$rc" = 0 ]
check "three finish calls, the last one through" [ "$(count '/runs/42/finish')" = 3 ]
reset
echo 5 >"$T/finish-fails"
rc=0; bash "$T/sync.sh" >"$T/out" 2>&1 || rc=$?
check "finish tried three times at most" [ "$(count '/runs/42/finish')" = 3 ]
check "and the sync still succeeds" [ "$rc" = 0 ]

# the key never reaches the error text
reset
printf 'import sys\nprint("boom sk-test-secret-key", file=sys.stderr)\nsys.exit(1)\n' >"$T/app/dcs_build.py.fail"
mv "$T/app/dcs_build.py" "$T/app/dcs_build.py.ok"; cp "$T/app/dcs_build.py.fail" "$T/app/dcs_build.py"
rc=0; bash "$T/sync.sh" >"$T/out" 2>&1 || rc=$?
mv "$T/app/dcs_build.py.ok" "$T/app/dcs_build.py"
check "a failed build exits non-zero" [ "$rc" != 0 ]
check "closes the run failed at build" has '"error": "10 records → out'
check "the key is masked in the error" has 'boom ***'
check "no upload after a failed build" lacks '"step": "upload"'

# tracking off at the start (backend down for the start): recorded at the end
reset
touch "$T/start-fails"
rc=0; START_FAILS="$T/start-fails" bash "$T/sync.sh" >"$T/out" 2>&1 || rc=$?
check "untracked sync still succeeds" [ "$rc" = 0 ]
check "no step recorded while untracked" lacks '/runs/42/step'
check "the run is recorded at the end" has '{"worker": "dcs_sync", "step": "upload", "summary": {"schedule": "15 */4 * * *", "untracked": true}}'
check "and closed ok" has '/runs/42/finish'

# the tracking endpoint unreachable all along: the sync still runs
reset
rc=0; WERAG_API="http://127.0.0.1:1/api/v1" bash "$T/sync.sh" >"$T/out" 2>&1 || rc=$?
check "backend unreachable: the sync still succeeds" [ "$rc" = 0 ]
check "it says tracking is off" grep -q 'run tracking off' "$T/out"
check "nothing reached the stub" [ ! -s "$T/calls" ]

# the ticker beats while a step runs
reset
rc=0; FETCH_SLEEP=1.5 DCS_RUN_HEARTBEAT_S=1.07 bash "$T/sync.sh" >"$T/out" 2>&1 || rc=$?
check "heartbeats during a long fetch" has '/runs/42/heartbeat'
sleep 0.3
check "the ticker and its sleep are gone after the run" bash -c "! pgrep -fx 'sleep 1.07' >/dev/null"

# a stop (docker stop / SIGTERM) during the fetch
reset
FETCH_SLEEP=60 bash "$T/sync.sh" >"$T/out" 2>&1 &
pid=$!
for _ in $(seq 50); do [ -s "$T/fetch.pid" ] && break; sleep 0.1; done
fetch=$(cat "$T/fetch.pid" 2>/dev/null || echo 0)
start=$(date +%s)
kill -TERM "$pid"
rc=0; wait "$pid" || rc=$?
took=$(($(date +%s) - start))
check "a stop exits 143 at once (${took}s)" bash -c "[ $rc = 143 ] && [ $took -le 5 ]"
check "the fetch is killed with the sync" bash -c "! kill -0 $fetch 2>/dev/null"
check "the stopped run is closed failed" has '/runs/42/finish key=sk-test-secret-key {"status": "failed"'
check "its error says it was stopped" grep -q 'stopped (SIGTERM/SIGINT)' "$T/work/.sync.error"
check "the lock is free again" bash -c "exec 9>'$T/work/.sync.lock'; flock -n 9"

# a stop while the backend hangs: the finish gives up after 10 s, the retry
# goes through, well within the entrypoint's 80 s
reset
touch "$T/finish-hang"
FETCH_SLEEP=60 bash "$T/sync.sh" >"$T/out" 2>&1 &
pid=$!
for _ in $(seq 50); do [ -s "$T/fetch.pid" ] && break; sleep 0.1; done
start=$(date +%s)
kill -TERM "$pid"
rc=0; wait "$pid" || rc=$?
took=$(($(date +%s) - start))
check "a stop with a hanging backend ends in about 10 s (${took}s)" bash -c "[ $rc = 143 ] && [ $took -ge 6 ] && [ $took -le 15 ]"
check "the retried finish was recorded" [ "$(count '/runs/42/finish')" = 2 ]

# docker stop: SIGTERM reaches the entrypoint (PID 1), which forwards it to
# the running sync.sh: the start-up run, then a run supercronic started
for mode in on_start cron; do
	reset
	if [ "$mode" = on_start ]; then
		DCS_SYNC_ON_START=1 FETCH_SLEEP=60 bash "$T/entrypoint.sh" >"$T/out" 2>&1 &
	else
		CRON_RUNS_JOB=1 FETCH_SLEEP=60 bash "$T/entrypoint.sh" >"$T/out" 2>&1 &
	fi
	pid=$!
	for _ in $(seq 50); do [ -s "$T/fetch.pid" ] && break; sleep 0.1; done
	fetch=$(cat "$T/fetch.pid" 2>/dev/null || echo 0)
	check "$mode: the fetch runs" [ "$fetch" != 0 ]
	start=$(date +%s)
	kill -TERM "$pid"
	rc=0; wait "$pid" || rc=$?
	took=$(($(date +%s) - start))
	check "$mode: the entrypoint stops at once with 143 (${took}s)" bash -c "[ $rc = 143 ] && [ $took -le 5 ]"
	check "$mode: the fetch is killed" bash -c "! kill -0 $fetch 2>/dev/null"
	check "$mode: the run is closed failed as stopped" has '/runs/42/finish key=sk-test-secret-key {"status": "failed"'
	sleep 0.3
	check "$mode: no sync.sh left" no_sync_left
	pkill -f "$T/bin/supercronic" 2>/dev/null || true
done

echo
[ "$fails" = 0 ] && echo "all passed" || { echo "$fails failed"; exit 1; }
