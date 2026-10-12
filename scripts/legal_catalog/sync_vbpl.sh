#!/usr/bin/env bash
# Nightly vbpl.vn → legal catalogue sync, meant for the orca user's crontab
# (cron runs outside the orca-serve cgroup, so a service restart cannot kill
# it). One run: incremental crawl (crawl_vbpl.py), then a full import of its
# export into legal_documents / legal_document_relations.
#
#   30 19 * * * /home/WeRAG/scripts/legal_catalog/sync_vbpl.sh   # 02:30 VN
#
# Environment (optional):
#   VBPL_OUT      crawl directory (default /home/orca/data/vbpl)
#   VBPL_DB_HOST  Postgres host; by default the WeRAG Postgres is found on
#                 the Docker network (.env names it "postgres", which the
#                 host cannot resolve)
set -euo pipefail

REPO=$(cd "$(dirname "$0")/../.." && pwd)
OUT=${VBPL_OUT:-/home/orca/data/vbpl}
LOG="$OUT/sync.log"
mkdir -p "$OUT/bin"
log() { echo "$(date -u +%FT%TZ) $*" >>"$LOG"; }

# One sync at a time, and never next to a crawl started by hand.
exec 9>"$OUT/.sync.lock"
flock -n 9 || { log "skip: previous sync still running"; exit 0; }
if pgrep -f "crawl_vbpl.py $OUT" >/dev/null; then
	log "skip: a crawl of $OUT is running"
	exit 0
fi

env_value() { sed -n "s/^$1=//p" "$REPO/.env" | tail -1 | tr -d "'\""; }
export DB_DRIVER=postgres
export DB_USER=${DB_USER:-$(env_value DB_USER)}
export DB_PASSWORD=${DB_PASSWORD:-$(env_value DB_PASSWORD)}
export DB_NAME=${DB_NAME:-$(env_value DB_NAME)}
export DB_PORT=5432
export PGPASSWORD=$DB_PASSWORD

find_db() {
	local ip
	for i in $(seq 2 60); do
		ip=172.20.0.$i
		if timeout 2 psql -h "$ip" -U "$DB_USER" -d "$DB_NAME" -Atc \
			"select 1 from information_schema.tables where table_name='legal_documents'" 2>/dev/null | grep -q 1; then
			echo "$ip"
			return 0
		fi
	done
	return 1
}
export DB_HOST=${VBPL_DB_HOST:-$(find_db)} || { log "error: WeRAG Postgres not found"; exit 1; }

log "crawl start"
python3 -I -u "$REPO/scripts/legal_catalog/crawl_vbpl.py" "$OUT" >>"$OUT/crawl.log" 2>&1
log "crawl done; import into $DB_HOST"
(cd "$REPO" && go build -o "$OUT/bin/legal-catalog-import" ./cmd/legal-catalog-import)
"$OUT/bin/legal-catalog-import" -documents "$OUT/documents.jsonl" -relations "$OUT/relations.jsonl" \
	-snapshot "$(date -u +%F)" >>"$LOG" 2>&1
log "sync done"
