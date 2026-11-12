# dcs-sync — văn bản của Đảng, cập nhật theo lịch

Container chạy `sync.sh` theo lịch supercronic (mặc định `15 */4 * * *`, giờ
Việt Nam). Mỗi lần chạy:

1. tải lại `DCS_REFRESH_PAGES` (mặc định 3) trang danh sách đầu của
   tulieuvankien.dangcongsan.vn và dangcongsan.org.vn, cùng trang chi tiết
   của chúng. Văn bản mới, và tệp được gắn thêm sau khi đăng, đều nằm ở đầu
   danh sách; trang sâu hơn và tệp đã tải lấy từ cache;
2. phân loại tệp, gộp hai cổng, dựng HTML (`scripts/dangcongsan`);
3. đẩy phần chưa đẩy, theo sổ `build/push-results.jsonl`:
   - văn bản có chữ → toàn văn vào kho **"Văn bản của Đảng"**;
   - bản scan hoặc không có tệp → dòng danh mục và một đề nghị
     `dangcongsan_no_text` trong hàng chờ của extension TVPL;
   - một văn bản trước chỉ có scan, sau đó cổng gắn thêm tệp Word: lần sau
     văn bản thành case `text` và được nạp toàn văn; đề nghị đang mở tự đóng.

Thư mục làm việc `DCS_WORK_DIR` (mặc định `/home/orca/dcs-data`) được gắn vào
container **cùng đường dẫn**, vì bản ghi crawl lưu đường dẫn tuyệt đối của
tệp trong cache. Cấu trúc thư mục:

- `cache/`
- `run-tulieu/`, `run-org/`: bản ghi và kết quả phân loại
- `build/`: `plan.jsonl`, `html/`, `push-results.jsonl`
- `push.log`

## Cấu hình (`.env`)

| Biến | Mặc định | |
|---|---|---|
| `DCS_API_KEY` | — | API key tạo trong tổ chức 10000 (`POST /tenants/10000/api-keys`), capability `ingest`, `knowledge_base_ids` = kho "Văn bản của Đảng" |
| `DCS_WERAG_API` | `http://app:8080/api/v1` | dev hot-reload: `http://WeKnora-app-dev:8080/api/v1` |
| `DCS_SYNC_CRON` | `15 */4 * * *` | |
| `DCS_SYNC_ON_START` | `0` | `1`: chạy ngay khi container khởi động |
| `DCS_REFRESH_PAGES` | `3` | |
| `DCS_DELAY` | `1` | giây giữa hai request tới cổng |

```bash
sudo docker compose --profile dcs-sync up -d --build dcs-sync
sudo docker exec WeKnora-dcs-sync sync.sh        # chạy một lần ngay
sudo docker logs -f WeKnora-dcs-sync
```

Container healthy khi lần đồng bộ thành công gần nhất cách đây chưa đến
1 ngày. Chưa ghi vào `legal_sync_runs`: việc này nối vào trang theo dõi
"Thu thập dữ liệu" khi nhánh legal-sync-monitor được merge.

Muốn tải lại toàn bộ danh sách thì chạy tay
`dcs_fetch.py --refresh-lists` (xem `scripts/dangcongsan/README.md`).
