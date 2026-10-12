# vbpl-sync — đồng bộ danh mục văn bản pháp luật từ vbpl.vn

Container chạy `sync.sh` theo lịch (`VBPL_SYNC_CRON`, mặc định 02:30 giờ VN):

1. thư mục làm việc `/work` trống → khôi phục từ MinIO;
2. `crawl_vbpl.py`: liệt kê toàn bộ văn bản trung ương, chỉ tải lại văn bản mới
   hoặc thay đổi (metadata, toàn văn HTML trừ văn bản hết hiệu lực toàn bộ,
   quan hệ);
3. `legal-catalog-import`: nạp vào `legal_documents` / `legal_document_relations`;
4. `mc mirror` thư mục làm việc lên MinIO (`$VBPL_S3_BUCKET/$VBPL_S3_PREFIX`).

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
