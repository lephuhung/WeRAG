#!/usr/bin/env bash
# Langfuse archiver: copies each UTC day of traces / observations / scores out
# of ClickHouse into Parquet files on S3-compatible storage (MinIO), so the
# data survives a ClickHouse retention cut and can be read by training jobs
# without going through Langfuse.
#
# Layout:  s3://<bucket>/<prefix><table>/dt=YYYY-MM-DD/<table>.parquet
#
# The s3() calls run inside the ClickHouse server, so the endpoint must be
# reachable from the langfuse-clickhouse container, not only from this one.
#
# Every run:
#   * re-exports the last LANGFUSE_ARCHIVE_LOOKBACK_DAYS days (Langfuse keeps
#     updating a trace after it starts, so a day is not final at midnight);
#   * exports any older day that has no file yet (first run = full backfill);
#   * when LANGFUSE_ARCHIVE_RETENTION_DAYS > 0, deletes days older than that
#     from ClickHouse — only after the archived row count matches.
set -euo pipefail

CH_HOST=${CLICKHOUSE_HOST:-langfuse-clickhouse}
CH_PORT=${CLICKHOUSE_PORT:-9000}
CH_USER=${CLICKHOUSE_USER:-clickhouse}
CH_PASSWORD=${CLICKHOUSE_PASSWORD:-clickhouse}

S3_ENDPOINT=${ARCHIVE_S3_ENDPOINT:?ARCHIVE_S3_ENDPOINT is required}
S3_BUCKET=${ARCHIVE_S3_BUCKET:?ARCHIVE_S3_BUCKET is required}
S3_PREFIX=${ARCHIVE_S3_PREFIX:-archive/}
S3_ACCESS_KEY=${ARCHIVE_S3_ACCESS_KEY:?ARCHIVE_S3_ACCESS_KEY is required}
S3_SECRET_KEY=${ARCHIVE_S3_SECRET_KEY:?ARCHIVE_S3_SECRET_KEY is required}

RUN_HOUR=${ARCHIVE_HOUR_UTC:-2}
LOOKBACK_DAYS=${ARCHIVE_LOOKBACK_DAYS:-3}
RETENTION_DAYS=${ARCHIVE_RETENTION_DAYS:-0}

# table -> column that decides which day a row belongs to
declare -A DAY_COLUMN=([traces]=timestamp [observations]=start_time [scores]=timestamp)
TABLES=(traces observations scores)

log() { echo "[langfuse-archiver] $(date -u +%FT%TZ) $*"; }

# SQL string literal
q() { printf "'%s'" "${1//\'/\'\'}"; }

ch() {
	clickhouse-client --host "$CH_HOST" --port "$CH_PORT" \
		--user "$CH_USER" --password "$CH_PASSWORD" "$@"
}

s3_url() { # table day
	printf '%s/%s/%s%s/dt=%s/%s.parquet' "${S3_ENDPOINT%/}" "$S3_BUCKET" "$S3_PREFIX" "$1" "$2" "$1"
}

s3_fn() { # table day
	printf "s3(%s, %s, %s, 'Parquet')" "$(q "$(s3_url "$1" "$2")")" "$(q "$S3_ACCESS_KEY")" "$(q "$S3_SECRET_KEY")"
}

source_rows() { # table day
	ch -q "SELECT count() FROM default.$1 FINAL WHERE is_deleted = 0 AND toDate(${DAY_COLUMN[$1]}) = $(q "$2")"
}

# Prints the archived row count, or nothing when the file does not exist.
archived_rows() { # table day
	ch -q "SELECT count() FROM $(s3_fn "$1" "$2")" 2>/dev/null || true
}

export_day() { # table day
	local rows
	rows=$(source_rows "$1" "$2")
	if [[ "$rows" == 0 ]]; then
		return
	fi
	ch --s3_truncate_on_insert=1 -q "
		INSERT INTO FUNCTION $(s3_fn "$1" "$2")
		SELECT * FROM default.$1 FINAL
		WHERE is_deleted = 0 AND toDate(${DAY_COLUMN[$1]}) = $(q "$2")"
	log "exported $1 $2 ($rows rows)"
}

purge_day() { # table day
	local want got
	want=$(source_rows "$1" "$2")
	got=$(archived_rows "$1" "$2")
	if [[ "$want" == 0 ]]; then
		return
	fi
	if [[ -z "$got" || "$got" -lt "$want" ]]; then
		log "keep $1 $2: archive has ${got:-no file} rows, ClickHouse has $want"
		return
	fi
	ch -q "DELETE FROM default.$1 WHERE toDate(${DAY_COLUMN[$1]}) = $(q "$2")"
	log "purged $1 $2 ($want rows, archived)"
}

run_once() {
	local today recent_from purge_before table day
	today=$(date -u +%F)
	recent_from=$(date -u -d "-${LOOKBACK_DAYS} days" +%F)
	for table in "${TABLES[@]}"; do
		for day in $(ch -q "SELECT DISTINCT toString(toDate(${DAY_COLUMN[$table]})) d FROM default.$table WHERE toDate(${DAY_COLUMN[$table]}) < today() ORDER BY d"); do
			if [[ "$day" > "$recent_from" || "$day" == "$recent_from" || -z "$(archived_rows "$table" "$day")" ]]; then
				export_day "$table" "$day" || log "export $table $day failed"
			fi
		done
	done
	if (( RETENTION_DAYS > 0 )); then
		purge_before=$(date -u -d "-${RETENTION_DAYS} days" +%F)
		for table in "${TABLES[@]}"; do
			for day in $(ch -q "SELECT DISTINCT toString(toDate(${DAY_COLUMN[$table]})) d FROM default.$table WHERE toDate(${DAY_COLUMN[$table]}) < $(q "$purge_before") ORDER BY d"); do
				purge_day "$table" "$day" || log "purge $table $day failed"
			done
		done
	fi
	log "run finished (today=$today, re-export from $recent_from, retention=${RETENTION_DAYS}d)"
}

seconds_until_next_run() {
	local now next
	now=$(date -u +%s)
	next=$(date -u -d "today ${RUN_HOUR}:00" +%s)
	if (( next <= now )); then
		next=$(date -u -d "tomorrow ${RUN_HOUR}:00" +%s)
	fi
	echo $(( next - now ))
}

case "${1:-loop}" in
once)
	run_once
	;;
loop)
	log "target $(s3_url '<table>' 'YYYY-MM-DD'), daily at ${RUN_HOUR}:00 UTC"
	run_once || log "initial run failed"
	while true; do
		sleep "$(seconds_until_next_run)"
		run_once || log "run failed"
	done
	;;
*)
	echo "usage: $0 [loop|once]" >&2
	exit 2
	;;
esac
