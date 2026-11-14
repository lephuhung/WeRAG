# dcs-sync — văn bản của Đảng, cập nhật theo lịch

Container chạy `sync.sh` theo lịch supercronic (mặc định `15 */4 * * *`, giờ
Việt Nam). Mỗi lần chạy có ba bước:

1. `fetch`: tải lại `DCS_REFRESH_PAGES` (mặc định 3) trang danh sách đầu của
   tulieuvankien.dangcongsan.vn và dangcongsan.org.vn, cùng trang chi tiết
   của chúng. Văn bản mới, và tệp được gắn thêm sau khi đăng, đều nằm ở đầu
   danh sách; trang sâu hơn và tệp đã tải lấy từ cache;
2. `build`: phân loại tệp, gộp hai cổng, dựng HTML (`scripts/dangcongsan`);
3. `upload`: đẩy phần chưa đẩy, theo sổ `build/push-results.jsonl`:
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
- `.sync.*`: khoá, output của bước đang chạy, số liệu, lỗi gửi lên trang theo dõi

## Cấu hình (`.env`)

| Biến | Mặc định | |
|---|---|---|
| `DCS_API_KEY` | — | API key tạo trong tổ chức 10000 (`POST /tenants/10000/api-keys`), capability `ingest`, `knowledge_base_ids` = kho "Văn bản của Đảng" |
| `DCS_WERAG_API` | `http://app:8080/api/v1` | dev hot-reload: `http://WeKnora-app-dev:8080/api/v1` |
| `DCS_SYNC_CRON` | `15 */4 * * *` | |
| `DCS_SYNC_ON_START` | `0` | `1`: chạy ngay khi container khởi động |
| `DCS_REFRESH_PAGES` | `3` | |
| `DCS_DELAY` | `1` | giây giữa hai request tới cổng |
| `DCS_RUN_TRACK_TIMEOUT` | `20` | giây cho mỗi request ghi nhận lần chạy (cả lệnh tối đa 3×) |
| `DCS_RUN_HEARTBEAT_S` | `300` | chu kỳ báo còn sống trong lúc chạy |

```bash
sudo docker compose --profile dcs-sync up -d --build dcs-sync
sudo docker exec WeKnora-dcs-sync sync.sh        # chạy một lần ngay
sudo docker logs -f WeKnora-dcs-sync
```

Container healthy khi lần đồng bộ thành công gần nhất cách đây chưa đến
1 ngày.

## Theo dõi lần chạy (trang "Thu thập dữ liệu", cảnh báo `legal_sync_alert`)

Container không có thông tin đăng nhập cơ sở dữ liệu: mỗi lần chạy được ghi vào
bảng `legal_sync_runs` (worker `dcs_sync`) qua API của backend, bằng chính
`WERAG_API` + `WERAG_API_KEY` của lệnh đẩy (`run_track.py`):

| Endpoint | Body |
|---|---|
| `POST /system/legal-sync/runs` | `{"worker":"dcs_sync","step":"fetch","summary":{"schedule":"15 */4 * * *"}}` → `{"data":{"id":N}}` |
| `POST /system/legal-sync/runs/{id}/step` | `{"step":"build","summary":{"fetch":{…}}}` |
| `POST /system/legal-sync/runs/{id}/heartbeat` | — |
| `POST /system/legal-sync/runs/{id}/finish` | `{"status":"ok"\|"failed","error":"…","summary":{"upload":{…}}}` |

Quyền: API key của tổ chức 10000 có capability `ingest` (hoặc full access) —
key `DCS_API_KEY` sẵn có là đủ, không cần tạo key mới — hoặc SuperAdmin đăng
nhập. Key của tổ chức khác, key thiếu `ingest`, người đẩy văn bản pháp luật,
quản trị tổ chức: 403. Chỉ worker `dcs_sync` được ghi qua API (vbpl-sync và
các worker của backend tự ghi); key không chạm được lần chạy của worker khác.

Ngữ nghĩa giống vbpl-sync: mở lần chạy mới thì lần chạy cũ còn treo được đóng
là "Bị ngắt"; mỗi lần ghi là một tín hiệu sống; trong lúc chạy cứ
`DCS_RUN_HEARTBEAT_S` giây sync.sh báo còn sống, lần chạy im lặng quá 30 phút bị
backend đóng là lỗi (tự mở lại nếu sync.sh ghi tiếp). Mỗi bước gửi kèm số liệu
mà script đã in:

- `fetch`: `records` (văn bản trong danh sách hai cổng), `loaded` (tải trong lần này);
- `build`: `planned`, `text`, `ocr`, `catalogue`, `skip`;
- `upload`: `pushed` (toàn văn), `queued` (vào hàng chờ extension TVPL),
  `existing` (409, đã có toàn văn), `refused` (400/422), `failed` (404/429/5xx,
  lỗi mạng: đẩy lại lần sau), `credentials_refused` khi API từ chối key.

Lần chạy lỗi ghi đoạn cuối output của bước hỏng (API key bị che, bỏ ký tự điều
khiển/mã màu; backend giữ 4000 byte cuối). Có lệnh đẩy lỗi (dcs_push thoát 4)
thì lần chạy là **lỗi** ở bước `upload`, đoạn lỗi chỉ gồm các dòng đẩy hỏng và
dòng tổng kết. Ghi nhận không bao giờ làm hỏng lần đồng bộ: không mở được lần
chạy (backend không tới được) thì chạy tiếp không ghi và thử ghi lại lúc kết thúc
(`"untracked": true`).

Lưu ý: backend không tới được thì cả lệnh đẩy lẫn việc ghi nhận đều hỏng (như
sự cố DNS 42 giờ khi container nằm sai network): không có lần chạy nào được ghi,
nên cảnh báo đến từ ngưỡng **quá hạn** (9 giờ không có lần thành công), không
phải từ lần chạy lỗi. Container chưa từng ghi lần chạy nào thì coi là chưa
triển khai: không cảnh báo.

Ngưỡng của `dcs_sync` (theo lịch 4 giờ): quá hạn khi 9 giờ không có lần chạy
thành công nào bắt đầu; chạy quá 2 giờ là có thể treo; im lặng 30 phút là bị
ngắt. Cảnh báo chung digest `legal_sync_alert` với vbpl-sync (ngưỡng riêng
12/26 giờ), tắt bằng thiết lập hệ thống `legal.sync_alert.enabled`. Lịch sử giữ
180 ngày.

Dừng container (`docker stop`, `compose up` lại): `entrypoint.sh` (PID 1) gửi
SIGTERM cho sync.sh đang chạy, chờ tối đa 80 giây; sync.sh (process group riêng)
dừng bước đang chạy cùng các tiến trình con và ghi lần chạy là lỗi "stopped".
Compose đặt `stop_grace_period: 90s`.

Kiểm thử bằng script và backend giả: `bash deploy/dcs-sync/sync_test.sh`.

Muốn tải lại toàn bộ danh sách thì chạy tay
`dcs_fetch.py --refresh-lists` (xem `scripts/dangcongsan/README.md`).
