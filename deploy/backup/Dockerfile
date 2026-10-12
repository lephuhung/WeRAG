# Container backup của WeRAG → MinIO (bucket inventory-backups, prefix werag/).
# Cùng mẫu với deploy/backup của máy inventory; cùng major Postgres (17) với
# paradedb/paradedb:*-pg17 để pg_dump/pg_basebackup khớp server.
# Chỉ giữ age PUBLIC key: container mã hoá được nhưng không đọc lại được backup.
FROM postgres:17-alpine

RUN apk add --no-cache bash age zstd jq tzdata minio-client supercronic tar coreutils \
    && ln -s /usr/bin/mcli /usr/local/bin/mc \
    # uid/gid 999 = user postgres trong image paradedb (Debian): file WAL do
    # archive_command chép vào spool thuộc 999, container phải đọc + xoá được.
    && delgroup ping \
    && addgroup -g 999 backup \
    && adduser -D -H -u 999 -G backup -h /work backup

COPY backup.sh entrypoint.sh restore.sh /usr/local/bin/
RUN chmod 0755 /usr/local/bin/backup.sh /usr/local/bin/entrypoint.sh /usr/local/bin/restore.sh \
    && mkdir -p /work /spool && chown backup:backup /work /spool

ENV HOME=/work MC_CONFIG_DIR=/work/.mc TZ=Asia/Ho_Chi_Minh
WORKDIR /work

HEALTHCHECK --interval=5m --timeout=10s --start-period=10m \
    CMD find /work/state/postgres.ok -mmin -${BACKUP_HEALTH_MAX_AGE_MIN:-1560} | grep -q .

# entrypoint chạy bằng root chỉ để chown /spool/wal rồi hạ quyền xuống backup (999).
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
