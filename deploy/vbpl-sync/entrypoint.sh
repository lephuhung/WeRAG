#!/usr/bin/env bash
# Lập lịch sync.sh bằng supercronic (giờ theo TZ, mặc định Asia/Ho_Chi_Minh).
set -euo pipefail
CRON=${VBPL_SYNC_CRON:-30 2 * * *}
echo "$CRON /usr/local/bin/sync.sh" > /tmp/crontab
echo "vbpl-sync: schedule '$CRON' ($TZ)"
if [ "${VBPL_SYNC_ON_START:-0}" = "1" ]; then
	/usr/local/bin/sync.sh || echo "vbpl-sync: start-up run failed" >&2
fi
exec supercronic /tmp/crontab
