# Backup WeRAG → MinIO

Container `WeKnora-backup` (service `backup`, profile `backup`) theo cùng mẫu với máy
inventory (`/home/windowsId/deploy/backup`): mã hoá phía client bằng **age**, đẩy lên
MinIO `10.10.0.123:9000`, bucket `inventory-backups` (versioning + object lock GOVERNANCE
7 ngày), prefix `werag/`. User MinIO `werag-backup` chỉ ghi + đọc lại, **không xoá được** —
thời gian giữ do lifecycle của bucket quyết định.

Vector DB là pgvector + ParadeDB nằm **trong** Postgres, nên backup Postgres là backup luôn
dữ liệu parse từ văn bản (chunk, vector, BM25, danh mục văn bản), dữ liệu chat (`messages`,
`sessions`, `memory_*`, DB `WeKnora_docassist`). Tệp gốc và tệp sinh ra khi parse (ảnh trích từ
văn bản…) nằm ngoài Postgres: `/data/files` (KB lưu local) và bucket `werag` (KB lưu MinIO) —
hai job `files` và `objects`. Khôi phục đủ = Postgres + `files` + `objects` (mục F).

| Prefix `werag/…` | Nội dung | Lịch (giờ VN) | Giữ |
|---|---|---|---|
| `wal/` | WAL Postgres (zstd + age), mất tối đa ~1 phút dữ liệu | liên tục (`archive_timeout=60`, đẩy mỗi phút) | 35 ngày |
| `pgbase/` | `pg_basebackup` cả cụm (tar + WAL cần thiết) | CN 01:00 (+ lần đầu khởi động) | 35 ngày |
| `postgres/{daily,weekly,monthly}/` | `pg_dump -Fc` từng DB + `pg_dumpall --globals-only` | 00:15 (CN → weekly, mùng 1 → monthly) | 14 ngày / 8 tuần / 365 ngày |
| `files/` | `/data/files` (GNU tar incremental: CN level 0, ngày khác level 1) | 02:30 | 35 ngày |
| `objects/` | bucket file ứng dụng `werag` (MinIO 10.10.0.123, user chỉ-đọc `werag-objects-ro`): `mc mirror` về `/work/objects` rồi tar incremental như `files` | 02:40 | 35 ngày |
| `neo4j/` | `neo4j-admin database dump` (neo4j + system) | 02:00 (systemd timer trên host, Neo4j dừng ~10 s) | 35 ngày |
| `env/` | `/home/WeRAG/.env` | 03:30 nếu đổi, tối thiểu 7 ngày/lần | 365 ngày |
| `status/werag-*.json` | kết quả lần chạy gần nhất của từng job | — | — |

Thời gian giữ đổi bằng biến `BACKUP_RETENTION_WERAG_*` khi chạy
`provision_backup_storage` (repo inventory, xem dưới). Base backup và WAL phải giữ bằng nhau.

**Không nằm trong backup này:** Redis (chỉ cache/khoá có TTL), Langfuse ClickHouse (đã archive
Parquet riêng; DB `langfuse` trong Postgres thì có). Bucket `werag` không có replication; từ
2026-11-12 có versioning (giữ phiên bản cũ / object đã xoá 15 ngày — chống ghi đè, xoá nhầm) nhưng
cùng ổ đĩa đơn với `inventory-backups` trên 10.10.0.123: hỏng ổ là mất cả bản gốc lẫn backup. Bản
sao ngoài bucket là job `objects` (và chưa có bản offsite).

Job `objects`: bản sao cục bộ `/work/objects` (volume `backup-work`, ~1,4 GiB / 11,7 nghìn object
ngày 2026-11-12, tăng theo văn bản) chỉ tải object mới/đổi và xoá object bucket đã xoá
(`--remove`), nên level 1 chỉ chứa phần thay đổi trong tuần. Archive mã hoá như mọi job; status
`werag-objects.json` có thêm `objects`, `source_bytes`, `copied`, `removed`, `level`. Không đặt
`BACKUP_SRC_S3_*` → job ghi `ok=true` + "bỏ qua" (bản cài không dùng MinIO); đặt thiếu biến hoặc
không kết nối được nguồn → `ok=false`. Lần chạy đầu tải cả bucket (vài phút). Khoá nguồn không
nằm trong argv: khoá chỉ gồm `A-Z a-z 0-9 . _ ~ + = -` → biến `MC_HOST_src` (truyền nguyên, mc
không giải mã `%xx`); có ký tự khác (`/ @ : % #`…) → file cấu hình mc riêng 0700 trong
`/work/tmp/objects.mcsrc`, xoá khi xong hoặc khi lỗi. Mỗi job chỉ một tiến trình (`flock`
`/work/state/<job>.lock`): lần chạy trùng (khởi động + cron, chạy tay) bỏ qua, không ghi status.

Chống xoá nhầm bản sao: nếu bucket nguồn **rỗng** hoặc ít hơn bản sao > 20 % / > 500 object (đổi
nhầm endpoint/bucket, lỗi quyền liệt kê…), job dừng với `ok=false` và **không** chạy
`mirror --remove`. Đúng là đã xoá nhiều thì chạy một lần với `BACKUP_OBJECTS_ALLOW_SHRINK=1`:
`sudo docker exec -u backup -e BACKUP_OBJECTS_ALLOW_SHRINK=1 WeKnora-backup backup.sh objects`.
Sau mirror, mọi dòng lỗi của mc → `ok=false`. Số tệp bản sao khác số object nguồn → mirror lại
tối đa 2 lần (đêm có thể đang nhập văn bản); vẫn lệch thì so từng key: chỉ key nguồn **có từ
trước** lần mirror cuối mà thiếu trong bản sao (vd key `x` cùng `x/con` trên S3) mới là lỗi, key
mới hơn được ghi chú và vào lần sau. Lỗi thì không tạo archive.

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
   và `PG_ARCHIVE_MODE=on`. Cho job `objects`: tạo user MinIO **chỉ đọc** bucket file ứng dụng
   (`mc admin user add` + policy `s3:GetObject`, `s3:ListBucket`, `s3:GetBucketLocation` trên
   `werag`) rồi đặt `BACKUP_SRC_S3_ENDPOINT/BUCKET/ACCESS_KEY/SECRET_KEY`. Lifecycle
   `werag/objects/` 35 ngày đang đặt tay trên bucket — thêm vào `provision_backup_storage` (repo
   inventory) để chạy lại script không xoá mất. Chạy lại không có `--users` để chỉ cập nhật lifecycle/policy;
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

   Mỗi lần khởi động container chạy `wal` trước, rồi chạy nền `postgres`, `env` và (lần đầu)
   `pgbase`, `files`, `objects` — supercronic khởi động ngay nên WAL vẫn được đẩy mỗi phút. Cho tới khi container
   tạo `/spool/wal`, `archive_command` báo lỗi và Postgres giữ WAL trong `pg_wal` (không mất).
4. Timer Neo4j trên host:

   ```bash
   sudo cp deploy/backup/werag-neo4j-backup.{service,timer} /etc/systemd/system/
   sudo systemctl daemon-reload && sudo systemctl enable --now werag-neo4j-backup.timer
   ```

Sửa script trong `deploy/backup/` → `--build` lại container (script nằm trong image),
riêng `archive-wal.sh` được mount thẳng vào Postgres và `neo4j-dump.sh` chạy từ repo.
Kiểm thử job `objects` + `restore.sh objects` trên MinIO/Postgres dùng một lần (không đụng hệ
thống thật): `sudo deploy/backup/test/objects-test.sh`.

## Giám sát

- **Cảnh báo trong app WeRAG** (không qua inventory/Telegram): backend đọc
  `status/werag-<job>.json` mỗi 10 phút bằng `BACKUP_S3_*` (cùng `.env`) và tạo một thông báo
  gom cho mọi SuperAdmin (loại `backup_alert`, "Sao lưu gặp sự cố: N việc") khi một job
  `ok=false`, quá hạn, hoặc không có/không đọc được status (lỗi đọc phải kéo dài ≥ 30 phút; trong
  lúc có job chưa đọc được, thông báo giữ nguyên, không đóng); tự đóng khi mọi job ổn lại (đánh
  dấu `resolved_at` trong payload nên khởi động lại backend không nuốt mất lần lỗi sau). Quá hạn sau: `wal` 15 phút (job ghi status ít nhất 10 phút/lần),
  `postgres`/`files`/`objects`/`neo4j` 26 giờ, `pgbase`/`env`/`restore-check` 8 ngày. Job WAL ghi
  status cả giữa lúc đẩy dồn nhiều file (≥ 10 phút/lần). Backend đọc bằng
  `BACKUP_STATUS_S3_ACCESS_KEY/SECRET_KEY` nếu có — user MinIO chỉ `s3:GetObject` trên
  `inventory-backups/status/werag-*` — nếu không thì bằng `BACKUP_S3_*`. Bảng trạng
  thái: Quản trị hệ thống → Sao lưu (`/platform/admin?section=backups`). Danh sách job theo dõi:
  `BACKUP_MONITOR_JOBS`. Portal inventory KHÔNG giám sát WeRAG (đã gỡ 2026-10-12, inventory
  cd24a7c). Job WAL báo lỗi khi `archive_command` hỏng (`pg_stat_archiver`; khi đó không ghi
  status "ok" giữa vòng đẩy).
- **Cảnh báo `objects` "giảm bất thường"** (bucket nguồn rỗng / ít hơn bản sao > 20 % hoặc > 500
  object): **đừng** cho phép ngay. Kiểm tra `BACKUP_SRC_S3_ENDPOINT/BUCKET` đúng chưa, user chỉ-đọc
  còn quyền liệt kê không, và số object thật (`mc du` bucket `werag`); so với số văn bản đã xoá
  trong app. Chỉ khi đúng là đã xoá nhiều: `sudo docker exec -u backup -e
  BACKUP_OBJECTS_ALLOW_SHRINK=1 WeKnora-backup backup.sh objects` (một lần; lần sau tự so với bản
  sao mới). Nếu nghi bucket bị xoá nhầm: khôi phục từ archive trước đó (mục F, tham số
  `tới-archive`) rồi `mc mirror` lên lại, bucket còn versioning 15 ngày.
- **Rủi ro đĩa:** nếu WAL không đẩy được, `pg_wal` và spool phình dần trên ổ của máy này
  (đã dùng > 80%). Cảnh báo WAL phải được xử lý trong ngày.
- Tại chỗ: `sudo docker logs WeKnora-backup`, `journalctl -u werag-neo4j-backup`,
  `sudo docker exec WeKnora-postgres psql -U postgres -c 'select * from pg_stat_archiver'`.
- Chạy tay một job: `sudo docker exec -u backup WeKnora-backup backup.sh <job>`.
- `restore-check` (CN 04:45) khôi phục `pg_dump` của DB `WeKnora` vào `WeKnora_restore_check`
  trên chính server rồi xoá: so số bảng + migration, chạy thử truy vấn vector và BM25. Phần
  `objects`: tải lại mọi archive của chuỗi hiện tại (level 0 + level 1) so sha256 lúc đẩy và
  header age, rồi đối chiếu key MinIO mà DB khôi phục tham chiếu (`resources`, provider
  `minio`, đang dùng, tạo trước lần đóng gói cuối) với danh sách tệp của chuỗi
  (`/work/state/objects-index/`): key có trong bản sao bucket mà thiếu trong chuỗi → lỗi; key
  thiếu cả trong bucket nguồn → ghi `missing_in_source=N`, lỗi khi N > max(`BACKUP_OBJECTS_MAX_MISSING`
  (10), `BACKUP_OBJECTS_MAX_MISSING_PCT` (1) % số tệp DB tham chiếu). DB tạm luôn bị xoá, kể cả khi job lỗi giữa chừng. Container không có private key
  nên **không giải mã** — diễn tập giải mã là `restore.sh objects` + `objects-check` (mục F). Lần
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

### F. Bucket file ứng dụng (`objects`) và khôi phục toàn bộ

```bash
$RESTORE objects /restore/objects                 # level 0 mới nhất + level 1 cuối sau nó
$RESTORE objects /restore/objects 20261110T024001 # hoặc: trạng thái lúc một archive cũ hơn
$RESTORE objects-check /restore/objects WeKnora   # mọi key resources (minio) trỏ tới phải có
```

Đẩy lại lên bucket (MinIO thật hoặc bucket mới) bằng một tài khoản **có quyền ghi** — không
phải `werag-objects-ro`; chạy lại được, chỉ chép tệp thiếu/khác:

```bash
sudo docker run --rm --network host -v /var/tmp/werag-restore/objects:/src:ro \
  -e MC_HOST_dst="http://<access>:<secret>@10.10.0.123:9000" --entrypoint mc werag-backup:latest \
  mirror --overwrite /src dst/werag
```

Không dùng `--remove` khi đẩy lại (bucket có thể đã có object mới hơn bản backup). Bản mới nhất
hỏng (vd nguồn bị xoá rồi mới backup với `ALLOW_SHRINK`) → `$RESTORE list objects` rồi khôi phục
tới archive trước đó (tham số thứ hai; level 0 tương ứng được lấy tự động, giữ 35 ngày).

**Khôi phục toàn bộ sau sự cố** (thứ tự, mỗi bước xem mục tương ứng):

1. `.env` (E) nếu mất máy — lấy `DB_*`, `BACKUP_*`, khoá storage.
2. Postgres: `pgbase` + WAL tới thời điểm cần (B), rồi **REINDEX BM25** như ở B — gồm dữ liệu
   parse (chunk, vector, BM25, danh mục). Không có bản vật lý dùng được thì `pg_dump` (A).
3. `files` (C) → volume `data-files` (KB lưu local).
4. `objects` (F) → `objects-check` với DB vừa khôi phục → `mc mirror` lên bucket `werag`.
5. Neo4j (D).
6. Khởi động app; kiểm tra mở được một văn bản + ảnh của KB MinIO và một KB local, thử hỏi.
   Sau đó chạy `backup.sh pgbase` (timeline mới).

Object tạo sau lần `objects` cuối (02:40) mà DB khôi phục (PITR) vẫn tham chiếu sẽ thiếu —
`objects-check` liệt kê; nếu bucket nguồn còn thì lấy lại từ đó.
