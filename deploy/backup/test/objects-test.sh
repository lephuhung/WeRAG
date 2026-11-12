#!/usr/bin/env bash
# Kiểm thử job `objects` (+ restore.sh objects, phần objects của restore-check) trên MinIO và
# Postgres DÙNG MỘT LẦN: build image từ deploy/backup với tag riêng, chạy minio + postgres tạm
# trong một network docker riêng, xoá hết khi xong. Không đụng container/volume/MinIO thật.
#
#   sudo deploy/backup/test/objects-test.sh            (cần docker; image minio/minio có sẵn)
#
# Kịch bản: chưa cấu hình nguồn → bỏ qua (ok); cấu hình thiếu → lỗi; nguồn không tới được → lỗi;
# bucket rỗng → level 0 rỗng; thêm/sửa/xoá object → level 1; khôi phục bằng private key khớp
# bucket nguồn; restore-check: sha256 + header age + đối chiếu key DB (thiếu trong backup → lỗi,
# thiếu ở nguồn → chỉ ghi chú); chạy job khi spool/stage rỗng không kích hoạt bẫy ERR.
set -Eeuo pipefail

HERE=$(cd "$(dirname "$0")/.." && pwd)
ID="bkobj$$"
NET="$ID-net"; MINIO="$ID-minio"; PG="$ID-pg"; WORKVOL="$ID-work"; IMAGE="werag-backup:objects-test-$$"
MINIO_IMAGE="${MINIO_IMAGE:-minio/minio:RELEASE.2025-09-07T16-13-09Z}"
PASS=0; FAILS=0
KEYDIR=$(mktemp -d)
BINDIR=$(mktemp -d)
cat > "$BINDIR/mc" <<'SH'
#!/bin/sh
# lớp bọc mc của kiểm thử 7c
/usr/bin/mcli "$@"; rc=$?
if [ "$1" = mirror ]; then
  [ -z "${TEST_DROP_AFTER_MIRROR:-}" ] || rm -f "$TEST_DROP_AFTER_MIRROR"
  [ -z "${TEST_MIRROR_ERROR_LINE:-}" ] || echo '{"status":"error","error":{"message":"giả lập lỗi một object"}}'
fi
exit $rc
SH
chmod 0755 "$BINDIR/mc"

cleanup() {
  docker rm -f "$MINIO" "$PG" >/dev/null 2>&1 || true
  docker volume rm -f "$WORKVOL" >/dev/null 2>&1 || true
  docker network rm "$NET" >/dev/null 2>&1 || true
  docker rmi -f "$IMAGE" >/dev/null 2>&1 || true
  rm -rf "$KEYDIR" "$BINDIR"
}
trap cleanup EXIT

ok()   { PASS=$(( PASS + 1 )); echo "  PASS $*"; }
bad()  { FAILS=$(( FAILS + 1 )); echo "  FAIL $*"; }
check() { local what=$1; shift; if "$@"; then ok "$what"; else bad "$what"; fi; }

echo "== build $IMAGE"
docker build -q -t "$IMAGE" "$HERE" >/dev/null
docker network create "$NET" >/dev/null
docker volume create "$WORKVOL" >/dev/null
docker run -d --name "$MINIO" --network "$NET" -e MINIO_ROOT_USER=testadmin -e MINIO_ROOT_PASSWORD=testadmin123 \
  "$MINIO_IMAGE" server /data >/dev/null
docker run -d --name "$PG" --network "$NET" -e POSTGRES_PASSWORD=pw postgres:17-alpine \
  -c archive_mode=on -c archive_command=/bin/true >/dev/null
docker run --rm --entrypoint age-keygen "$IMAGE" > "$KEYDIR/key.txt" 2>/dev/null
RECIPIENT=$(grep -o 'age1[0-9a-z]*' "$KEYDIR/key.txt" | head -1)

# Lệnh trong image với cấu hình chung (chạy root cho gọn: không có entrypoint chown).
ENV_BASE=(-e BACKUP_S3_ENDPOINT="http://$MINIO:9000" -e BACKUP_S3_ACCESS_KEY=testadmin
  -e BACKUP_S3_SECRET_KEY=testadmin123 -e BACKUP_S3_BUCKET=inventory-backups -e BACKUP_AGE_RECIPIENT="$RECIPIENT"
  -e PGHOST="$PG" -e PGUSER=postgres -e PGPASSWORD=pw -e PGDATABASE=postgres -e BACKUP_HOST_LABEL=test)
ENV_SRC=(-e BACKUP_SRC_S3_ENDPOINT="http://$MINIO:9000" -e BACKUP_SRC_S3_BUCKET=werag
  -e BACKUP_SRC_S3_ACCESS_KEY=testadmin -e BACKUP_SRC_S3_SECRET_KEY=testadmin123)
run() { docker run --rm -i --network "$NET" -v "$WORKVOL:/work" -v "$KEYDIR:/keys:ro" --user 0 "${ENV_BASE[@]}" "$@"; }
sh_in() { run --entrypoint bash "$IMAGE" -c "export MC_CONFIG_DIR=/tmp/mc; mc alias set t http://$MINIO:9000 testadmin testadmin123 >/dev/null; $1"; }
job() { run "$@" --entrypoint backup.sh "$IMAGE" objects >/dev/null 2>&1; }
status() { sh_in "mc cat t/inventory-backups/status/werag-objects.json"; }
field() { status | jq -r "$1"; }

for _ in $(seq 30); do sh_in "mc ls t >/dev/null 2>&1" && break; sleep 1; done
sh_in "mc mb -q t/inventory-backups t/werag t/empty >/dev/null"
for _ in $(seq 30); do docker exec "$PG" pg_isready -q -U postgres && break; sleep 1; done

echo "== 1. BACKUP_SRC_S3_* chưa cấu hình → bỏ qua, ok=true"
job || true
check "ok=true" [ "$(field .ok)" = true ]
check "note bỏ qua" grep -q "bỏ qua" <<<"$(field .note)"

echo "== 2. cấu hình thiếu → ok=false"
job -e BACKUP_SRC_S3_BUCKET=werag && bad "job phải thoát lỗi" || ok "thoát lỗi"
check "ok=false" [ "$(field .ok)" = false ]

echo "== 3. nguồn không tới được → ok=false"
job "${ENV_SRC[@]}" -e BACKUP_SRC_S3_ENDPOINT=http://nohost.invalid:9000 && bad "job phải thoát lỗi" || ok "thoát lỗi"
check "ok=false" [ "$(field .ok)" = false ]
echo "    error: $(field .error)"
check "error nêu bucket nguồn" grep -q "werag" <<<"$(field .error)"

echo "== 4. bucket nguồn rỗng → level 0, 0 object"
job "${ENV_SRC[@]}" && ok "thoát 0" || bad "thoát lỗi: $(field .error)"
check "ok=true" [ "$(field .ok)" = true ]
check "objects=0" [ "$(field .objects)" = 0 ]
check "level=0" [ "$(field .level)" = 0 ]
check "key objects/…-L0" grep -q '^werag/objects/[0-9]\{4\}/[0-9]\{2\}/objects-[0-9T]*-L0\.tar\.zst\.age$' <<<"$(field .object)"

echo "== 5. thêm object → level 1 (không phải Chủ nhật)"
sh_in 'mkdir -p /tmp/s/10000/a /tmp/s/10003/b /tmp/s/10000/c
  echo one > /tmp/s/10000/a/1.html; echo two > /tmp/s/10000/a/2.png; echo three > "/tmp/s/10003/b/tệp có dấu cách.pdf"
  head -c 300000 /dev/urandom > /tmp/s/10000/c/big.bin; echo keep > /tmp/s/10000/c/keep.txt
  mc cp -q -r /tmp/s/ t/werag/ >/dev/null'
job "${ENV_SRC[@]}" && ok "thoát 0" || bad "thoát lỗi: $(field .error)"
check "objects=5" [ "$(field .objects)" = 5 ]
check "copied=5" [ "$(field .copied)" = 5 ]
if [ "$(date +%u)" != 7 ]; then check "level=1" [ "$(field .level)" = 1 ]; fi
check "bytes = cỡ archive > 0" [ "$(field .bytes)" -gt 0 ]
ARCH5=$(basename "$(field .object)" .tar.zst.age)
sh_in 'mc mirror -q t/werag /work/test-snap5 >/dev/null'

echo "== 6. sửa 1, xoá 1, thêm 1 → level 1 so với level 0"
sh_in 'echo changed > /tmp/x; mc cp -q /tmp/x t/werag/10000/a/1.html >/dev/null
  mc rm -q t/werag/10000/a/2.png >/dev/null; echo new > /tmp/n; mc cp -q /tmp/n t/werag/10003/b/new.html >/dev/null'
job "${ENV_SRC[@]}" && ok "thoát 0" || bad "thoát lỗi: $(field .error)"
check "objects=5" [ "$(field .objects)" = 5 ]
check "copied=2 removed=1" [ "$(field '"\(.copied) \(.removed)"')" = "2 1" ]
check "chuỗi có 3 archive" [ "$(sh_in 'wc -l < /work/state/objects.chain.tsv')" = 3 ]

echo "== 7. chạy lại không đổi gì → copied=0, archive level 1 vẫn đủ"
job "${ENV_SRC[@]}" && ok "thoát 0" || bad "thoát lỗi: $(field .error)"
check "copied=0 removed=0" [ "$(field '"\(.copied) \(.removed)"')" = "0 0" ]

echo "== 7b. nguồn sai (bucket rỗng) → không mirror --remove, ok=false, bản sao còn nguyên"
job "${ENV_SRC[@]}" -e BACKUP_SRC_S3_BUCKET=empty && bad "job phải thoát lỗi" || ok "thoát lỗi"
check "ok=false" [ "$(field .ok)" = false ]
check "error nêu giảm bất thường" grep -q "giảm bất thường" <<<"$(field .error)"
check "bản sao vẫn 5 tệp" [ "$(sh_in 'find /work/objects -type f | wc -l')" = 5 ]
check "không thêm archive" [ "$(sh_in 'wc -l < /work/state/objects.chain.tsv')" = 4 ]

echo "== 7c. mirror bỏ sót object im lặng / log có dòng lỗi mà mc vẫn thoát 0 → ok=false"
# MinIO không cho tạo cặp key "x" + "x/con" (S3 thì cho, mc bỏ qua im lặng): giả lập bằng một
# lớp bọc mc chỉ trong kiểm thử — sau mirror xoá một tệp của bản sao, hoặc chèn dòng lỗi JSON.
FAKE=(-v "$BINDIR:/test-bin:ro" -e PATH=/test-bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin)
job "${ENV_SRC[@]}" "${FAKE[@]}" -e TEST_DROP_AFTER_MIRROR=/work/objects/10000/c/keep.txt && bad "job phải thoát lỗi" || ok "thoát lỗi"
check "ok=false nêu mirror thiếu" grep -q "mirror thiếu" <<<"$(field .error)"
check "không thêm archive" [ "$(sh_in 'wc -l < /work/state/objects.chain.tsv')" = 4 ]
job "${ENV_SRC[@]}" "${FAKE[@]}" -e TEST_MIRROR_ERROR_LINE=1 && bad "job phải thoát lỗi" || ok "thoát lỗi"
check "ok=false nêu dòng lỗi" grep -q "báo 1 lỗi" <<<"$(field .error)"
check "không thêm archive" [ "$(sh_in 'wc -l < /work/state/objects.chain.tsv')" = 4 ]

echo "== 8. restore.sh objects (private key) khớp bucket nguồn"
sh_in 'restore() { BACKUP_AGE_IDENTITY=/keys/key.txt restore.sh "$@"; }
  restore objects /tmp/out >/dev/null 2>&1 && mc mirror -q t/werag /tmp/src >/dev/null && diff -r /tmp/src /tmp/out' \
  && ok "cây khôi phục = bucket nguồn (có sửa/xoá)" || bad "cây khôi phục khác bucket nguồn"

echo "== 8b. restore.sh objects <dir> <archive cũ> → trạng thái lúc archive đó (sau bước 5)"
sh_in "BACKUP_AGE_IDENTITY=/keys/key.txt restore.sh objects /tmp/out5 '$ARCH5' >/dev/null 2>&1 && diff -r /work/test-snap5 /tmp/out5" \
  && ok "khôi phục tới $ARCH5 = bucket lúc đó" || bad "khôi phục tới $ARCH5 khác bucket lúc đó"
sh_in "BACKUP_AGE_IDENTITY=/keys/key.txt restore.sh objects /tmp/outx no-such-archive >/dev/null 2>&1" \
  && bad "archive không có phải báo lỗi" || ok "archive không có → lỗi"

echo "== 9. restore-check phần objects (hàm của backup.sh, DB tạm có bảng resources)"
docker exec -i "$PG" psql -qX -U postgres <<'SQL' >/dev/null
CREATE TABLE resources (physical_path text, provider varchar(32), state varchar(16), deleted_at timestamptz,
  created_at timestamptz DEFAULT now() - interval '1 hour');
INSERT INTO resources (physical_path, provider, state) VALUES
  ('storage://b1/minio://werag/10000/a/1.html', 'minio', 'active'),
  ('storage://b1/minio://werag/10003/b/tệp có dấu cách.pdf', 'minio', 'active'),
  ('storage://b1/minio://werag/10000/c/big.bin', 'minio', 'active'),
  ('storage://b1/minio://werag/10000/gone/lost.html', 'minio', 'active'),
  ('storage://b1/minio://other/10000/a/1.html', 'minio', 'active'),
  ('storage://b2/local://10000/x.pdf', 'local', 'active'),
  ('storage://b1/minio://werag/10000/a/2.png', 'minio', 'active');
UPDATE resources SET deleted_at = now() WHERE physical_path LIKE '%2.png';
SQL
rc_out=$(run "${ENV_SRC[@]}" --entrypoint bash "$IMAGE" -c '
  set -Eeuo pipefail; source /usr/local/bin/backup.sh restore-check
  mkdir -p "$TMP"; ERRLOG=$TMP/t.stderr; exec 3>&2 2>"$ERRLOG"
  mc alias set backup "$BACKUP_S3_ENDPOINT" "$BACKUP_S3_ACCESS_KEY" "$BACKUP_S3_SECRET_KEY" >/dev/null
  objects_db_refs postgres /tmp/refs; echo "refs=$(wc -l < /tmp/refs)"
  check_objects /tmp/refs; echo "$OBJ_NOTE"' 2>&1) || true
echo "$rc_out" | sed 's/^/    /'
check "refs=4 (chỉ minio://werag, đang dùng)" grep -q '^refs=4$' <<<"$rc_out"
check "chain=4 archive ok" grep -q 'chain=4 archive ok' <<<"$rc_out"
check "missing_in_backup=0 missing_in_source=1" grep -q 'missing_in_backup=0 missing_in_source=1' <<<"$rc_out"

echo "== 10. tệp có trong bản sao mà thiếu trong chuỗi → lỗi"
rc_out=$(run "${ENV_SRC[@]}" --entrypoint bash "$IMAGE" -c '
  set -Eeuo pipefail; source /usr/local/bin/backup.sh restore-check
  mkdir -p "$TMP"; ERRLOG=$TMP/t.stderr; exec 3>&2 2>"$ERRLOG"
  mc alias set backup "$BACKUP_S3_ENDPOINT" "$BACKUP_S3_ACCESS_KEY" "$BACKUP_S3_SECRET_KEY" >/dev/null
  mkdir -p /work/objects/10000/gone && echo x > /work/objects/10000/gone/lost.html
  objects_db_refs postgres /tmp/refs; check_objects /tmp/refs; echo UNEXPECTED_OK
  ' 2>&1) || true
check "check_objects báo lỗi" bash -c '! grep -q UNEXPECTED_OK <<<"$1"' _ "$rc_out"
check "status restore-check ok=false nêu tệp thiếu" grep -q 'thiếu trong chuỗi backup' \
  <<<"$(sh_in 'mc cat t/inventory-backups/status/werag-restore-check.json' | jq -r .error)"
sh_in 'rm -rf /work/objects/10000/gone'

echo "== 10b. > 10 tệp DB tham chiếu mất khỏi bucket nguồn → lỗi"
docker exec -i "$PG" psql -qX -U postgres -c "INSERT INTO resources (physical_path, provider, state)
  SELECT 'storage://b1/minio://werag/10000/lost/' || g || '.html', 'minio', 'active' FROM generate_series(1, 11) g" >/dev/null
rc_out=$(run "${ENV_SRC[@]}" --entrypoint bash "$IMAGE" -c '
  set -Eeuo pipefail; source /usr/local/bin/backup.sh restore-check
  mkdir -p "$TMP"; ERRLOG=$TMP/t.stderr; exec 3>&2 2>"$ERRLOG"
  mc alias set backup "$BACKUP_S3_ENDPOINT" "$BACKUP_S3_ACCESS_KEY" "$BACKUP_S3_SECRET_KEY" >/dev/null
  objects_db_refs postgres /tmp/refs; check_objects /tmp/refs; echo UNEXPECTED_OK' 2>&1) || true
check "check_objects báo lỗi" bash -c '! grep -q UNEXPECTED_OK <<<"$1"' _ "$rc_out"
check "status nêu mất ở nguồn" grep -q 'không còn trong bucket nguồn' \
  <<<"$(sh_in 'mc cat t/inventory-backups/status/werag-restore-check.json' | jq -r .error)"
docker exec "$PG" psql -qX -U postgres -c "DELETE FROM resources WHERE physical_path LIKE '%/lost/%'" >/dev/null

echo "== 11. archive trên MinIO bị thay → sha256 lệch → lỗi"
sh_in 'k=$(head -1 /work/state/objects.chain.tsv | cut -f1); echo junk | mc pipe -q "t/inventory-backups/$k" >/dev/null'
rc_out=$(run "${ENV_SRC[@]}" --entrypoint bash "$IMAGE" -c '
  set -Eeuo pipefail; source /usr/local/bin/backup.sh restore-check
  mkdir -p "$TMP"; ERRLOG=$TMP/t.stderr; exec 3>&2 2>"$ERRLOG"
  mc alias set backup "$BACKUP_S3_ENDPOINT" "$BACKUP_S3_ACCESS_KEY" "$BACKUP_S3_SECRET_KEY" >/dev/null
  objects_db_refs postgres /tmp/refs; check_objects /tmp/refs; echo UNEXPECTED_OK' 2>&1) || true
check "sha256 lệch bị phát hiện" bash -c '! grep -q UNEXPECTED_OK <<<"$1"' _ "$rc_out"
check "status nêu sha256" grep -q 'sha256' <<<"$(sh_in 'mc cat t/inventory-backups/status/werag-restore-check.json' | jq -r .error)"

echo "== 12a. bucket giảm > 20 % → ok=false; BACKUP_OBJECTS_ALLOW_SHRINK=1 → ok"
sh_in 'mc rm -q t/werag/10000/c/big.bin t/werag/10003/b/new.html >/dev/null'
job "${ENV_SRC[@]}" && bad "job phải thoát lỗi" || ok "thoát lỗi"
check "ok=false" [ "$(field .ok)" = false ]
check "bản sao vẫn 5 tệp" [ "$(sh_in 'find /work/objects -type f | wc -l')" = 5 ]
job "${ENV_SRC[@]}" -e BACKUP_OBJECTS_ALLOW_SHRINK=1 && ok "thoát 0 khi cho phép" || bad "thoát lỗi: $(field .error)"
check "objects=3 removed=2" [ "$(field '"\(.objects) \(.removed)"')" = "3 2" ]

echo "== 12b. on_error xoá DB tạm của restore-check"
docker exec "$PG" psql -qX -U postgres -c 'CREATE DATABASE "postgres_restore_check"' >/dev/null
run --entrypoint bash "$IMAGE" -c '
  set -Eeuo pipefail; source /usr/local/bin/backup.sh restore-check
  mkdir -p "$TMP"; ERRLOG=$TMP/t.stderr; exec 3>&2 2>"$ERRLOG"; pg_env
  mc alias set backup "$BACKUP_S3_ENDPOINT" "$BACKUP_S3_ACCESS_KEY" "$BACKUP_S3_SECRET_KEY" >/dev/null
  RESTORE_SCRATCH=postgres_restore_check; fail "giả lập lỗi"' >/dev/null 2>&1 || true
check "DB tạm đã bị xoá" [ -z "$(docker exec "$PG" psql -qAtX -U postgres -c "SELECT 1 FROM pg_database WHERE datname = 'postgres_restore_check'")" ]

echo "== 12c. job wal: spool rỗng và spool có file (status giữa vòng lặp mỗi lần)"
wal() { run -e BACKUP_SPOOL=/work/spool "$@" --entrypoint backup.sh "$IMAGE" wal >/dev/null 2>&1; }
sh_in 'mkdir -p /work/spool && rm -f /work/state/wal.status-epoch'
wal && ok "spool rỗng: thoát 0" || bad "spool rỗng: thoát lỗi"
check "wal ok=true" [ "$(sh_in 'mc cat t/inventory-backups/status/werag-wal.json' | jq -r .ok)" = true ]
wal && ok "spool rỗng lần 2 (không ghi status): thoát 0" || bad "spool rỗng lần 2: thoát lỗi"
sh_in 'for f in 000000010000000000000001 000000010000000000000002 000000010000000000000003; do head -c 1000 /dev/urandom > /work/spool/$f; done'
wal -e BACKUP_WAL_STATUS_EVERY_S=0 && ok "3 file: thoát 0" || bad "3 file: thoát lỗi"
check "wal note 3 file" grep -q '^3 file' <<<"$(sh_in 'mc cat t/inventory-backups/status/werag-wal.json' | jq -r .note)"
check "spool đã trống" [ -z "$(sh_in 'ls /work/spool')" ]
check "3 WAL trên MinIO" [ "$(sh_in 'mc ls -r t/inventory-backups/werag/wal/ | wc -l')" = 3 ]

echo "== 12. không còn tệp tạm trong /work/tmp"
check "/work/tmp chỉ còn stderr log" [ -z "$(sh_in 'ls /work/tmp | grep -v "\.stderr$" || true')" ]

echo
echo "PASS=$PASS FAIL=$FAILS"
[ "$FAILS" -eq 0 ]
