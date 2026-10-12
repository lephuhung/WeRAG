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

: "${VBPL_S3_ENDPOINT:?VBPL_S3_ENDPOINT is required}"
: "${VBPL_S3_BUCKET:?VBPL_S3_BUCKET is required}"
DEST="store/$VBPL_S3_BUCKET/${VBPL_S3_PREFIX:-werag/vbpl}"
mc alias set store "$VBPL_S3_ENDPOINT" "${VBPL_S3_ACCESS_KEY:?}" "${VBPL_S3_SECRET_KEY:?}" >/dev/null

if [ ! -f "$WORK/state.sqlite" ] && mc stat "$DEST/state.sqlite" >/dev/null 2>&1; then
	log "restore working copy from $DEST"
	mc mirror --quiet "$DEST" "$WORK"
fi

log "crawl"
python3 -I -u /app/crawl_vbpl.py "$WORK" ${VBPL_CRAWL_ARGS:-}

log "import into $DB_HOST/$DB_NAME"
legal-catalog-import -documents "$WORK/documents.jsonl" -relations "$WORK/relations.jsonl" \
	-snapshot "$(date -u +%F)"

# --remove also deletes the HTML of documents that turned fully expired;
# it needs delete rights, which append-only (backup) keys lack.
remove=()
[ "${VBPL_S3_REMOVE:-0}" = "1" ] && remove=(--remove)
log "push to $DEST"
mc mirror --quiet --overwrite "${remove[@]}" \
	--exclude ".sync.*" --exclude "*.tmp" --exclude "*.log" \
	"$WORK" "$DEST"
touch "$WORK/.sync.ok"
log "done"
