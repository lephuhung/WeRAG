#!/usr/bin/env bash
# Kiểm tra cấu hình → chuẩn bị spool WAL → sinh crontab → chạy supercronic (user backup, uid 999).
set -euo pipefail

if [ "$(id -u)" = 0 ]; then
  # Volume spool do Docker tạo thuộc root; archive_command của Postgres (uid 999) ghi vào
  # /spool/wal, container này (cũng 999) đọc + xoá. Chỉ việc này cần root.
  # (cap_drop ALL: root không có DAC_OVERRIDE → đổi chủ trước, tạo thư mục bằng user backup.)
  chown backup:backup /spool /work
  gosu backup sh -c 'mkdir -p /spool/wal && chmod 0700 /spool/wal'
  exec gosu backup "$0" "$@"
fi

: "${BACKUP_S3_ENDPOINT:?thiếu BACKUP_S3_ENDPOINT}"
: "${BACKUP_S3_ACCESS_KEY:?thiếu BACKUP_S3_ACCESS_KEY}"
: "${BACKUP_S3_SECRET_KEY:?thiếu BACKUP_S3_SECRET_KEY}"
: "${BACKUP_AGE_RECIPIENT:?thiếu BACKUP_AGE_RECIPIENT (age public key)}"
case "$BACKUP_AGE_RECIPIENT" in
  age1*) ;;
  *) echo "BACKUP_AGE_RECIPIENT phải là age public key (age1...), KHÔNG phải private key" >&2; exit 1 ;;
esac

mkdir -p /work/state /work/tmp

cat > /work/crontab <<EOF
${BACKUP_WAL_CRON:-* * * * *} /usr/local/bin/backup.sh wal
${BACKUP_PG_CRON:-15 0 * * *} /usr/local/bin/backup.sh postgres
${BACKUP_BASE_CRON:-0 1 * * 0} /usr/local/bin/backup.sh pgbase
${BACKUP_FILES_CRON:-30 2 * * *} /usr/local/bin/backup.sh files
${BACKUP_ENV_CRON:-30 3 * * *} /usr/local/bin/backup.sh env
${BACKUP_RESTORE_CHECK_CRON:-45 4 * * 0} /usr/local/bin/backup.sh restore-check
EOF
echo "[backup] lịch:"; sed 's/^/  /' /work/crontab

if [ "${BACKUP_RUN_ON_START:-true}" = "true" ]; then
  /usr/local/bin/backup.sh postgres || true
  /usr/local/bin/backup.sh env || true
  # WAL vô dụng nếu chưa có base backup nào → tạo ngay lần đầu.
  [ -f /work/state/pgbase.ok ] || /usr/local/bin/backup.sh pgbase || true
  [ -f /work/state/files.ok ] || /usr/local/bin/backup.sh files || true
fi

exec supercronic -passthrough-logs /work/crontab
