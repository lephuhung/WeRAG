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
liệu, lần chạy tự mở lại khi sync.sh ghi tiếp). SIGTERM/SIGINT (`docker stop`)
dừng ngay bước đang chạy cùng mọi tiến trình con (sync.sh chạy trong process
group riêng qua `setsid`) và ghi lần chạy là lỗi "stopped"; lần chạy bị giết hẳn
(SIGKILL) được lần sau hoặc backend đóng là lỗi. Lịch sử giữ 180 ngày.
Kiểm thử bằng binary giả: `bash deploy/vbpl-sync/sync_test.sh`.
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
# build + chạy
sudo docker compose --profile vbpl-sync up -d --build vbpl-sync
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
`VBPL_UID`/`VBPL_GID` (chủ thư mục đó), `VBPL_CRAWL_ARGS` (thêm tham số crawler).

Lần crawl đầu tiên khoảng 5 giờ (≈18 nghìn văn bản, ~1 văn bản/giây); các
lần sau chỉ vài phút đến vài chục phút. HTML có ảnh nhúng: vài GB trên MinIO.
