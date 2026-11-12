#!/usr/bin/env bash
# Lập lịch sync.sh bằng supercronic (giờ theo TZ, mặc định Asia/Ho_Chi_Minh).
set -euo pipefail
CRON=${DCS_SYNC_CRON:-15 */4 * * *}
echo "$CRON /usr/local/bin/sync.sh" > /tmp/crontab
echo "dcs-sync: schedule '$CRON' ($TZ)"
if [ "${DCS_SYNC_ON_START:-0}" = "1" ]; then
	/usr/local/bin/sync.sh || echo "dcs-sync: start-up run failed" >&2
fi
exec supercronic /tmp/crontab
