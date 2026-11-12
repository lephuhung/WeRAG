#!/usr/bin/env bash
# Một lần đồng bộ vbpl.vn → danh mục văn bản pháp luật:
#   1. thư mục làm việc trống → khôi phục từ MinIO (state.sqlite, html/…);
#   2. crawl cập nhật thêm (crawl_vbpl.py: chỉ văn bản mới hoặc thay đổi);
#   3. import documents.jsonl / relations.jsonl vào Postgres;
#   4. đẩy thư mục làm việc lên MinIO (mirror; VBPL_S3_REMOVE=1 xoá cả HTML
#      đã gỡ — văn bản chuyển sang hết hiệu lực toàn bộ chỉ còn metadata —
#      cần khoá có quyền xoá, khoá backup chỉ ghi thêm).
set -euo pipefail
WORK=/work
log() { echo "$(date -u +%FT%TZ) vbpl-sync: $*"; }

exec 9>"$WORK/.sync.lock"
flock -n 9 || { log "skip: previous run still going"; exit 0; }
touch "$WORK/.sync.started"

# Run tracking for the SuperAdmin monitor (legal_sync_runs, through
# legal-catalog-import's -run-* flags). It never fails the sync: without a
# run id the sync goes on untracked. Each step's output is kept in
# .sync.step.log; a failed run records its tail. Files .sync.* stay out of
# the MinIO mirror.
STEP_LOG="$WORK/.sync.step.log"
CRAWL_SUMMARY="$WORK/.sync.crawl.json"
RUN_ID=$(legal-catalog-import -run-start -run-worker vbpl_sync -run-step prepare \
	-run-summary "{\"schedule\":\"${VBPL_SYNC_CRON:-}\"}" 2>"$WORK/.sync.run.err") || RUN_ID=""
# only an id: anything else on stdout must never reach -run-id of the import
if ! [[ $RUN_ID =~ ^[0-9]+$ ]]; then
	RUN_ID=""
	log "run tracking off: $(tail -n 1 "$WORK/.sync.run.err" 2>/dev/null)"
fi
track() {
	[ -n "$RUN_ID" ] || return 0
	legal-catalog-import -run-id "$RUN_ID" "$@" >/dev/null 2>&1 || log "run tracking ($*) failed"
}
step() {
	log "$1"
	track -run-step "$1"
	: >"$STEP_LOG"
}
finish() {
	rc=$?
	if [ "$rc" -eq 0 ]; then
		track -run-finish ok
	else
		# Go keeps the last 4000 bytes on a character boundary
		tail -c 8000 "$STEP_LOG" >"$WORK/.sync.error" 2>/dev/null || true
		track -run-finish failed -run-error-file "$WORK/.sync.error"
	fi
}
trap finish EXIT
trap 'exit 143' TERM INT

DEST="store/${VBPL_S3_BUCKET:-}/${VBPL_S3_PREFIX:-werag/vbpl}"
step prepare
{
	: "${VBPL_S3_ENDPOINT:?VBPL_S3_ENDPOINT is required}"
	: "${VBPL_S3_BUCKET:?VBPL_S3_BUCKET is required}"
	mc alias set store "$VBPL_S3_ENDPOINT" "${VBPL_S3_ACCESS_KEY:?}" "${VBPL_S3_SECRET_KEY:?}" >/dev/null
	if [ ! -f "$WORK/state.sqlite" ] && mc stat "$DEST/state.sqlite" >/dev/null 2>&1; then
		log "restore working copy from $DEST"
		mc mirror --quiet "$DEST" "$WORK"
	fi
} 2>&1 | tee -a "$STEP_LOG"

step crawl
rm -f "$CRAWL_SUMMARY"
python3 -I -u /app/crawl_vbpl.py "$WORK" --summary "$CRAWL_SUMMARY" ${VBPL_CRAWL_ARGS:-} 2>&1 | tee -a "$STEP_LOG"
track -run-summary-key crawl -run-summary-file "$CRAWL_SUMMARY"

step import
log "import into $DB_HOST/$DB_NAME"
legal-catalog-import -documents "$WORK/documents.jsonl" -relations "$WORK/relations.jsonl" \
	-snapshot "$(date -u +%F)" -enqueue-no-fulltext -auto-ingest-html -run-id "${RUN_ID:-0}" 2>&1 | tee -a "$STEP_LOG"

# --remove also deletes the HTML of documents that turned fully expired;
# it needs delete rights, which append-only (backup) keys lack.
remove=()
[ "${VBPL_S3_REMOVE:-0}" = "1" ] && remove=(--remove)
step push
log "push to $DEST"
mc mirror --quiet --overwrite "${remove[@]}" \
	--exclude ".sync.*" --exclude "*.tmp" --exclude "*.log" \
	"$WORK" "$DEST" 2>&1 | tee -a "$STEP_LOG"
touch "$WORK/.sync.ok"
log "done"
