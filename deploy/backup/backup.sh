#!/usr/bin/env bash
# backup.sh wal | pgbase | postgres | files | neo4j <db> | env | restore-check
#
#   wal            WAL do archive_command chép vào /spool/wal → zstd → age → MinIO, xoá khỏi spool.
#                  Cùng base backup cho phép khôi phục tới một thời điểm (PITR).
#   pgbase         pg_basebackup (tar, kèm WAL cần thiết) → zstd → age → MinIO
#   postgres       pg_dumpall --globals-only + pg_dump -Fc từng DB → age → MinIO (daily/weekly/monthly)
#   files          /data/files: GNU tar incremental (CN hoặc lần đầu: level 0, ngày khác: level 1)
#   neo4j <db>     dump Neo4j đọc từ stdin (deploy/backup/neo4j-dump.sh trên host gọi) → age
#   env            các file trong /secrets (.env) → tar|zstd|age (khi đổi, hoặc ≥ 7 ngày)
#   restore-check  pg_dump | pg_restore DB chính vào DB tạm → so bảng/migration, thử truy vấn
#                  vector + BM25 → xoá DB tạm
#
# Mỗi job ghi status/werag-<job>.json lên MinIO (portal inventory đọc để cảnh báo) và
# /work/state/<job>.ok (healthcheck). Container KHÔNG có private key age.
set -Eeuo pipefail

JOB="${1:?job: wal | pgbase | postgres | files | neo4j | env | restore-check}"
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
WRITE_STATUS=true

log() { echo "[backup:$JOB] $*"; }

write_status() {  # ok error
  local ok=$1 err=${2:-} f="$TMP/status-$JOB.json"
  jq -n --arg job "$JOB" --argjson ok "$ok" --arg error "$err" --arg object "$OBJECT" \
        --argjson bytes "${BYTES:-0}" --arg sha256 "$SHA" --arg note "$NOTE" \
        --arg host "${BACKUP_HOST_LABEL:-$(hostname)}" --arg finished_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        --argjson duration_s "$(( $(date +%s) - STARTED ))" \
        '{job:$job, ok:$ok, finished_at:$finished_at, duration_s:$duration_s,
          object:$object, bytes:$bytes, sha256:$sha256, note:$note, error:$error, host:$host}' > "$f"
  # Bucket có object lock → PUT phải kèm Content-MD5 (--md5).
  mc cp --quiet --md5 "$f" "backup/$BUCKET/status/$PREFIX-$JOB.json" >/dev/null \
    || log "CẢNH BÁO: không ghi được status lên MinIO"
  rm -f "$f"
}

on_error() {
  local rc=$? line=$1 reason
  # Dòng stderr cuối (vd "pg_dump: error: ... password authentication failed") để
  # cảnh báo nói được nguyên nhân thay vì chỉ số dòng.
  reason=$(grep -v '^\s*$' "$ERRLOG" 2>/dev/null | tail -1 | cut -c1-300 || true)
  cat "$ERRLOG" >&3 2>/dev/null || true
  log "THẤT BẠI (dòng $line, mã $rc)"
  write_status false "${reason:-lỗi ở dòng $line} (mã $rc)"
  # Lần chạy thành công kế tiếp phải ghi status ngay để xoá cảnh báo (wal chỉ ghi mỗi giờ).
  rm -f "$STATE/$JOB.status-epoch" 2>/dev/null || true
  rm -f "$TMP/$JOB".* 2>/dev/null || true
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

job_wal() {
  pg_env
  local mode failed_at archived_at n=0 f dir enc last now
  [ -d "$SPOOL" ] || fail "thiếu thư mục spool $SPOOL"
  # Lần chạy theo lịch và lần chạy tay không được cùng đẩy/xoá một file WAL.
  exec 9>"$STATE/wal.lock"
  if ! flock -n 9; then WRITE_STATUS=false; log "đang chạy ở tiến trình khác — bỏ qua"; return 0; fi
  mode=$(psql -qAt -d postgres -c "SHOW archive_mode")
  [ "$mode" = "on" ] || fail "archive_mode=$mode trên Postgres — PITR không hoạt động (xem deploy/backup/README.md)"

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
  done

  # archive_command lỗi (spool chưa có, đầy đĩa…) → WAL dồn lại trong pg_wal của server.
  read -r failed_at archived_at < <(psql -qAt -F' ' -d postgres -c \
    "SELECT coalesce(extract(epoch FROM last_failed_time)::bigint, 0),
            coalesce(extract(epoch FROM last_archived_time)::bigint, 0) FROM pg_stat_archiver")
  if [ "$failed_at" -gt "$archived_at" ]; then
    fail "archive_command đang lỗi từ $(date -d "@$failed_at" '+%F %T') — xem log WeKnora-postgres"
  fi
  NOTE="$n file; WAL cuối của server: $( [ "$archived_at" -gt 0 ] && date -d "@$archived_at" '+%F %T' || echo chưa có)"

  # Chạy mỗi phút: chỉ ghi status khi có file mới hoặc ≥ 60 phút chưa ghi (đủ cho cảnh báo quá hạn).
  now=$(date +%s); last=$(cat "$STATE/wal.status-epoch" 2>/dev/null || echo 0)
  if [ "$n" -eq 0 ] && [ $(( now - last )) -lt 3600 ]; then
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

job_restore_check() {
  pg_env
  local scratch="${PGDATABASE}_restore_check" src_tables dst_tables src_rev dst_rev dim vec bm25 emb
  local q_tables="SELECT count(*) FROM pg_tables WHERE schemaname='public'"
  local q_rev="SELECT version || CASE WHEN dirty THEN ' dirty' ELSE '' END FROM schema_migrations"
  need_space "$(psql -qAt -d postgres -c "SELECT pg_database_size('$PGDATABASE')")"
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
  psql -qAt -d postgres -c "DROP DATABASE \"$scratch\""
  NOTE="tables=$dst_tables/$src_tables migration=$dst_rev embeddings=$emb vector_hits=$vec bm25_hits=$bm25"
  if [ "$src_tables" != "$dst_tables" ] || [ "$src_rev" != "$dst_rev" ]; then
    fail "khôi phục không khớp: $NOTE"
  fi
  if [ "${emb:-0}" -gt 0 ] && { [ "$vec" -eq 0 ] || [ "$bm25" -eq 0 ]; }; then
    fail "truy vấn vector/BM25 trên bản khôi phục không trả kết quả: $NOTE"
  fi
}

mkdir -p "$STATE" "$TMP"
ERRLOG="$TMP/$JOB.stderr"
exec 3>&2 2>"$ERRLOG"     # stderr vào file (đồng bộ) → đọc được nguyên nhân khi lỗi
mc alias set backup "$BACKUP_S3_ENDPOINT" "$BACKUP_S3_ACCESS_KEY" "$BACKUP_S3_SECRET_KEY" >/dev/null
case "$JOB" in
  wal) job_wal ;;
  pgbase) job_pgbase ;;
  postgres) job_postgres ;;
  files) job_files ;;
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
