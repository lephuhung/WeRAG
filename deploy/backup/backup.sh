#!/usr/bin/env bash
# backup.sh wal | pgbase | postgres | files | objects | neo4j <db> | env | restore-check
#
#   wal            WAL do archive_command chép vào /spool/wal → zstd → age → MinIO, xoá khỏi spool.
#                  Cùng base backup cho phép khôi phục tới một thời điểm (PITR).
#   pgbase         pg_basebackup (tar, kèm WAL cần thiết) → zstd → age → MinIO
#   postgres       pg_dumpall --globals-only + pg_dump -Fc từng DB → age → MinIO (daily/weekly/monthly)
#   files          /data/files: GNU tar incremental (CN hoặc lần đầu: level 0, ngày khác: level 1)
#   objects        bucket file ứng dụng (BACKUP_SRC_S3_*: văn bản gốc + ảnh/tệp sinh ra khi parse của
#                  KB dùng MinIO) → mc mirror về /work/objects → GNU tar incremental như files
#   neo4j <db>     dump Neo4j đọc từ stdin (deploy/backup/neo4j-dump.sh trên host gọi) → age
#   env            các file trong /secrets (.env) → tar|zstd|age (khi đổi, hoặc ≥ 7 ngày)
#   restore-check  pg_dump | pg_restore DB chính vào DB tạm → so bảng/migration, thử truy vấn
#                  vector + BM25; kiểm chuỗi backup objects (sha256 + header age) và đối chiếu
#                  key MinIO mà DB tạm tham chiếu (resources) với danh sách tệp trong chuỗi → xoá DB tạm
#
# Mỗi job ghi status/werag-<job>.json lên MinIO (portal inventory đọc để cảnh báo) và
# /work/state/<job>.ok (healthcheck). Container KHÔNG có private key age.
set -Eeuo pipefail

JOB="${1:?job: wal | pgbase | postgres | files | objects | neo4j | env | restore-check}"
BUCKET="${BACKUP_S3_BUCKET:-inventory-backups}"
PREFIX="${BACKUP_PREFIX:-werag}"
WORK="${BACKUP_WORK:-/work}"
SPOOL="${BACKUP_SPOOL:-/spool/wal}"
STATE="$WORK/state"
TMP="$WORK/tmp"
STARTED=$(date +%s)
OBJECT=""
BYTES=0
SHA=""
NOTE=""
EXTRA='{}'        # trường thêm của status JSON (job objects: số object, dung lượng…)
RESTORE_SCRATCH=""  # DB tạm của restore-check (on_error xoá)
WRITE_STATUS=true

log() { echo "[backup:$JOB] $*"; }

write_status() {  # ok error
  local ok=$1 err=${2:-} f="$TMP/status-$JOB.json"
  jq -n --arg job "$JOB" --argjson ok "$ok" --arg error "$err" --arg object "$OBJECT" \
        --argjson bytes "${BYTES:-0}" --arg sha256 "$SHA" --arg note "$NOTE" \
        --arg host "${BACKUP_HOST_LABEL:-$(hostname)}" --arg finished_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        --argjson duration_s "$(( $(date +%s) - STARTED ))" --argjson extra "$EXTRA" \
        '$extra + {job:$job, ok:$ok, finished_at:$finished_at, duration_s:$duration_s,
          object:$object, bytes:$bytes, sha256:$sha256, note:$note, error:$error, host:$host}' > "$f"
  # Bucket có object lock → PUT phải kèm Content-MD5 (--md5).
  mc cp --quiet --md5 "$f" "backup/$BUCKET/status/$PREFIX-$JOB.json" >/dev/null \
    || log "CẢNH BÁO: không ghi được status lên MinIO"
  rm -f "$f"
}

on_error() {
  local rc=$? line=$1 reason
  trap - ERR
  # Dòng stderr cuối (vd "pg_dump: error: ... password authentication failed") để
  # cảnh báo nói được nguyên nhân thay vì chỉ số dòng.
  reason=$(grep -v '^\s*$' "$ERRLOG" 2>/dev/null | tail -1 | cut -c1-300 || true)
  cat "$ERRLOG" >&3 2>/dev/null || true
  log "THẤT BẠI (dòng $line, mã $rc)"
  write_status false "${reason:-lỗi ở dòng $line} (mã $rc)"
  # Lần chạy thành công kế tiếp phải ghi status ngay để xoá cảnh báo (wal chỉ ghi mỗi giờ).
  rm -f "$STATE/$JOB.status-epoch" 2>/dev/null || true
  rm -rf "$TMP/$JOB".* 2>/dev/null || true
  # restore-check: không để DB tạm (cỡ cả DB chính) nằm lại tới lần chạy sau.
  if [ -n "${RESTORE_SCRATCH:-}" ]; then
    psql -qAt -d postgres -c "DROP DATABASE IF EXISTS \"$RESTORE_SCRATCH\"" >/dev/null 2>&1 || true
  fi
  exit "$rc"
}
trap 'on_error $LINENO' ERR

fail() { echo "$*" >&2; return 1; }

# Đẩy file đã mã hoá lên MinIO rồi tải lại để so sha256 (phát hiện object hỏng/thiếu).
upload_verified() {  # file key
  local file=$1 key=$2 remote
  SHA=$(sha256sum "$file" | cut -d' ' -f1)
  BYTES=$(( BYTES + $(stat -c %s "$file") ))
  mc cp --quiet --md5 "$file" "backup/$BUCKET/$key" >/dev/null
  remote=$(mc cat "backup/$BUCKET/$key" | sha256sum | cut -d' ' -f1)
  [ "$remote" = "$SHA" ] || fail "sha256 trên MinIO không khớp: $key"
  OBJECT="$key"
}

encrypt() {  # out — stdin → age
  age -r "$BACKUP_AGE_RECIPIENT" -o "$1"
}

pg_env() {
  export PGHOST="${PGHOST:-postgres}" PGPORT="${PGPORT:-5432}"
  : "${PGUSER:?thiếu PGUSER}" "${PGPASSWORD:?thiếu PGPASSWORD}" "${PGDATABASE:?thiếu PGDATABASE}"
}

stamp() { date +%Y%m%dT%H%M%S; }

# Bản dump đầy đủ lớn gần bằng dữ liệu → kiểm tra chỗ trống trước khi ghi ra /work/tmp.
need_space() {  # bytes
  local free
  free=$(( $(df -Pk "$TMP" | awk 'NR==2 {print $4}') * 1024 ))
  [ "$free" -gt $(( $1 + 2 * 1024 * 1024 * 1024 )) ] \
    || fail "không đủ chỗ trống ở $TMP: còn $(( free / 1048576 )) MiB, cần ~$(( $1 / 1048576 )) MiB + 2 GiB"
}

archiver_times() {  # → "lần-lỗi-cuối lần-archive-cuối" (epoch, 0 = chưa có)
  psql -qAt -F' ' -d postgres -c \
    "SELECT coalesce(extract(epoch FROM last_failed_time)::bigint, 0),
            coalesce(extract(epoch FROM last_archived_time)::bigint, 0) FROM pg_stat_archiver"
}

job_wal() {
  pg_env
  local mode failed_at archived_at n=0 f dir enc last now every="${BACKUP_WAL_STATUS_EVERY_S:-600}" archiving=ok
  [ -d "$SPOOL" ] || fail "thiếu thư mục spool $SPOOL"
  mode=$(psql -qAt -d postgres -c "SHOW archive_mode")
  [ "$mode" = "on" ] || fail "archive_mode=$mode trên Postgres — PITR không hoạt động (xem deploy/backup/README.md)"
  # archive_command đang lỗi: vẫn đẩy nốt spool, nhưng không ghi status "ok" giữa chừng
  # (status cuối báo lỗi ở dưới).
  read -r failed_at archived_at < <(archiver_times)
  if [ "$failed_at" -gt "$archived_at" ]; then archiving=failed; fi

  # Spool trống (DB không ghi gì → không đổi segment) là bình thường: grep không khớp ≠ lỗi.
  for f in $(cd "$SPOOL" && ls | { grep -E '^[0-9A-F]{24}(\.partial|\.[0-9A-F]{8}\.backup)?$|^[0-9A-F]{8}\.history$' || true; } | LC_ALL=C sort); do
    case "$f" in *.history) dir=history ;; *) dir=${f:0:16} ;; esac
    enc="$TMP/$JOB.$f.zst.age"
    zstd -q -c "$SPOOL/$f" | encrypt "$enc"
    BYTES=$(( BYTES + $(stat -c %s "$enc") ))
    # WAL nhỏ + rất nhiều: tin Content-MD5 (MinIO kiểm tra khi nhận), không tải lại.
    mc cp --quiet --md5 "$enc" "backup/$BUCKET/$PREFIX/wal/$dir/$f.zst.age" >/dev/null
    OBJECT="$PREFIX/wal/$dir/$f.zst.age"
    rm -f "$enc" "$SPOOL/$f"
    n=$(( n + 1 ))
    # Đẩy dồn nhiều WAL (sau sự cố) có thể lâu hơn hạn cảnh báo 15 phút: ghi status giữa chừng
    # (các lần cron trong lúc này bị flock bỏ qua, không ghi gì).
    now=$(date +%s); last=$(cat "$STATE/wal.status-epoch" 2>/dev/null || echo 0)
    if [ "$archiving" = ok ] && [ $(( now - last )) -ge "$every" ]; then
      NOTE="đang đẩy WAL dồn: $n file xong"
      write_status true
      echo "$now" > "$STATE/wal.status-epoch"
    fi
  done

  # archive_command lỗi (spool chưa có, đầy đĩa…) → WAL dồn lại trong pg_wal của server.
  read -r failed_at archived_at < <(archiver_times)
  if [ "$failed_at" -gt "$archived_at" ]; then
    fail "archive_command đang lỗi từ $(date -d "@$failed_at" '+%F %T') — xem log WeKnora-postgres"
  fi
  NOTE="$n file; WAL cuối của server: $( [ "$archived_at" -gt 0 ] && date -d "@$archived_at" '+%F %T' || echo chưa có)"

  # Chạy mỗi phút: chỉ ghi status khi có file mới hoặc ≥ 10 phút chưa ghi — backend cảnh báo
  # WAL quá hạn sau 15 phút (internal/application/service/backup_monitor.go).
  now=$(date +%s); last=$(cat "$STATE/wal.status-epoch" 2>/dev/null || echo 0)
  if [ "$n" -eq 0 ] && [ $(( now - last )) -lt "$every" ]; then
    WRITE_STATUS=false
  else
    echo "$now" > "$STATE/wal.status-epoch"
  fi
}

job_pgbase() {
  pg_env
  local enc="$TMP/$JOB.tar.zst.age" start size
  size=$(psql -qAt -d postgres -c "SELECT sum(pg_database_size(datname))::bigint FROM pg_database")
  need_space "$size"
  # Tên WAL lúc bắt đầu: khi khôi phục cần base này + mọi WAL từ đây trở đi.
  start=$(psql -qAt -d postgres -c "SELECT pg_walfile_name(pg_current_wal_lsn())")
  log "pg_basebackup ($(( size / 1048576 )) MiB, WAL từ $start)"
  pg_basebackup -D - -Ft -X fetch --checkpoint=fast --label "werag-$(stamp)" \
    | zstd -q -T0 -3 | encrypt "$enc"
  NOTE="start_wal=$start db_bytes=$size"
  upload_verified "$enc" "$PREFIX/pgbase/$(date +%Y/%m)/base-$(stamp)-$start.tar.zst.age"
  rm -f "$enc"
}

job_postgres() {
  pg_env
  # Bản đầu tiên trong ngày (giờ < 6): mùng 1 → monthly, Chủ nhật → weekly.
  local tier=daily hour ts dbs db dump enc dir n=0
  hour=$(date +%-H)
  if [ "$hour" -lt 6 ] && [ "$(date +%d)" = "01" ]; then tier=monthly
  elif [ "$hour" -lt 6 ] && [ "$(date +%u)" = "7" ]; then tier=weekly; fi
  ts=$(stamp); dir="$PREFIX/postgres/$tier/$(date +%Y/%m)"

  enc="$TMP/$JOB.globals.sql.age"
  pg_dumpall --globals-only | encrypt "$enc"
  upload_verified "$enc" "$dir/globals-$ts.sql.age"
  rm -f "$enc"

  dbs=$(psql -qAt -d postgres -c "SELECT datname FROM pg_database
          WHERE datallowconn AND NOT datistemplate AND datname !~ '_restore_check$' ORDER BY 1")
  for db in $dbs; do
    dump="$TMP/$JOB.dump"; enc="$dump.age"
    need_space "$(psql -qAt -d postgres -c "SELECT pg_database_size('$db')")"
    log "pg_dump $db ($tier)"
    pg_dump -Fc -Z 6 --no-owner -d "$db" -f "$dump"
    pg_restore --list "$dump" >/dev/null           # archive đọc được, TOC hợp lệ
    encrypt "$enc" < "$dump"
    rm -f "$dump"
    upload_verified "$enc" "$dir/$db-$ts.dump.age"
    rm -f "$enc"
    n=$(( n + 1 ))
  done
  OBJECT="$dir/"
  NOTE="tier=$tier databases=$n (+ globals)"
}

job_files() {
  local src=/data/files snar0="$STATE/files.l0.snar" snar="$TMP/$JOB.snar" level enc
  [ -d "$src" ] || { NOTE="không mount $src — bỏ qua"; log "$NOTE"; return 0; }
  if [ ! -s "$snar0" ] || [ "$(date +%u)" = "7" ]; then level=0; rm -f "$snar"
  else level=1; cp "$snar0" "$snar"; fi
  need_space "$(du -sb "$src" | cut -f1)"
  enc="$TMP/$JOB.tar.zst.age"
  # --no-check-device: số device của volume đổi sau mỗi lần tạo lại container.
  # tar thoát 1 = file đổi trong lúc đọc (upload đang ghi) → vẫn dùng được.
  { tar -C "$src" --no-check-device -g "$snar" -cf - . || [ $? -eq 1 ]; } \
    | zstd -q -T0 -10 | encrypt "$enc"
  upload_verified "$enc" "$PREFIX/files/$(date +%Y/%m)/files-$(stamp)-L$level.tar.zst.age"
  rm -f "$enc"
  # Level 1 luôn so với bản level 0 gần nhất → chỉ cập nhật snapshot sau level 0 thành công.
  if [ "$level" = 0 ]; then mv "$snar" "$snar0"; else rm -f "$snar"; fi
  NOTE="level=$level"
}

# Chuỗi objects hiện tại (từ level 0 gần nhất): mỗi dòng "key<TAB>bytes<TAB>sha256<TAB>epoch-bắt-đầu";
# objects-index/ giữ danh sách tệp (tar --index-file) của từng archive trong chuỗi, để
# restore-check đối chiếu với DB mà không cần private key.
OBJECTS_STAGE="$WORK/objects"
OBJECTS_CHAIN="$STATE/objects.chain.tsv"
OBJECTS_INDEX="$STATE/objects-index"

# Lỗi mc mirror --json nằm trên stdout (file log), không ở stderr.
mirror_error() {  # log
  { jq -r 'select(.status=="error") | (.error.message // "") + " " + (.error.cause.message // "")' "$1" 2>/dev/null || true; } \
    | grep -v '^\s*$' | tail -1 | cut -c1-200 || true
}

# Alias "src" của bucket nguồn, không để khoá trong argv: khoá chỉ gồm ký tự an toàn trong URL
# → MC_HOST_src (mc không giải mã %xx nên truyền nguyên); có @ / : % … → file cấu hình mc
# riêng (0700, xoá khi xong/khi lỗi) chỉ chứa alias src.
SRC_MC_DIR=""
src_alias() {  # endpoint access secret
  local ep=${1%/} scheme=http
  case "$ep" in
    https://*) scheme=https; ep=${ep#https://} ;;
    http://*) ep=${ep#http://} ;;
  esac
  if [[ "$2$3" =~ ^[A-Za-z0-9._~+=-]+$ ]]; then
    export MC_HOST_src="$scheme://$2:$3@$ep"
  else
    SRC_MC_DIR="$TMP/$JOB.mcsrc"
    rm -rf "$SRC_MC_DIR"; (umask 077; mkdir -p "$SRC_MC_DIR")
    (umask 077; jq -n --arg url "$scheme://$ep" --arg a "$2" --arg s "$3" \
      '{version: "10", aliases: {src: {url: $url, accessKey: $a, secretKey: $s, api: "s3v4", path: "auto"}}}' \
      > "$SRC_MC_DIR/config.json")
  fi
}

src_mc() {  # lệnh-mc args… — mc với alias src (lệnh con đứng đầu)
  local sub=$1; shift
  # (cờ --config-dir sau lệnh con không thắng MC_CONFIG_DIR của image → đặt biến môi trường)
  if [ -n "$SRC_MC_DIR" ]; then MC_CONFIG_DIR="$SRC_MC_DIR" mc "$sub" "$@"; else mc "$sub" "$@"; fi
}

# source_keys bucket → mỗi dòng "byte<TAB>key" (object hiện hành, bỏ key kiểu thư mục "…/").
source_keys() {
  src_mc ls --recursive --json "src/$1" \
    | jq -r 'select(.type == "file" and ((.key // "") | endswith("/") | not)) | "\(.size // 0)\t\(.key)"'
}

# source_counts bucket → "số-object tổng-byte".
source_counts() {
  source_keys "$1" | awk -F'\t' '{ n++; b += $1 } END { printf "%d %d\n", n, b }'
}

# mirror_once bucket stage log → cộng dồn COPIED/REMOVED; lỗi (mã ≠ 0 hoặc dòng lỗi) → fail.
mirror_once() {
  local bucket=$1 stage=$2 mlog=$3 errors c r
  if ! src_mc mirror --overwrite --remove --retry --json "src/$bucket" "$stage" > "$mlog"; then
    fail "mc mirror từ $bucket lỗi: $(mirror_error "$mlog")"
  fi
  errors=$({ jq -r 'select(.status=="error") | 1' "$mlog" 2>/dev/null || true; } | wc -l)
  [ "$errors" -eq 0 ] || fail "mc mirror từ $bucket báo $errors lỗi: $(mirror_error "$mlog")"
  c=$({ jq -r 'select(.status=="success" and has("target") and (.source // "") != "") | 1' "$mlog" 2>/dev/null || true; } | wc -l)
  r=$({ jq -r 'select(.status=="success" and has("target") and (.source // "") == "") | 1' "$mlog" 2>/dev/null || true; } | wc -l)
  COPIED=$(( COPIED + c )); REMOVED=$(( REMOVED + r ))
  rm -f "$mlog"
}

# missing_old bucket stage since → MISSING_OLD = số key nguồn thiếu trong bản sao mà tạo trước
# `since` (epoch); key mới hơn (đang nhập văn bản lúc chạy) không tính. MISSING_FIRST = key đầu.
missing_old() {
  local bucket=$1 stage=$2 since=$3 key lm at n=0 total
  source_keys "$bucket" | cut -f2- | LC_ALL=C sort > "$TMP/$JOB.srckeys"
  (cd "$stage" && find . -type f | sed 's#^\./##' | LC_ALL=C sort) > "$TMP/$JOB.stagekeys"
  LC_ALL=C comm -23 "$TMP/$JOB.srckeys" "$TMP/$JOB.stagekeys" > "$TMP/$JOB.missing"
  total=$(wc -l < "$TMP/$JOB.missing")
  MISSING_FIRST=""; MISSING_OLD=0
  if [ "$total" -gt 200 ]; then  # quá nhiều để hỏi từng key: coi như thiếu thật
    MISSING_FIRST=$(head -1 "$TMP/$JOB.missing"); MISSING_OLD=$total; return 0
  fi
  while IFS= read -r -u 4 key; do
    lm=$(src_mc stat --json "src/$bucket/$key" | jq -r '.lastModified // empty')
    at=$(date -d "$lm" +%s 2>/dev/null || echo 0)
    if [ "$at" -lt "$since" ]; then n=$(( n + 1 )); [ -n "$MISSING_FIRST" ] || MISSING_FIRST=$key; fi
  done 4< "$TMP/$JOB.missing"
  rm -f "$TMP/$JOB.srckeys" "$TMP/$JOB.stagekeys" "$TMP/$JOB.missing"
  MISSING_OLD=$n
}

job_objects() {
  local src_vars="${BACKUP_SRC_S3_ENDPOINT:-}${BACKUP_SRC_S3_BUCKET:-}${BACKUP_SRC_S3_ACCESS_KEY:-}${BACKUP_SRC_S3_SECRET_KEY:-}"
  local bucket="${BACKUP_SRC_S3_BUCKET:-}" stage="$OBJECTS_STAGE" snar0="$STATE/objects.l0.snar" snar="$TMP/$JOB.snar"
  local level enc mlog idx key started n_obj n_bytes copied removed src_count src_size have have_bytes drop out counts
  local mstart attempt drift=""
  # Bản cài không dùng MinIO cho file ứng dụng: không cấu hình gì → bỏ qua, không báo lỗi.
  if [ -z "$src_vars" ]; then NOTE="chưa cấu hình BACKUP_SRC_S3_* — bỏ qua"; log "$NOTE"; return 0; fi
  if [ -z "${BACKUP_SRC_S3_ENDPOINT:-}" ] || [ -z "$bucket" ] || [ -z "${BACKUP_SRC_S3_ACCESS_KEY:-}" ] \
     || [ -z "${BACKUP_SRC_S3_SECRET_KEY:-}" ]; then
    fail "BACKUP_SRC_S3_* thiếu biến (cần ENDPOINT, BUCKET, ACCESS_KEY, SECRET_KEY)"
  fi
  [[ "$bucket" =~ ^[a-z0-9][a-z0-9.-]{1,62}$ ]] || fail "BACKUP_SRC_S3_BUCKET không hợp lệ: $bucket"
  src_alias "$BACKUP_SRC_S3_ENDPOINT" "$BACKUP_SRC_S3_ACCESS_KEY" "$BACKUP_SRC_S3_SECRET_KEY"
  # Giữ câu lỗi của mc (không chứa khoá) trong status.
  if ! out=$(src_mc ls "src/$bucket" 2>&1 >/dev/null); then
    fail "không đọc được bucket nguồn $bucket ở $BACKUP_SRC_S3_ENDPOINT: $(echo "${out##*<ERROR> }" | tail -1 | cut -c1-300)"
  fi

  mkdir -p "$stage"
  counts=$(source_counts "$bucket"); read -r src_count src_size <<<"$counts"
  have=$(find "$stage" -type f | wc -l)
  # Nguồn sai/rỗng (đổi endpoint, bucket mới, lỗi quyền liệt kê…) + --remove = xoá sạch bản sao
  # mà vẫn báo ok. Bucket giảm bất thường so với bản sao → dừng, không mirror.
  if [ "${BACKUP_OBJECTS_ALLOW_SHRINK:-0}" != 1 ] && [ "$have" -gt 0 ]; then
    drop=$(( have - src_count ))
    if [ "$src_count" -eq 0 ] || [ "$drop" -gt 500 ] || [ $(( drop * 100 )) -gt $(( have * 20 )) ]; then
      fail "bucket nguồn $bucket có $src_count object, bản sao có $have — giảm bất thường, không mirror --remove (đúng là đã xoá thì chạy với BACKUP_OBJECTS_ALLOW_SHRINK=1)"
    fi
  fi
  # Chỗ trống: phần bucket chưa có trong bản sao + archive (≈ cả bucket, ảnh/PDF nén ít).
  have_bytes=$(du -sb "$stage" | cut -f1)
  need_space $(( src_size > have_bytes ? 2 * src_size - have_bytes : src_size ))
  # Bản sao cục bộ của bucket: chỉ tải object mới/đổi, --remove xoá tệp mà bucket đã xoá.
  # Tệp không đổi giữ nguyên mtime → tar level 1 chỉ chứa phần thay đổi.
  mlog="$TMP/$JOB.mirror.jsonl"
  COPIED=0; REMOVED=0
  mstart=$(date +%s)
  mirror_once "$bucket" "$stage" "$mlog"
  # Key S3 kiểu symlink/thiết bị không thể có; gặp thì dừng thay vì đóng gói thứ lạ.
  [ -z "$(find "$stage" ! -type f ! -type d -print -quit)" ] || fail "$stage có mục không phải tệp/thư mục"
  # --remove xoá tệp nhưng để lại thư mục rỗng (S3 không có thư mục) → dọn cho khớp bucket.
  find "$stage" -mindepth 1 -type d -empty -delete
  # Bản sao phải đủ từng object: key "x" cùng "x/con" (tệp trùng tên thư mục) bị mc bỏ qua im lặng.
  # Đêm có thể đang nhập văn bản → bucket đổi trong lúc chạy: mirror lại tối đa 2 lần, rồi chỉ
  # coi là lỗi khi thiếu key đã có từ trước lần mirror cuối.
  for attempt in 1 2 3; do
    n_obj=$(find "$stage" -type f | wc -l)
    counts=$(source_counts "$bucket"); read -r src_count src_size <<<"$counts"
    [ "$n_obj" -ne "$src_count" ] || break
    [ "$attempt" -lt 3 ] || break
    log "bản sao $n_obj tệp ≠ nguồn $src_count object — mirror lại"
    mstart=$(date +%s)
    mirror_once "$bucket" "$stage" "$mlog"
  done
  if [ "$n_obj" -ne "$src_count" ]; then
    missing_old "$bucket" "$stage" "$mstart"
    if [ "$MISSING_OLD" -gt 0 ]; then
      fail "mirror thiếu $MISSING_OLD object đã có trước lúc chạy (bản sao $n_obj tệp, bucket nguồn $bucket $src_count object; vd $MISSING_FIRST) — key trùng tên thư mục?"
    fi
    drift="; bucket đổi trong lúc chạy: nguồn $src_count object, bản sao $n_obj tệp (key mới sẽ vào lần sau)"
  fi
  copied=$COPIED; removed=$REMOVED
  n_bytes=$(du -sb "$stage" | cut -f1)

  if [ ! -s "$snar0" ] || [ ! -s "$OBJECTS_CHAIN" ] || [ "$(date +%u)" = "7" ]; then level=0; rm -f "$snar"
  else level=1; cp "$snar0" "$snar"; fi
  enc="$TMP/$JOB.tar.zst.age"; idx="$TMP/$JOB.index"
  started=$(date +%s)
  tar -C "$stage" --no-check-device --quoting-style=literal -g "$snar" -v --index-file="$idx" -cf - . \
    | zstd -q -T0 -6 | encrypt "$enc"
  key="$PREFIX/objects/$(date +%Y/%m)/objects-$(stamp)-L$level.tar.zst.age"
  upload_verified "$enc" "$key"
  rm -f "$enc"

  # Level 1 luôn so với level 0 → chỉ thay snapshot + bắt đầu chuỗi mới sau level 0 thành công.
  if [ "$level" = 0 ]; then
    mv "$snar" "$snar0"
    rm -rf "$OBJECTS_INDEX"; : > "$OBJECTS_CHAIN"
  else
    rm -f "$snar"
  fi
  mkdir -p "$OBJECTS_INDEX"
  gzip -c "$idx" > "$OBJECTS_INDEX/$(basename "$key" .tar.zst.age).idx.gz"
  rm -f "$idx"
  printf '%s\t%s\t%s\t%s\n' "$key" "$BYTES" "$SHA" "$started" >> "$OBJECTS_CHAIN"

  NOTE="level=$level objects=$n_obj source_bytes=$n_bytes copied=$copied removed=$removed$drift"
  if [ -n "$SRC_MC_DIR" ]; then rm -rf "$SRC_MC_DIR"; fi
  EXTRA=$(jq -nc --argjson level "$level" --argjson objects "$n_obj" --argjson source_bytes "$n_bytes" \
    --argjson copied "$copied" --argjson removed "$removed" --arg bucket "$bucket" \
    '{level:$level, objects:$objects, source_bytes:$source_bytes, copied:$copied, removed:$removed, source_bucket:$bucket}')
}

job_neo4j() {  # db — dump đọc từ stdin
  local db=${1:?tên database Neo4j} enc="$TMP/$JOB.dump.age"
  [ "$db" != "dump-failed" ] || fail "neo4j-admin dump lỗi trên host (xem journalctl -u werag-neo4j-backup)"
  [[ "$db" =~ ^[a-z0-9.-]+$ ]] || fail "tên database Neo4j không hợp lệ: $db"
  [ ! -t 0 ] || fail "neo4j: cần dump ở stdin (chạy deploy/backup/neo4j-dump.sh trên host)"
  encrypt "$enc"                                   # dump Neo4j đã nén sẵn
  [ "$(stat -c %s "$enc")" -gt 1024 ] || fail "dump Neo4j $db rỗng"
  upload_verified "$enc" "$PREFIX/neo4j/$(date +%Y/%m)/$db-$(stamp).dump.age"
  rm -f "$enc"
  NOTE="db=$db"
}

job_env() {
  local files digest last_digest last_epoch now arc
  [ -d /secrets ] || { log "không có /secrets — bỏ qua"; NOTE="không mount /secrets"; return 0; }
  files=$(cd /secrets && find . -type f | LC_ALL=C sort)
  [ -n "$files" ] || { log "/secrets trống — bỏ qua"; NOTE="/secrets trống"; return 0; }
  digest=$(cd /secrets && echo "$files" | xargs sha256sum | sha256sum | cut -d' ' -f1)
  last_digest=$(cat "$STATE/env.digest" 2>/dev/null || true)
  last_epoch=$(cat "$STATE/env.epoch" 2>/dev/null || echo 0)
  now=$(date +%s)
  if [ "$digest" = "$last_digest" ] && [ $(( now - last_epoch )) -lt $(( 7 * 86400 )) ]; then
    NOTE="không đổi từ lần trước — bỏ qua"
    OBJECT=$(cat "$STATE/env.object" 2>/dev/null || true)
    log "$NOTE"
    return 0
  fi
  arc="$TMP/$JOB.tar.zst.age"
  (cd /secrets && echo "$files" | tar -cf - -T - | zstd -q -19 | encrypt "$arc")
  upload_verified "$arc" "$PREFIX/env/$(date +%Y/%m)/secrets-$(stamp).tar.zst.age"
  rm -f "$arc"
  NOTE="$(echo "$files" | wc -l) file"
  echo "$digest" > "$STATE/env.digest"; echo "$now" > "$STATE/env.epoch"; echo "$OBJECT" > "$STATE/env.object"
}

# Chuỗi backup objects (container không có private key → không giải mã được, phần đó là
# diễn tập tay: restore.sh objects + objects-check):
#  1. mọi archive của chuỗi còn trên MinIO, đúng sha256 lúc đẩy, mở đầu bằng header age;
#  2. key MinIO mà DB khôi phục tham chiếu (resources đang dùng, tạo trước lần đóng gói cuối)
#     có trong danh sách tệp của chuỗi. Key thiếu cả trong bản sao bucket = bucket nguồn đã mất
#     object (chỉ ghi chú); có trong bản sao mà không có trong chuỗi = backup thiếu (lỗi).
objects_configured() { [ -n "${BACKUP_SRC_S3_BUCKET:-}" ]; }

objects_db_refs() {  # db file-ra — key (sort -u) mà DB tham chiếu trong bucket nguồn
  local db=$1 out=$2 bucket=${BACKUP_SRC_S3_BUCKET:-} cutoff
  [[ "$bucket" =~ ^[a-z0-9][a-z0-9.-]{1,62}$ ]] || fail "BACKUP_SRC_S3_BUCKET không hợp lệ: $bucket"
  [ -s "$OBJECTS_CHAIN" ] || fail "objects: chưa có chuỗi backup nào (chạy backup.sh objects)"
  cutoff=$(tail -1 "$OBJECTS_CHAIN" | cut -f4)
  [[ "$cutoff" =~ ^[0-9]+$ ]] || fail "objects: $OBJECTS_CHAIN hỏng"
  if [ -z "$(psql -qAt -d "$db" -c "SELECT to_regclass('public.resources')")" ]; then
    : > "$out"; return 0
  fi
  psql -qAt -d "$db" -c "SELECT substring(physical_path FROM '/minio://$bucket/(.*)\$') FROM resources
      WHERE provider = 'minio' AND deleted_at IS NULL AND state = 'active'
        AND physical_path LIKE '%/minio://$bucket/%' AND created_at < to_timestamp($cutoff)" \
    | LC_ALL=C sort -u > "$out"
}

check_objects() {  # refs-file → OBJ_NOTE
  local refs=$1 idx="$TMP/$JOB.objects.idx" miss="$TMP/$JOB.objects.missing" f key size sha started
  local n=0 remote hdr total lost=0 src_missing=0 first="" first_src="" max_n pct limit
  while IFS=$'\t' read -r -u 4 key size sha started; do
    [ -n "$key" ] || continue
    remote=$(mc cat "backup/$BUCKET/$key" | sha256sum | cut -d' ' -f1)
    [ "$remote" = "$sha" ] || fail "objects: $key trên MinIO khác bản đã đẩy (sha256)"
    hdr=$(mc head -n 1 "backup/$BUCKET/$key")
    [ "$hdr" = "age-encryption.org/v1" ] || fail "objects: $key không phải tệp age"
    n=$(( n + 1 ))
  done 4< "$OBJECTS_CHAIN"
  { for f in "$OBJECTS_INDEX"/*.idx.gz; do if [ -e "$f" ]; then gzip -dc "$f"; fi; done; } \
    | sed 's#^\./##' | LC_ALL=C sort -u > "$idx"
  LC_ALL=C comm -23 "$refs" "$idx" > "$miss"
  total=$(wc -l < "$refs")
  while IFS= read -r key; do
    if [ -f "$OBJECTS_STAGE/$key" ]; then
      lost=$(( lost + 1 )); [ -n "$first" ] || first=$key
    else
      src_missing=$(( src_missing + 1 )); [ -n "$first_src" ] || first_src=$key
    fi
  done < "$miss"
  rm -f "$idx" "$miss"
  OBJ_NOTE="objects: chain=$n archive ok, db_refs=$total missing_in_backup=$lost missing_in_source=$src_missing"
  [ "$lost" -eq 0 ] || fail "objects: $lost tệp DB tham chiếu có trong bản sao bucket nhưng thiếu trong chuỗi backup (vd $first)"
  # Vài object mất lẻ tẻ ở nguồn chỉ ghi chú; nhiều hơn max(BACKUP_OBJECTS_MAX_MISSING (10),
  # BACKUP_OBJECTS_MAX_MISSING_PCT (1) % số tệp DB tham chiếu) = bucket nguồn hỏng/sai → lỗi.
  max_n=${BACKUP_OBJECTS_MAX_MISSING:-10}; pct=${BACKUP_OBJECTS_MAX_MISSING_PCT:-1}
  [[ "$max_n" =~ ^[0-9]+$ ]] || max_n=10
  [[ "$pct" =~ ^[0-9]+$ ]] || pct=1
  limit=$(( total * pct / 100 )); [ "$limit" -ge "$max_n" ] || limit=$max_n
  if [ "$src_missing" -gt "$limit" ]; then
    fail "objects: $src_missing/$total tệp DB tham chiếu không còn trong bucket nguồn (vd $first_src)"
  fi
}

job_restore_check() {
  pg_env
  local scratch="${PGDATABASE}_restore_check" src_tables dst_tables src_rev dst_rev dim vec bm25 emb
  local refs="$TMP/$JOB.objects.refs" OBJ_NOTE="objects: chưa cấu hình BACKUP_SRC_S3_*"
  local q_tables="SELECT count(*) FROM pg_tables WHERE schemaname='public'"
  local q_rev="SELECT version || CASE WHEN dirty THEN ' dirty' ELSE '' END FROM schema_migrations"
  need_space "$(psql -qAt -d postgres -c "SELECT pg_database_size('$PGDATABASE')")"
  RESTORE_SCRATCH=$scratch
  psql -qAt -d postgres -c "DROP DATABASE IF EXISTS \"$scratch\"" -c "CREATE DATABASE \"$scratch\" TEMPLATE template0"
  # Khôi phục dựng lại toàn bộ index HNSW + BM25 → thời gian job ≈ thời gian khôi phục logic thật.
  pg_dump -Fc -d "$PGDATABASE" | pg_restore --no-owner --no-privileges --exit-on-error -d "$scratch"
  src_tables=$(psql -qAt -c "$q_tables"); dst_tables=$(psql -qAt -d "$scratch" -c "$q_tables")
  src_rev=$(psql -qAt -c "$q_rev");       dst_rev=$(psql -qAt -d "$scratch" -c "$q_rev")
  emb=$(psql -qAt -d "$scratch" -c "SELECT count(*) FROM embeddings")
  vec=0; bm25=0
  dim=$(psql -qAt -d "$scratch" -c "SELECT dimension FROM embeddings LIMIT 1")
  if [ -n "$dim" ]; then
    vec=$(psql -qAt -d "$scratch" -c "SELECT count(*) FROM (SELECT id FROM embeddings WHERE dimension=$dim
            ORDER BY embedding::halfvec($dim) <=> (SELECT embedding::halfvec($dim) FROM embeddings
            WHERE dimension=$dim LIMIT 1) LIMIT 5) t")
    bm25=$(psql -qAt -d "$scratch" -c "SELECT count(*) FROM (SELECT id FROM embeddings
            WHERE id @@@ paradedb.all() LIMIT 5) t")
  fi
  if objects_configured; then objects_db_refs "$scratch" "$refs"; fi
  psql -qAt -d postgres -c "DROP DATABASE \"$scratch\""
  RESTORE_SCRATCH=""
  NOTE="tables=$dst_tables/$src_tables migration=$dst_rev embeddings=$emb vector_hits=$vec bm25_hits=$bm25"
  if [ "$src_tables" != "$dst_tables" ] || [ "$src_rev" != "$dst_rev" ]; then
    fail "khôi phục không khớp: $NOTE"
  fi
  if [ "${emb:-0}" -gt 0 ] && { [ "$vec" -eq 0 ] || [ "$bm25" -eq 0 ]; }; then
    fail "truy vấn vector/BM25 trên bản khôi phục không trả kết quả: $NOTE"
  fi
  if objects_configured; then check_objects "$refs"; rm -f "$refs"; fi
  NOTE="$NOTE; $OBJ_NOTE"
}

# deploy/backup/test/objects-test.sh source file này để gọi từng hàm.
[ "${BASH_SOURCE[0]}" = "$0" ] || return 0

mkdir -p "$STATE" "$TMP"
ERRLOG="$TMP/$JOB.stderr"
exec 3>&2 2>"$ERRLOG"     # stderr vào file (đồng bộ) → đọc được nguyên nhân khi lỗi
# Một tiến trình mỗi job: job chạy nền lúc khởi động có thể trùng giờ cron (00:15, 01:00,
# 02:30…), lần chạy tay trùng lần theo lịch — dùng chung tệp tạm/snapshot. Đang chạy → bỏ qua.
case "$JOB" in wal|pgbase|postgres|files|objects|neo4j|env|restore-check) ;; *) echo "job không hợp lệ: $JOB" >&2; exit 2 ;; esac
exec 7>"$STATE/$JOB.lock"
if ! flock -n 7; then
  log "đang chạy ở tiến trình khác — bỏ qua"
  exit 0
fi
mc alias set backup "$BACKUP_S3_ENDPOINT" "$BACKUP_S3_ACCESS_KEY" "$BACKUP_S3_SECRET_KEY" >/dev/null
case "$JOB" in
  wal) job_wal ;;
  pgbase) job_pgbase ;;
  postgres) job_postgres ;;
  files) job_files ;;
  objects) job_objects ;;
  neo4j) job_neo4j "${2:-}" ;;
  env) job_env ;;
  restore-check) job_restore_check ;;
  *) echo "job không hợp lệ: $JOB" >&2; exit 2 ;;
esac
cat "$ERRLOG" >&3 2>/dev/null || true
if [ "$WRITE_STATUS" = true ]; then
  write_status true
  log "xong ${OBJECT:+→ $OBJECT }(${BYTES} byte) ${NOTE}"
fi
touch "$STATE/$JOB.ok"
