# Backup WeRAG → MinIO

Container `WeKnora-backup` (service `backup`, profile `backup`) theo cùng mẫu với máy
inventory (`/home/windowsId/deploy/backup`): mã hoá phía client bằng **age**, đẩy lên
MinIO `10.10.0.123:9000`, bucket `inventory-backups` (versioning + object lock GOVERNANCE
7 ngày), prefix `werag/`. User MinIO `werag-backup` chỉ ghi + đọc lại, **không xoá được** —
thời gian giữ do lifecycle của bucket quyết định.

Vector DB là pgvector + ParadeDB nằm **trong** Postgres, nên backup Postgres là backup luôn
vector, BM25, dữ liệu chat (`messages`, `sessions`, `memory_*`, DB `WeKnora_docassist`).

| Prefix `werag/…` | Nội dung | Lịch (giờ VN) | Giữ |
|---|---|---|---|
| `wal/` | WAL Postgres (zstd + age), mất tối đa ~1 phút dữ liệu | liên tục (`archive_timeout=60`, đẩy mỗi phút) | 35 ngày |
| `pgbase/` | `pg_basebackup` cả cụm (tar + WAL cần thiết) | CN 01:00 (+ lần đầu khởi động) | 35 ngày |
| `postgres/{daily,weekly,monthly}/` | `pg_dump -Fc` từng DB + `pg_dumpall --globals-only` | 00:15 (CN → weekly, mùng 1 → monthly) | 14 ngày / 8 tuần / 365 ngày |
| `files/` | `/data/files` (GNU tar incremental: CN level 0, ngày khác level 1) | 02:30 | 35 ngày |
| `neo4j/` | `neo4j-admin database dump` (neo4j + system) | 02:00 (systemd timer trên host, Neo4j dừng ~10 s) | 35 ngày |
| `env/` | `/home/WeRAG/.env` | 03:30 nếu đổi, tối thiểu 7 ngày/lần | 365 ngày |
| `status/werag-*.json` | kết quả lần chạy gần nhất của từng job | — | — |

Thời gian giữ đổi bằng biến `BACKUP_RETENTION_WERAG_*` khi chạy
`provision_backup_storage` (repo inventory, xem dưới). Base backup và WAL phải giữ bằng nhau.

**Không nằm trong backup này:** Redis (chỉ cache/khoá có TTL), bucket file ứng dụng `werag`
trên cùng MinIO (cấu hình storage của profile 10009) — bảo vệ bằng replication/versioning MinIO,
Langfuse ClickHouse (đã archive Parquet riêng; DB `langfuse` trong Postgres thì có).

## Khoá

- Public key age chung với inventory (`BACKUP_AGE_RECIPIENT` trong `.env`). Private key:
  `/home/orca/.backup-keys/inventory-backup-age.key` — **phải có thêm ≥ 1 bản offline**.
  Mất private key = mất toàn bộ backup. Không đưa private key vào `.env` (chính `.env` được
  backup bằng khoá này).
- Credential `werag-backup` chỉ nằm trong `.env` (và bản `.env` đã mã hoá trên MinIO).

## Cài đặt (máy mới / làm lại)

1. User MinIO + lifecycle (repo inventory, cần tài khoản admin MinIO lưu trong DB inventory):

   ```bash
   cd /home/windowsId && sudo docker compose exec api python -m scripts.provision_backup_storage --users werag-backup
   ```

   Chép 4 dòng `BACKUP_S3_*` in ra vào `/home/WeRAG/.env`, thêm `BACKUP_AGE_RECIPIENT=age1…`
   và `PG_ARCHIVE_MODE=on`. Chạy lại không có `--users` để chỉ cập nhật lifecycle/policy;
   `--users werag-backup` sẽ **đổi** secret.
2. Cho phép `pg_basebackup` từ mạng Docker (một lần, nằm trong volume dữ liệu):

   ```bash
   sudo docker exec WeKnora-postgres sh -c 'echo "host replication all samenet scram-sha-256" >> $PGDATA/pg_hba.conf'
   sudo docker exec WeKnora-postgres psql -U postgres -c 'select pg_reload_conf()'
   ```
3. Tạo lại Postgres (archive + `shm_size`) và chạy container backup:

   ```bash
   cd /home/WeRAG
   sudo docker compose -p spec-airag-refactor -f docker-compose.yml up -d --no-deps --wait postgres
   sudo docker compose -p spec-airag-refactor -f docker-compose.yml --profile backup up -d --no-deps --build backup
   ```

   Lần đầu container chạy ngay `postgres`, `env`, `pgbase`, `files`. Cho tới khi container
   tạo `/spool/wal`, `archive_command` báo lỗi và Postgres giữ WAL trong `pg_wal` (không mất).
4. Timer Neo4j trên host:

   ```bash
   sudo cp deploy/backup/werag-neo4j-backup.{service,timer} /etc/systemd/system/
   sudo systemctl daemon-reload && sudo systemctl enable --now werag-neo4j-backup.timer
   ```

Sửa script trong `deploy/backup/` → `--build` lại container (script nằm trong image),
riêng `archive-wal.sh` được mount thẳng vào Postgres và `neo4j-dump.sh` chạy từ repo.

## Giám sát

- **Chưa có kênh cảnh báo riêng.** Portal inventory KHÔNG giám sát WeRAG (đã gỡ 2026-10-12,
  inventory cd24a7c — cảnh báo WeRAG không thuộc app inventory). Mỗi job vẫn ghi
  `status/werag-<job>.json` lên MinIO (ok/lỗi, thời điểm, ghi chú) để một kênh cảnh báo sau
  này đọc; job WAL ghi ít nhất mỗi giờ và báo lỗi khi `archive_command` hỏng (`pg_stat_archiver`).
- **Rủi ro đĩa:** nếu WAL không đẩy được, `pg_wal` và spool phình dần trên ổ của máy này
  (đã dùng > 80%). Cảnh báo WAL phải được xử lý trong ngày.
- Tại chỗ: `sudo docker logs WeKnora-backup`, `journalctl -u werag-neo4j-backup`,
  `sudo docker exec WeKnora-postgres psql -U postgres -c 'select * from pg_stat_archiver'`.
- Chạy tay một job: `sudo docker exec -u backup WeKnora-backup backup.sh <job>`.
- `restore-check` (CN 04:45) khôi phục `pg_dump` của DB `WeKnora` vào `WeKnora_restore_check`
  trên chính server rồi xoá: so số bảng + migration, chạy thử truy vấn vector và BM25. Lần
  đầu (2026-10-12, 76k vector) mất 1 phút 42 giây. Job này sinh lượng WAL xấp xỉ kích thước
  DB (bị archive như mọi WAL) — khi DB lớn, nên chuyển sang lịch hằng tháng.

## Khôi phục

Mọi lệnh chạy trong `/home/WeRAG` bằng user giữ private key, qua `restore.sh` trong image:

```bash
RESTORE="sudo docker compose -p spec-airag-refactor -f docker-compose.yml --profile backup run --rm --no-deps \
  --user $(id -u):$(id -g) -v /home/orca/.backup-keys:/keys:ro -v /var/tmp/werag-restore:/restore \
  -e BACKUP_AGE_IDENTITY=/keys/inventory-backup-age.key --entrypoint restore.sh backup"
$RESTORE list pgbase
```

### A. Một DB từ `pg_dump` (lấy lại bảng / phiên chat bị xoá nhầm)

```bash
$RESTORE postgres WeKnora latest WeKnora_restored      # hoặc thay latest bằng key cụ thể
```

Tạo DB mới cạnh DB đang chạy (từ `template0`), không đụng DB thật; lấy dữ liệu cần thiết rồi
`DROP DATABASE "WeKnora_restored"`. Khôi phục logic dựng lại toàn bộ index HNSW → lâu khi
nhiều vector, và cần `shm_size` đủ lớn (compose đặt 2 GB).

### B. Cả cụm tới một thời điểm (PITR) — đã chạy thử 2026-10-12

```bash
sudo mkdir -p /var/tmp/werag-restore && sudo chown $(id -u):$(id -g) /var/tmp/werag-restore
$RESTORE pgbase latest /restore/base           # in ra file WAL bắt đầu, vd 0000000100000000000000F0
$RESTORE wal 0000000100000000000000F0 /restore/wal
R=/var/tmp/werag-restore
sudo chown -R 999:999 $R && sudo chmod 0700 $R/base
sudo docker run --rm --user 999:999 -v $R:/r --entrypoint pg_verifybackup paradedb/paradedb:v0.22.6-pg17 -n /r/base
sudo touch $R/base/recovery.signal && sudo chown 999:999 $R/base/recovery.signal
echo "restore_command = 'cp /r/wal/%f %p'" | sudo tee -a $R/base/postgresql.auto.conf
# Tuỳ chọn — dừng ở một thời điểm thay vì cuối WAL:
# echo "recovery_target_time = '2026-10-12 08:55:00+07'" | sudo tee -a $R/base/postgresql.auto.conf
sudo docker run -d --name werag-restore --network none --user 999:999 -v $R:/r -e PGDATA=/r/base \
  paradedb/paradedb:v0.22.6-pg17 postgres -c archive_mode=off
```

`archive_mode=off` + `--network none`: bản khôi phục không được ghi vào spool/MinIO thật.
Khi `select pg_is_in_recovery()` trả `f`, **dựng lại index BM25** (ParadeDB Community không
ghi WAL cho index BM25 nên bản vật lý có thể lệch); index HNSW của pgvector thì có WAL, không
cần dựng lại:

```bash
for db in WeKnora WeKnora_docassist; do
  sudo docker exec werag-restore psql -U postgres -d $db -Atc "select format('REINDEX INDEX %s', c.oid::regclass)
    from pg_class c join pg_am a on a.oid = c.relam where a.amname = 'bm25'" \
  | sudo docker exec -i werag-restore psql -U postgres -d $db
done
```

Kiểm tra (đếm `messages`/`embeddings`/`sessions`, thử một câu hỏi). Để **thay** server đang
chạy: dừng `app-dev`/`app`, `langfuse-*` và `WeKnora-postgres`; giữ nguyên volume cũ (đổi tên
hoặc sao lưu lạnh, KHÔNG `down -v`); chép `$R/base` vào volume `spec-airag-refactor_postgres-data`
mới (bỏ `recovery.signal`/`restore_command` nếu đã khôi phục xong trong container thử, hoặc
để Postgres thật tự recovery); khởi động lại Postgres rồi app. Sau khi chuyển, chạy ngay
`backup.sh pgbase` (timeline mới).

### C. Tệp tải lên

```bash
$RESTORE files /restore/files     # level 0 mới nhất + level 1 sau nó
```

Chép nội dung vào volume `spec-airag-refactor_data-files` (đang mount `/data/files`).

### D. Neo4j

```bash
$RESTORE fetch latest-neo4j /restore/neo4j.dump     # kiểm tra tên: $RESTORE list neo4j
sudo docker stop WeKnora-neo4j
sudo docker run --rm --user 7474:7474 --volumes-from WeKnora-neo4j -v /var/tmp/werag-restore:/dump \
  --entrypoint neo4j-admin neo4j:2025.10.1 database load neo4j --from-path=/dump --overwrite-destination=true
sudo docker start WeKnora-neo4j
```

(`load` cần file tên `<db>.dump` trong thư mục `--from-path`.)

### E. `.env`

```bash
$RESTORE fetch latest-env /restore/env.tar.zst && zstd -dc /var/tmp/werag-restore/env.tar.zst | tar -tvf -
```
