#!/usr/bin/env bash
# Khôi phục (chạy tay, cần age PRIVATE key — container backup thường trực không có).
#
#   restore.sh list [postgres|pgbase|wal|files|neo4j|env]   liệt kê bản backup
#   restore.sh fetch <object|latest-<loại>> <file-ra>         tải + giải mã một object
#   restore.sh postgres <db> <object|latest> <db-mới>        pg_restore bản dump của <db> vào DB mới
#   restore.sh pgbase <object|latest> <thư-mục>              giải nén base backup (vật lý) ra thư mục
#   restore.sh wal <file-WAL-đầu> <thư-mục>                  tải mọi WAL từ file đó trở đi (+ .history)
#   restore.sh files <thư-mục>                               level 0 mới nhất + level 1 sau nó
#
# Ví dụ (trong /home/WeRAG, xem README.md cho quy trình đầy đủ):
#   docker compose -p spec-airag-refactor run --rm --no-deps --user "$(id -u)" \
#     -v /home/orca/.backup-keys:/keys:ro -e BACKUP_AGE_IDENTITY=/keys/inventory-backup-age.key \
#     --entrypoint restore.sh backup postgres WeKnora latest WeKnora_restored
set -Eeuo pipefail

BUCKET="${BACKUP_S3_BUCKET:-inventory-backups}"
PREFIX="${BACKUP_PREFIX:-werag}"
IDENTITY="${BACKUP_AGE_IDENTITY:-/keys/backup-age.key}"
# Chạy bằng --user của chủ private key → không ghi được /work (của user backup).
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
export MC_CONFIG_DIR="$WORK/.mc"

mc alias set backup "$BACKUP_S3_ENDPOINT" "$BACKUP_S3_ACCESS_KEY" "$BACKUP_S3_SECRET_KEY" >/dev/null

keys() {  # loại [lọc-tên] — key đầy đủ, cũ → mới
  mc ls --recursive --json "backup/$BUCKET/$PREFIX/$1/" \
    | jq -r 'select(.type=="file") | [.lastModified, .key] | @tsv' \
    | sort | cut -f2 | grep -E "${2:-.}" | sed "s#^#$PREFIX/$1/#"
}

resolve() {  # object|latest-<loại>
  case "$1" in
    latest-*) keys "${1#latest-}" | tail -1 ;;
    *) echo "$1" ;;
  esac
}

decrypt() {  # key → stdout
  [ -r "$IDENTITY" ] || { echo "không đọc được private key $IDENTITY" >&2; exit 1; }
  echo "[restore] $1" >&2
  mc cat "backup/$BUCKET/$1" | age -d -i "$IDENTITY"
}

fetch() {  # object out
  local key; key=$(resolve "$1")
  [ -n "$key" ] || { echo "không tìm thấy bản backup" >&2; exit 1; }
  decrypt "$key" > "$2"
}

case "${1:-}" in
  list)
    mc ls --recursive "backup/$BUCKET/$PREFIX/${2:-}" ;;
  fetch)
    fetch "${2:?object}" "${3:?file ra}" ;;
  postgres)
    db="${2:?tên DB gốc}"; target="${4:?tên DB mới}"; obj="${3:-latest}"
    [ "$obj" = latest ] && obj=$(keys postgres "/$db-[0-9T]+\.dump\.age$" | tail -1)
    [ -n "$obj" ] || { echo "không có bản dump nào của $db" >&2; exit 1; }
    export PGHOST="${PGHOST:-postgres}"
    out="$WORK/restore.dump"
    fetch "$obj" "$out"
    psql -qAt -d postgres -c "CREATE DATABASE \"$target\" TEMPLATE template0"
    pg_restore --no-owner --no-privileges --exit-on-error -d "$target" "$out"
    rm -f "$out"
    echo "[restore] xong → DB $target (DB đang chạy KHÔNG bị đụng tới)" ;;
  pgbase)
    dir="${3:?thư mục ra}"; key=$(resolve "${2:-latest}"); [ "$key" = latest ] && key=$(keys pgbase | tail -1)
    [ -n "$key" ] || { echo "không có base backup" >&2; exit 1; }
    mkdir -p "$dir"
    decrypt "$key" | zstd -q -d | tar -C "$dir" -xf -
    echo "[restore] base → $dir; WAL bắt đầu từ: $(basename "$key" | sed -E 's/.*-([0-9A-F]{24})\.tar.*/\1/')" ;;
  wal)
    from="${2:?file WAL đầu (24 ký tự hex)}"; dir="${3:?thư mục ra}"
    mkdir -p "$dir"
    n=0
    while read -r key; do
      name=$(basename "$key" .zst.age)
      # File WAL: so theo tên (timeline+log+segment) — bỏ các file trước base backup.
      if [[ "$name" != *.history ]] && [[ "${name:0:24}" < "$from" ]]; then continue; fi
      decrypt "$key" | zstd -q -d > "$dir/$name"
      n=$(( n + 1 ))
    done < <(keys wal)
    echo "[restore] $n file WAL → $dir" ;;
  files)
    dir="${2:?thư mục ra}"
    l0=$(keys files 'L0\.tar' | tail -1)
    [ -n "$l0" ] || { echo "không có bản level 0" >&2; exit 1; }
    l1=$(keys files 'L1\.tar' | awk -v l0="$(basename "$l0")" '{ split($0, p, "/"); if (p[length(p)] > l0) last=$0 } END { print last }')
    mkdir -p "$dir"
    for key in $l0 $l1; do
      decrypt "$key" | zstd -q -d | tar -C "$dir" -g /dev/null -xf -
    done
    echo "[restore] files → $dir (level 0: $(basename "$l0")${l1:+, level 1: $(basename "$l1")})" ;;
  *)
    sed -n '2,15p' "$0"; exit 2 ;;
esac
