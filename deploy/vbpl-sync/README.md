# vbpl-sync — đồng bộ danh mục văn bản pháp luật từ vbpl.vn

Container chạy `sync.sh` theo lịch (`VBPL_SYNC_CRON`, mặc định 02:30 giờ VN):

1. thư mục làm việc `/work` trống → khôi phục từ MinIO;
2. `crawl_vbpl.py`: liệt kê toàn bộ văn bản trung ương, chỉ tải lại văn bản mới
   hoặc thay đổi (metadata, toàn văn HTML trừ văn bản hết hiệu lực toàn bộ,
   quan hệ);
3. `legal-catalog-import`: nạp vào `legal_documents` / `legal_document_relations`;
   văn bản chưa hết hiệu lực toàn bộ mà vbpl.vn không có toàn văn và kho công khai
   chưa có bản nào được đưa vào hàng chờ đề nghị bổ sung (`source=vbpl_no_text`,
   tối đa 500 văn bản mỗi đêm) để lấy từ thuvienphapluat.vn bằng extension;
   văn bản có HTML mà kho chưa có, hoặc toàn văn trên cổng đã đổi so với bản
   trong kho, thì backend tự nạp (`status=auto_ingest`);
   hiệu lực trong danh mục và trên văn bản đã nạp luôn theo vbpl.vn;
   ngay sau đó đọc lại quan hệ ghi trong toàn văn (nhãn "toàn văn") của các
   nguồn `VBPL_TEXT_RELATION_SOURCES` (mặc định `dangcongsan,tvpl,vbpl`; đặt
   rỗng `VBPL_TEXT_RELATION_SOURCES=` thì bỏ qua) bằng
   `legal-catalog-import -rebuild-text-relations`, để lần nạp không bao giờ
   làm mất đồ thị. Backend tự đọc quan hệ toàn văn khi bản được duyệt của một
   văn bản phân tích xong hoặc khi SuperAdmin duyệt; bước này là lưới an toàn
   hằng đêm cho mọi văn bản mà các hook đó bỏ sót, của mọi nguồn (văn bản vbpl
   chỉ giữ cạnh "succeeds"; riêng nguồn `vbpl` mất khoảng 5 phút cho ~11 nghìn
   văn bản có bản trong kho). Bước này lỗi thì cả lần chạy ghi là lỗi (cố ý).
   **Sửa `cmd/legal-catalog-import` hay `internal/legalcatalog` thì phải build
   lại image này**: image dịch sẵn chương trình nhập, backend chạy bằng air
   không kéo theo nó (xem "Image cũ" bên dưới);
4. `mc mirror` thư mục làm việc lên MinIO (`$VBPL_S3_BUCKET/$VBPL_S3_PREFIX`).

Mỗi lần chạy được ghi vào bảng `legal_sync_runs` (migration 000198) qua các cờ
`-run-*` của `legal-catalog-import`: bước đang chạy (chuẩn bị → cào → nạp →
sao lưu MinIO), thời gian từng bước, số liệu của crawler (`--summary`) và của
import, và khi lỗi là đoạn cuối output của bước hỏng. Ghi nhận không bao giờ làm
hỏng lần đồng bộ: không mở được lần chạy (chưa migrate, mất kết nối) thì chạy
tiếp không ghi và ghi lại lúc kết thúc (`"untracked": true`), để một đêm chạy
tốt không bị coi là quá hạn. Mỗi lệnh ghi có giới hạn (kết nối 10 giây, cả lệnh
`VBPL_RUN_TRACK_TIMEOUT`, mặc định 60 giây). Trong lúc chạy, cứ
`VBPL_RUN_HEARTBEAT_S` giây (mặc định 300) sync.sh báo còn sống; lần chạy im
lặng quá 30 phút bị backend đóng là "Bị ngắt" (nếu chỉ là mất kết nối cơ sở dữ
liệu, lần chạy tự mở lại khi sync.sh ghi tiếp).

Dừng container (`docker stop`, `compose down`/`up` lại): supercronic không chuyển
tín hiệu cho job, nên `entrypoint.sh` (PID 1, giữ supercronic làm tiến trình con)
bắt SIGTERM/SIGINT, gửi SIGTERM cho sync.sh đang chạy (lần chạy theo lịch hoặc
lần chạy lúc khởi động `VBPL_SYNC_ON_START=1`), chờ nó xong (tối đa 80 giây) rồi
mới dừng supercronic; compose đặt `stop_grace_period: 90s`. sync.sh chạy trong
process group riêng (`setsid`), nhận SIGTERM thì dừng ngay bước đang chạy cùng
mọi tiến trình con và ghi lần chạy là lỗi "stopped". `docker exec … kill` vào
sync.sh cũng vậy. Lần chạy bị giết hẳn (SIGKILL, hết thời gian chờ) được lần
sau hoặc backend (30 phút im lặng) đóng là lỗi. Lịch sử giữ 180 ngày.
Kiểm thử bằng binary giả (sync.sh và entrypoint.sh, gồm chuyển tiếp SIGTERM):
`bash deploy/vbpl-sync/sync_test.sh`.
SuperAdmin xem ở Quản trị hệ thống → Pháp lý → Thu thập dữ liệu
(`/platform/admin?section=legal-documents&view=sync`, cùng worker tự nạp và
xoá toàn văn hết hiệu lực của backend), và nhận thông báo trong app
`legal_sync_alert` khi lần chạy gần nhất lỗi, chạy quá 12 giờ, hoặc 26 giờ không
có lần thành công (lịch mặc định hằng ngày; lịch thưa hơn sẽ bị coi là quá hạn).
Trong lúc lần chạy kế tiếp đang chạy, sự cố của đêm trước vẫn giữ nguyên; chỉ
lần chạy thành công mới đóng cảnh báo. Cố ý dừng đồng bộ thì tắt cảnh báo bằng
thiết lập hệ thống `legal.sync_alert.enabled` (cùng chỗ với
`legal.expired_retention_days`, nhóm legal; mặc định bật); tắt thì cảnh báo đang mở được đóng ở lần kiểm tra
sau (10 phút).

Dữ liệu nằm ở bucket riêng `werag-vbpl` (prefix `vbpl/`) trên MinIO dùng chung
10.10.0.123, user `werag-vbpl` toàn quyền trên riêng bucket này (tạo 2026-10-12
bằng tài khoản admin MinIO của inventory, như `provision_backup_storage.py`).
Đây là dữ liệu dùng chung, không phải backup: không object lock, được xoá
(`VBPL_S3_REMOVE=1`). Không có `VBPL_S3_*` thì compose rơi về khoá backup
(chỉ ghi thêm, phải để `VBPL_S3_REMOVE=0`).

```bash
# build + chạy: scripts/build-image.sh vbpl-sync --up (kèm GIT_COMMIT và nhãn
# werag.src-stamp, xem deploy/image-check); tương đương bằng tay (không nhãn):
GIT_COMMIT=$(git rev-parse --short HEAD) docker compose -p spec-airag-refactor -f docker-compose.yml --profile vbpl-sync build vbpl-sync && docker compose -p spec-airag-refactor -f docker-compose.yml --profile vbpl-sync up -d --no-deps vbpl-sync
# chạy một lần ngay
sudo docker exec WeKnora-vbpl-sync sync.sh
# log
sudo docker logs -f WeKnora-vbpl-sync
```

Biến `.env` (đều tuỳ chọn; mặc định theo `BACKUP_S3_*`): `VBPL_S3_ENDPOINT`,
`VBPL_S3_BUCKET`, `VBPL_S3_PREFIX`, `VBPL_S3_ACCESS_KEY`, `VBPL_S3_SECRET_KEY`,
`VBPL_S3_REMOVE=1` (xoá trên MinIO HTML của văn bản đã hết hiệu lực; cần khoá
có quyền xoá — khoá backup chỉ ghi thêm), `VBPL_SYNC_CRON`, `VBPL_SYNC_ON_START=1` (chạy ngay khi khởi động),
`VBPL_WORK_DIR` (thư mục host, mặc định `/home/orca/data/vbpl`),
`VBPL_UID`/`VBPL_GID` (chủ thư mục đó), `VBPL_CRAWL_ARGS` (thêm tham số crawler), `VBPL_TEXT_RELATION_SOURCES` (nguồn đọc lại quan hệ toàn văn sau mỗi lần nạp, mặc định `dangcongsan,tvpl,vbpl`; compose dùng `${VBPL_TEXT_RELATION_SOURCES-…}` nên giá trị rỗng vẫn tắt được).

## Không xoá dữ liệu tốt vì đầu vào đêm nay rỗng hoặc thiếu

- **Crawl**: toàn văn đã lưu chỉ bị xoá khi văn bản chuyển "Hết hiệu lực toàn
  bộ"; cổng trả nội dung rỗng cho văn bản còn hiệu lực thì giữ bản cũ (đếm
  `empty_content_kept`, lấy lại đêm sau). Quá max(200, 2 %) văn bản cùng chuyển
  hết hiệu lực trong một lần thì không xoá bản nào (`mass_expiry_held`), vẫn
  xuất file, nạp và sao lưu, rồi ghi lần chạy là lỗi. Đã kiểm tra là thật thì
  chạy một lần: `sudo docker exec -e VBPL_CRAWL_ARGS=--allow-mass-expiry WeKnora-vbpl-sync sync.sh`.
- **Chuẩn bị**: không có `state.sqlite` mà MinIO trả lỗi khác "không tồn tại"
  (mạng, khoá) thì dừng, không crawl lại từ đầu. Có `state.sqlite` mà thiếu
  `html/` thì khôi phục `html/` từ MinIO (mirror, không xoá, không ghi đè),
  không được thì dừng trước khi crawl.
- **Sao lưu với `VBPL_S3_REMOVE=1`**: đếm HTML trên MinIO không còn ở máy;
  quá `VBPL_S3_REMOVE_MAX` (mặc định max(200, 2 % số HTML trên MinIO)) thì
  mirror không `--remove` và ghi lần chạy là lỗi. Xoá thật thì chạy một lần
  `sudo docker exec -e VBPL_S3_REMOVE_FORCE=1 WeKnora-vbpl-sync sync.sh`.

## Quan hệ vbpl.vn: cập nhật theo chênh lệch, chặn xoá hàng loạt

Mỗi lần nạp, quan hệ của vbpl.vn (không phải cạnh "toàn văn", không phải cạnh
xuất phát từ văn bản nguồn khác) được cập nhật theo chênh lệch với danh sách
của đêm đó: chỉ xoá cạnh không còn trong danh sách, chỉ thêm cạnh mới, sửa
nhãn đã đổi; cạnh không đổi giữ nguyên `created_at`. Số liệu ghi vào run
(`import.relations_added` / `relations_removed` / `relations_relabelled` /
`relations_unchanged`).

Nếu chênh lệch phải **xoá quá max(1000, 10 %)** số cạnh vbpl.vn hiện có, lần
nạp từ chối (file crawl hỏng hoặc thiếu), không ghi gì, bước nạp lỗi và lần
chạy được ghi là lỗi. Khi đã kiểm tra là xoá thật (vbpl.vn gỡ hàng loạt quan
hệ), chạy lại bước nạp một lần bằng tay với `-allow-relation-removal` (cùng
file của đêm đó; lần chạy tay không ghi vào `legal_sync_runs`):

```bash
sudo docker exec WeKnora-vbpl-sync sh -c 'legal-catalog-import -documents /work/documents.jsonl \
  -relations /work/relations.jsonl -snapshot "$(date -u +%F)" -enqueue-no-fulltext -auto-ingest-html \
  -allow-relation-removal && legal-catalog-import -rebuild-text-relations dangcongsan,tvpl,vbpl'
```

Đêm sau chạy bình thường (không có cờ).

## Image cũ

Image dịch sẵn `legal-catalog-import` lúc build: sửa mã nguồn mà không build
lại thì đêm nào cũng chạy mã cũ (đã xoá quan hệ toàn văn ba đêm liền). Mỗi lần
chạy ghi vào summary `importer_revision` (hằng `legalcatalog.ImporterRevision`
trong `internal/legalcatalog/revision.go`) và `image_commit` (build arg
`GIT_COMMIT` → `WERAG_IMAGE_COMMIT`, nhãn `org.opencontainers.image.revision`;
bỏ trống khi build không truyền `GIT_COMMIT`). Trang Thu thập dữ liệu và cảnh
báo `legal_sync_alert` báo `image_stale` khi lần chạy mới nhất của `vbpl_sync`
ghi bản thấp hơn bản của backend, hoặc không ghi bản nào (image dựng trước
khi có dấu này); lần chạy đầu tiên của image dựng lại sẽ xoá cảnh báo ngay khi
bắt đầu (`sudo docker exec WeKnora-vbpl-sync sync.sh` để không phải chờ đêm).

**Tăng `ImporterRevision`** mỗi khi đổi hành vi của `cmd/legal-catalog-import`,
`internal/legalcatalog`, đường ghi quan hệ (repository `ReplaceRelations`,
`mergeRelations`) hoặc `service.BuildLegalTextRelations`, rồi build lại image
bằng lệnh ở trên.

Lần crawl đầu tiên khoảng 5 giờ (≈18 nghìn văn bản, ~1 văn bản/giây); các
lần sau chỉ vài phút đến vài chục phút. HTML có ảnh nhúng: vài GB trên MinIO.
