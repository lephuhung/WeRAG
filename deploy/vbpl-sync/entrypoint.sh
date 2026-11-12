#!/usr/bin/env bash
# Lập lịch sync.sh bằng supercronic (giờ theo TZ, mặc định Asia/Ho_Chi_Minh).
#
# Entrypoint là PID 1 và giữ supercronic làm tiến trình con: supercronic không
# chuyển tiếp tín hiệu cho job, nên khi `docker stop` (SIGTERM/SIGINT) entrypoint
# tự gửi SIGTERM cho sync.sh đang chạy (sync.sh dừng bước hiện tại cùng cả
# process group, ghi lần chạy là lỗi "stopped"), chờ nó xong rồi mới dừng
# supercronic. Compose cho stop_grace_period 90s; sau đó Docker SIGKILL.
set -euo pipefail
SYNC=/usr/local/bin/sync.sh
CRON=${VBPL_SYNC_CRON:-30 2 * * *}
echo "$CRON $SYNC" > /tmp/crontab
echo "vbpl-sync: schedule '$CRON' ($TZ)"

# sync_pids: the running sync.sh, by its command line; only the process
# group leaders (sync.sh runs in its own group: its subshells share the
# command line) unless none is one.
sync_pids() {
	local d pid cmd st leaders="" all=""
	for d in /proc/[0-9]*; do
		pid=${d#/proc/}
		[ "$pid" = "$$" ] && continue
		cmd=$(tr '\0' ' ' <"$d/cmdline" 2>/dev/null) || continue
		case " $cmd" in
		*" $SYNC "*) ;;
		*) continue ;;
		esac
		all="$all $pid"
		st=$(<"$d/stat") 2>/dev/null || continue
		st=${st##*) }
		# shellcheck disable=SC2086
		set -- $st
		[ "$3" = "$pid" ] && leaders="$leaders $pid"
	done
	echo "${leaders:-$all}"
}

CRON_PID=""
stop() {
	trap '' TERM INT
	echo "vbpl-sync: stopping"
	local pids
	pids=$(sync_pids)
	# shellcheck disable=SC2086
	[ -n "${pids// /}" ] && kill -TERM $pids 2>/dev/null || true
	# sync.sh records its run as stopped; then supercronic has no job left
	for _ in $(seq 80); do
		pids=$(sync_pids)
		[ -z "${pids// /}" ] && break
		sleep 1
	done
	[ -n "$CRON_PID" ] && kill -TERM "$CRON_PID" 2>/dev/null || true
	[ -n "$CRON_PID" ] && wait "$CRON_PID" 2>/dev/null || true
	exit 143
}
trap stop TERM INT

if [ "${VBPL_SYNC_ON_START:-0}" = "1" ]; then
	# in the background and waited for, so a stop is handled at once
	"$SYNC" &
	wait $! || echo "vbpl-sync: start-up run failed" >&2
fi
supercronic /tmp/crontab &
CRON_PID=$!
wait "$CRON_PID"
