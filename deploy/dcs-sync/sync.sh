#!/usr/bin/env bash
# Một lần đồng bộ văn bản của Đảng (scripts/dangcongsan):
#   1. tải lại DCS_REFRESH_PAGES trang danh sách đầu của mỗi cổng và trang
#      chi tiết của chúng (văn bản mới, tệp gắn thêm sau); trang sâu hơn và
#      tệp đã có lấy từ cache;
#   2. phân loại tệp, gộp hai cổng, dựng HTML (dcs_classify, dcs_build);
#   3. đẩy phần chưa đẩy (dcs_push, sổ push-results.jsonl): văn bản có chữ →
#      toàn văn vào kho "Văn bản của Đảng"; bản scan / không có tệp → dòng
#      danh mục + đề nghị trong hàng chờ của extension TVPL.
set -euo pipefail
WORK=${DCS_WORK_DIR:?DCS_WORK_DIR is required}
log() { echo "$(date -u +%FT%TZ) dcs-sync: $*"; }

exec 9>"$WORK/.sync.lock"
flock -n 9 || { log "skip: previous run still going"; exit 0; }
touch "$WORK/.sync.started"
: "${WERAG_API_KEY:?WERAG_API_KEY is required (API key of organization 10000 with ingest)}"

PY="python3 -I -u"
PAGES=${DCS_REFRESH_PAGES:-3}
DELAY=${DCS_DELAY:-1}
log "fetch (first $PAGES listing pages again)"
$PY /app/dcs_fetch.py --site tulieu --cache-dir "$WORK/cache" --out-dir "$WORK/run-tulieu" --delay "$DELAY" --refresh-pages "$PAGES"
$PY /app/dcs_fetch.py --site org --cache-dir "$WORK/cache" --out-dir "$WORK/run-org" --delay "$DELAY" --refresh-pages "$PAGES"

log "classify + build"
$PY /app/dcs_classify.py "$WORK/run-tulieu/records.jsonl" "$WORK/run-tulieu/classified.jsonl"
$PY /app/dcs_classify.py "$WORK/run-org/records.jsonl" "$WORK/run-org/classified.jsonl"
$PY /app/dcs_build.py "$WORK/run-tulieu/classified.jsonl" "$WORK/run-org/classified.jsonl" --out-dir "$WORK/build"

log "push to ${WERAG_API:-default}"
$PY /app/dcs_push.py "$WORK/build" | tee -a "$WORK/push.log"
touch "$WORK/.sync.ok"
log "done"
