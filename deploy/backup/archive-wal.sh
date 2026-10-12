#!/bin/sh
# archive_command của Postgres: chép một file WAL vào spool, container backup đẩy lên MinIO.
#   archive_command = '/usr/local/bin/werag-archive-wal %p %f'
# Thoát khác 0 → Postgres giữ WAL trong pg_wal và thử lại (không mất dữ liệu).
set -eu
src=$1 name=$2 dir=/backup-spool/wal
[ -d "$dir" ] || { echo "werag-archive-wal: chưa có $dir (container backup chưa chạy?)" >&2; exit 1; }
if [ -f "$dir/$name" ]; then
  # Lưu trữ lại sau sự cố: chỉ chấp nhận khi nội dung trùng.
  cmp -s "$src" "$dir/$name"
  exit
fi
cp "$src" "$dir/.$name.part"
sync "$dir/.$name.part" 2>/dev/null || sync
mv "$dir/.$name.part" "$dir/$name"
