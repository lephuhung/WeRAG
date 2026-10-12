#!/usr/bin/env bash
# Dump Neo4j (Community: không có backup online) → container backup mã hoá + đẩy MinIO.
# Chạy trên HOST bằng root (systemd timer werag-neo4j-backup.timer, xem README.md):
# dừng WeKnora-neo4j vài phút, neo4j-admin dump từng database, khởi động lại.
set -Eeuo pipefail

NEO4J=${NEO4J_CONTAINER:-WeKnora-neo4j}
BACKUP=${BACKUP_CONTAINER:-WeKnora-backup}
DATABASES=${NEO4J_DATABASES:-"neo4j system"}

image=$(docker inspect -f '{{.Config.Image}}' "$NEO4J")
uid=$(docker inspect -f '{{.Config.User}}' "$NEO4J"); uid=${uid:-7474:7474}
tmp=$(mktemp -d /var/tmp/werag-neo4j.XXXXXX)
chown "$uid" "$tmp"

started=false
cleanup() {
  rm -rf "$tmp"
  if [ "$started" = false ]; then docker start "$NEO4J" >/dev/null; fi
}
trap cleanup EXIT

echo "[neo4j-dump] dừng $NEO4J"
docker stop -t 120 "$NEO4J" >/dev/null
rc=0
for db in $DATABASES; do
  # Cùng image + volume với container thật; chạy bằng user neo4j để không tạo file root trong /data.
  docker run --rm --user "$uid" --volumes-from "$NEO4J" -v "$tmp:/dump" --entrypoint neo4j-admin \
    "$image" database dump "$db" --to-path=/dump --overwrite-destination=true || { rc=$?; break; }
done
docker start "$NEO4J" >/dev/null; started=true
echo "[neo4j-dump] đã khởi động lại $NEO4J"

if [ "$rc" -ne 0 ]; then
  # Ghi trạng thái thất bại để portal inventory cảnh báo.
  docker exec -i -u backup "$BACKUP" /usr/local/bin/backup.sh neo4j "dump-failed" < /dev/null || true
  exit "$rc"
fi
for db in $DATABASES; do
  docker exec -i -u backup "$BACKUP" /usr/local/bin/backup.sh neo4j "$db" < "$tmp/$db.dump"
done
