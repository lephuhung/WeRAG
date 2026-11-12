#!/usr/bin/env bash
# Một lần đồng bộ vbpl.vn → danh mục văn bản pháp luật:
#   1. thư mục làm việc trống → khôi phục từ MinIO (state.sqlite, html/…);
#   2. crawl cập nhật thêm (crawl_vbpl.py: chỉ văn bản mới hoặc thay đổi);
#   3. import documents.jsonl / relations.jsonl vào Postgres;
#   4. đẩy thư mục làm việc lên MinIO (mirror; VBPL_S3_REMOVE=1 xoá cả HTML
#      đã gỡ — văn bản chuyển sang hết hiệu lực toàn bộ chỉ còn metadata —
#      cần khoá có quyền xoá, khoá backup chỉ ghi thêm).
# Mỗi lần chạy được ghi vào legal_sync_runs (bước, số liệu, lỗi) cho trang
# SuperAdmin Pháp lý → Thu thập dữ liệu và cảnh báo legal_sync_alert.
set -euo pipefail
# Its own process group, so a stop (SIGTERM) can end the running step and
# everything it started (setsid execs in place: same pid, supercronic still
# waits for it). /proc/self/stat after "(comm) ": state ppid pgrp …
pgrp_of() { local st; st=$(<"/proc/$1/stat"); st=${st##*) }; set -- $st; echo "$3"; }
if [ "$(pgrp_of $$)" != "$$" ] && [ -z "${VBPL_SYNC_SETSID:-}" ] && command -v setsid >/dev/null; then
	export VBPL_SYNC_SETSID=1
	exec setsid "$BASH" "$0" "$@"
fi
GROUP_LEADER=0
[ "$(pgrp_of $$)" = "$$" ] && GROUP_LEADER=1

WORK=/work
log() { echo "$(date -u +%FT%TZ) vbpl-sync: $*"; }

exec 9>"$WORK/.sync.lock"
flock -n 9 || { log "skip: previous run still going"; exit 0; }
touch "$WORK/.sync.started"

# Run tracking for the SuperAdmin monitor (legal_sync_runs, through
# legal-catalog-import's -run-* flags). It never fails the sync: without a
# run id the sync goes on untracked and is recorded at its end. Each
# tracking call is bounded (connect 10 s, whole call VBPL_RUN_TRACK_TIMEOUT
# s). A ticker touches the run every VBPL_RUN_HEARTBEAT_S s: a running run
# silent for 30 minutes is closed as interrupted by the monitor. Each
# step's output is kept in .sync.step.log; a failed run records its tail.
# Files .sync.* stay out of the MinIO mirror.
STEP_LOG="$WORK/.sync.step.log"
CRAWL_SUMMARY="$WORK/.sync.crawl.json"
TRACK_TIMEOUT=${VBPL_RUN_TRACK_TIMEOUT:-60}
STEP=""
CHILD=""
TICKER=""
# the schedule as entrypoint.sh sets it (quotes and backslashes dropped: JSON)
SCHEDULE=${VBPL_SYNC_CRON:-30 2 * * *}
SCHEDULE=${SCHEDULE//[\"\\]/}
RUN_ID=""
# Every call that may take a while runs in the background and is waited
# for: a trapped signal interrupts a wait, while a foreground command (or
# a command substitution) would delay the trap until it ends.
#
# open_run STEP SUMMARY opens a run and sets RUN_ID ("" when tracking is off)
open_run() {
	local f="$WORK/.sync.run.id"
	: >"$f"
	timeout --foreground "$TRACK_TIMEOUT" legal-catalog-import -run-start -run-worker vbpl_sync -run-step "$1" \
		-run-summary "$2" >"$f" 2>"$WORK/.sync.run.err" &
	wait $! || true
	RUN_ID=$(<"$f")
	# only an id: anything else on stdout must never reach -run-id of the import
	[[ $RUN_ID =~ ^[0-9]+$ ]] || RUN_ID=""
}
track() {
	[ -n "$RUN_ID" ] || return 0
	timeout --foreground "$TRACK_TIMEOUT" legal-catalog-import -run-id "$RUN_ID" "$@" >/dev/null 2>&1 &
	wait $! || log "run tracking ($*) failed"
}
# ticker touches the run while the sync lives (it leaves when the sync is
# gone, e.g. killed hard)
ticker() {
	local s=""
	trap '[ -n "$s" ] && kill "$s" 2>/dev/null; exit 0' TERM
	while :; do
		sleep "${VBPL_RUN_HEARTBEAT_S:-300}" &
		s=$!
		wait "$s" || true
		kill -0 $$ 2>/dev/null || exit 0
		track -run-heartbeat
	done
}
step() {
	log "$1"
	STEP=$1
	track -run-step "$1"
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
	if [ -n "$TICKER" ]; then
		kill "$TICKER" 2>/dev/null || true
		wait "$TICKER" 2>/dev/null || true
	fi
	if [ -z "$RUN_ID" ]; then
		# tracking was off at the start: record the run now, so a good night
		# is not taken for a missed one
		open_run "${STEP:-prepare}" "{\"schedule\":\"$SCHEDULE\",\"untracked\":true}"
	fi
	if [ "$rc" -eq 0 ]; then
		track -run-finish ok
	else
		# Go keeps the last 4000 bytes on a character boundary
		tail -c 8000 "$STEP_LOG" >"$WORK/.sync.error" 2>/dev/null || true
		[ "$rc" -eq 143 ] && echo "vbpl-sync: stopped (SIGTERM/SIGINT)" >>"$WORK/.sync.error"
		track -run-finish failed -run-error-file "$WORK/.sync.error"
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

open_run prepare "{\"schedule\":\"$SCHEDULE\"}"
[ -n "$RUN_ID" ] || log "run tracking off: $(tail -n 1 "$WORK/.sync.run.err" 2>/dev/null)"
[ -n "$RUN_ID" ] && { ticker 9>&- & TICKER=$!; }

DEST="store/${VBPL_S3_BUCKET:-}/${VBPL_S3_PREFIX:-werag/vbpl}"
prepare() {
	{
		: "${VBPL_S3_ENDPOINT:?VBPL_S3_ENDPOINT is required}"
		: "${VBPL_S3_BUCKET:?VBPL_S3_BUCKET is required}"
		mc alias set store "$VBPL_S3_ENDPOINT" "${VBPL_S3_ACCESS_KEY:?}" "${VBPL_S3_SECRET_KEY:?}" >/dev/null
		if [ ! -f "$WORK/state.sqlite" ] && mc stat "$DEST/state.sqlite" >/dev/null 2>&1; then
			log "restore working copy from $DEST"
			mc mirror --quiet "$DEST" "$WORK"
		fi
	} 2>&1 | tee -a "$STEP_LOG"
}
crawl() {
	python3 -I -u /app/crawl_vbpl.py "$WORK" --summary "$CRAWL_SUMMARY" ${VBPL_CRAWL_ARGS:-} 2>&1 | tee -a "$STEP_LOG"
}
import_catalogue() {
	legal-catalog-import -documents "$WORK/documents.jsonl" -relations "$WORK/relations.jsonl" \
		-snapshot "$(date -u +%F)" -enqueue-no-fulltext -auto-ingest-html -run-id "${RUN_ID:-0}" 2>&1 | tee -a "$STEP_LOG"
}
# --remove also deletes the HTML of documents that turned fully expired;
# it needs delete rights, which append-only (backup) keys lack.
push() {
	local remove=()
	[ "${VBPL_S3_REMOVE:-0}" = "1" ] && remove=(--remove)
	mc mirror --quiet --overwrite "${remove[@]}" \
		--exclude ".sync.*" --exclude "*.tmp" --exclude "*.log" \
		"$WORK" "$DEST" 2>&1 | tee -a "$STEP_LOG"
}

step prepare
run prepare

step crawl
rm -f "$CRAWL_SUMMARY"
run crawl
track -run-summary-key crawl -run-summary-file "$CRAWL_SUMMARY"

step import
log "import into $DB_HOST/$DB_NAME"
run import_catalogue

step push
log "push to $DEST"
run push
touch "$WORK/.sync.ok"
log "done"
