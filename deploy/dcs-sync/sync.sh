#!/usr/bin/env bash
# Một lần đồng bộ văn bản của Đảng (scripts/dangcongsan):
#   1. fetch: tải lại DCS_REFRESH_PAGES trang danh sách đầu của mỗi cổng và
#      trang chi tiết của chúng (văn bản mới, tệp gắn thêm sau); trang sâu
#      hơn và tệp đã có lấy từ cache;
#   2. build: phân loại tệp, gộp hai cổng, dựng HTML (dcs_classify, dcs_build);
#   3. upload: đẩy phần chưa đẩy (dcs_push, sổ push-results.jsonl): văn bản
#      có chữ → toàn văn vào kho "Văn bản của Đảng"; bản scan / không có tệp
#      → dòng danh mục + đề nghị trong hàng chờ của extension TVPL.
# Mỗi lần chạy được ghi vào legal_sync_runs (worker dcs_sync: bước, số liệu,
# lỗi) qua API của backend (run_track.py, cùng API key với lệnh đẩy) cho
# trang SuperAdmin Pháp lý → Thu thập dữ liệu và cảnh báo legal_sync_alert.
# Có lệnh đẩy lỗi (dcs_push thoát 4: HTTP 404/429/5xx, mạng) → lần chạy lỗi.
set -euo pipefail
# Its own process group, so a stop (SIGTERM) can end the running step and
# everything it started (setsid execs in place: same pid, supercronic still
# waits for it). /proc/self/stat after "(comm) ": state ppid pgrp …
pgrp_of() { local st; st=$(<"/proc/$1/stat"); st=${st##*) }; set -- $st; echo "$3"; }
if [ "$(pgrp_of $$)" != "$$" ] && [ -z "${DCS_SYNC_SETSID:-}" ] && command -v setsid >/dev/null; then
	export DCS_SYNC_SETSID=1
	exec setsid "$BASH" "$0" "$@"
fi
GROUP_LEADER=0
[ "$(pgrp_of $$)" = "$$" ] && GROUP_LEADER=1

WORK=${DCS_WORK_DIR:?DCS_WORK_DIR is required}
log() { echo "$(date -u +%FT%TZ) dcs-sync: $*"; }

exec 9>"$WORK/.sync.lock"
flock -n 9 || { log "skip: previous run still going"; exit 0; }
touch "$WORK/.sync.started"
: "${WERAG_API_KEY:?WERAG_API_KEY is required (API key of organization 10000 with ingest)}"

PY="python3 -I -u"
APP=/app
# Run tracking for the SuperAdmin monitor (legal_sync_runs through the
# backend's /system/legal-sync/runs endpoints, run_track.py). It never
# fails the sync: without a run id the sync goes on untracked and is
# recorded at its end. Each tracking call is bounded (each HTTP request
# DCS_RUN_TRACK_TIMEOUT s inside, the whole call 3× that: DNS is not
# bounded by the socket timeout; 10 s on a stop, within the entrypoint's
# 80 s); closing the run is tried three times. A ticker touches the run
# every DCS_RUN_HEARTBEAT_S s: a running run silent for 30 minutes is
# closed as interrupted by the monitor. Each step's output (API key
# masked) is kept in .sync.step.log; a step's counts go into the run, a
# failed run records the output's tail.
#
# Another dcs-sync (a second container on the same backend, another work
# dir: the lock does not see it) whose run is alive makes the backend
# refuse the start (409): this sync then does nothing and exits 0, leaving
# the run to the other one.
STEP_LOG="$WORK/.sync.step.log"
SUMMARY="$WORK/.sync.summary.json"
TRACK_TIMEOUT=$((3 * ${DCS_RUN_TRACK_TIMEOUT:-20}))
STEP=""
CHILD=""
TICKER=""
# the schedule as entrypoint.sh sets it (quotes and backslashes dropped: JSON)
SCHEDULE=${DCS_SYNC_CRON:-15 */4 * * *}
SCHEDULE=${SCHEDULE//[\"\\]/}
RUN_ID=""
OPEN_RC=0
TRACK_RC=0
SKIP=0
# Every call that may take a while runs in the background and is waited
# for: a trapped signal interrupts a wait, while a foreground command (or
# a command substitution) would delay the trap until it ends.
#
# open_run STEP SUMMARY opens a run and sets RUN_ID ("" when tracking is off)
open_run() {
	local f="$WORK/.sync.run.id"
	: >"$f"
	OPEN_RC=0
	timeout --foreground "$TRACK_TIMEOUT" $PY "$APP/run_track.py" start "$1" --summary "$2" \
		>"$f" 2>>"$WORK/.sync.run.err" &
	wait $! || OPEN_RC=$?
	RUN_ID=$(<"$f")
	# only an id
	[[ $RUN_ID =~ ^[0-9]+$ ]] || RUN_ID=""
}
# track CMD ARGS… (never fails; TRACK_RC: run_track.py's exit code, 124 on
# timeout)
track() {
	TRACK_RC=0
	[ -n "$RUN_ID" ] || return 0
	timeout --foreground "$TRACK_TIMEOUT" $PY "$APP/run_track.py" "$1" "$RUN_ID" "${@:2}" >/dev/null 2>>"$WORK/.sync.run.err" &
	wait $! || TRACK_RC=$?
	[ "$TRACK_RC" = 0 ] || log "run tracking ($1) failed: $(tail -n 1 "$WORK/.sync.run.err" 2>/dev/null)"
}
# track_retry: track, again up to twice after a failure worth retrying
# (transport, 5xx, timeout; not a refusal)
track_retry() {
	local i
	for i in 1 2 3; do
		track "$@"
		case "$TRACK_RC" in 0 | 2 | 3 | 4) return 0 ;; esac
		[ "$i" = 3 ] || sleep "${DCS_RUN_RETRY_S:-2}"
	done
}
# mask: the step's output with the API key masked (docker logs and the
# step log alike)
mask() { $PY "$APP/run_track.py" mask; }
# summarize: the counts of the current step's output, into $SUMMARY
summarize() {
	: >"$SUMMARY"
	[ -n "$STEP" ] || return 0
	$PY "$APP/run_track.py" summarize "$STEP" "$STEP_LOG" >"$SUMMARY" 2>/dev/null &
	wait $! || : >"$SUMMARY"
}
# ticker touches the run while the sync lives (it leaves when the sync is
# gone, e.g. killed hard)
ticker() {
	local s=""
	trap '[ -n "$s" ] && kill "$s" 2>/dev/null; exit 0' TERM
	while :; do
		sleep "${DCS_RUN_HEARTBEAT_S:-60}" &
		s=$!
		wait "$s" || true
		kill -0 $$ 2>/dev/null || exit 0
		track heartbeat
	done
}
# step NAME: the previous step's counts go with the move
step() {
	log "$1"
	summarize
	STEP=$1
	track step "$1" --summary-file "$SUMMARY"
	: >"$STEP_LOG"
}
# run CMD… runs a step's command in the background and waits for it.
run() {
	"$@" &
	CHILD=$!
	wait "$CHILD"
	CHILD=""
}
finish() {
	rc=$?
	trap '' TERM INT
	[ "$SKIP" = 1 ] && return 0
	if [ "$rc" -eq 143 ]; then
		# a stop: the entrypoint waits 80 s at most
		TRACK_TIMEOUT=10
		export DCS_RUN_TRACK_TIMEOUT=8
	fi
	if [ -n "$TICKER" ]; then
		kill "$TICKER" 2>/dev/null || true
		wait "$TICKER" 2>/dev/null || true
	fi
	summarize
	if [ -z "$RUN_ID" ]; then
		# tracking was off at the start: record the run now, so a good run
		# is not taken for a missed one
		open_run "${STEP:-fetch}" "{\"schedule\":\"$SCHEDULE\",\"untracked\":true}"
		[ "$OPEN_RC" = 3 ] && log "run not recorded: another dcs-sync run is in progress"
	fi
	if [ "$rc" -eq 0 ]; then
		track_retry finish ok --summary-file "$SUMMARY"
	else
		# the upload's tail: only the failed pushes and the script's verdict
		# (a 2xx/409 line is a document that went through)
		if [ "$STEP" = upload ]; then
			grep -Ev '^(2[0-9][0-9]|409) ' "$STEP_LOG" 2>/dev/null | tail -c 8000 >"$WORK/.sync.error" || true
		else
			tail -c 8000 "$STEP_LOG" >"$WORK/.sync.error" 2>/dev/null || true
		fi
		[ "$rc" -eq 143 ] && echo "dcs-sync: stopped (SIGTERM/SIGINT)" >>"$WORK/.sync.error"
		[ "$rc" -eq 4 ] && echo "dcs-sync: some pushes failed (exit 4), retried next run" >>"$WORK/.sync.error"
		track_retry finish failed --error-file "$WORK/.sync.error" --summary-file "$SUMMARY"
	fi
}
# on a stop, end the running step and everything it started (the whole
# process group when it is ours), then record the run as failed
stop() {
	trap '' TERM INT
	log "stopping"
	if [ "$GROUP_LEADER" = 1 ]; then
		kill -TERM 0 2>/dev/null || true
	else
		[ -n "$CHILD" ] && kill -TERM "$CHILD" 2>/dev/null
		[ -n "$TICKER" ] && kill -TERM "$TICKER" 2>/dev/null
	fi
	exit 143
}
# the traps first: a stop while the run is being opened is handled too
trap finish EXIT
trap stop TERM INT

: >"$WORK/.sync.run.err"
open_run fetch "{\"schedule\":\"$SCHEDULE\"}"
if [ "$OPEN_RC" = 3 ]; then
	log "skip: another dcs-sync run is in progress on this backend ($(tail -n 1 "$WORK/.sync.run.err" 2>/dev/null))"
	SKIP=1
	exit 0
fi
[ -n "$RUN_ID" ] || log "run tracking off: $(tail -n 1 "$WORK/.sync.run.err" 2>/dev/null)"
[ -n "$RUN_ID" ] && { ticker 9>&- & TICKER=$!; }

PAGES=${DCS_REFRESH_PAGES:-3}
DELAY=${DCS_DELAY:-1}
fetch() {
	{
		$PY "$APP/dcs_fetch.py" --site tulieu --cache-dir "$WORK/cache" --out-dir "$WORK/run-tulieu" --delay "$DELAY" --refresh-pages "$PAGES"
		$PY "$APP/dcs_fetch.py" --site org --cache-dir "$WORK/cache" --out-dir "$WORK/run-org" --delay "$DELAY" --refresh-pages "$PAGES"
	} 2>&1 | mask | tee -a "$STEP_LOG"
}
build() {
	{
		$PY "$APP/dcs_classify.py" "$WORK/run-tulieu/records.jsonl" "$WORK/run-tulieu/classified.jsonl"
		$PY "$APP/dcs_classify.py" "$WORK/run-org/records.jsonl" "$WORK/run-org/classified.jsonl"
		$PY "$APP/dcs_build.py" "$WORK/run-tulieu/classified.jsonl" "$WORK/run-org/classified.jsonl" --out-dir "$WORK/build"
	} 2>&1 | mask | tee -a "$STEP_LOG"
}
upload() {
	$PY "$APP/dcs_push.py" "$WORK/build" 2>&1 | mask | tee -a "$WORK/push.log" "$STEP_LOG"
}

step fetch
log "first $PAGES listing pages again"
run fetch

step build
run build

step upload
log "push to ${WERAG_API:-default}"
run upload
touch "$WORK/.sync.ok"
log "done"
