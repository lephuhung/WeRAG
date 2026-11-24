# image-check — image có chạy đúng mã nguồn không

Mọi image dựng từ repo này (bảng trong `scripts/image-sources.sh`: app, sandbox,
docreader, frontend, sherpa-asr, screenai-ocr, backup, vbpl-sync, dcs-sync,
embed-rerank, tvpl) mang nhãn `werag.src-stamp`: hash các cây/tệp git của
đúng những đường dẫn đi vào image đó (đọc từ `COPY`/context của từng
Dockerfile). Hash chỉ đổi khi một trong các đường dẫn đó đổi, nên không cần
nhớ tăng số phiên bản.

`scripts/check-images.sh` so nhãn của image mà mỗi container (đang chạy hoặc
đã dừng) được tạo ra với hash của HEAD:

- `ok`: cùng hash;
- `stale`: khác hash — image cũ hơn (hoặc dựng từ cây có thay đổi chưa
  commit, hash có đuôi `-dirty-…`); cần dựng lại;
- `unstamped`: không có nhãn — image dựng trước khi có cơ chế này; cần dựng lại;
- `not running`: máy này không có container của service đó (không xét).

Mã thoát 0 khi không có `stale`/`unstamped`, 1 khi có, 2 khi lỗi docker.
`--json` in bản tóm tắt máy đọc. `WERAG_IMAGE_CHECK_SKIP` (danh sách cách
nhau bằng dấu phẩy) bỏ qua các service không dựng từ repo trên máy này: trên
máy này đặt `WERAG_IMAGE_CHECK_SKIP=app,sandbox` trong tệp env bên dưới (app
không triển khai từ repo ở đây, sandbox chỉ là image không có container).

Hash của Go image bỏ qua `*_test.go` và `testdata/`. Riêng vbpl-sync cố ý
thu hẹp vào mã của chương trình nhập và mã pháp lý nó gọi (`legal_*.go` của
types/repository/service, `internal/legalcatalog`, `internal/vbpl`,
`internal/vietnamese_legal/*.go`, …) để không báo cũ sau gần như mọi commit
backend; thay đổi hành vi nằm ngoài các đường dẫn đó thì phải tăng
`legalcatalog.ImporterRevision` (lưới thứ hai, cảnh báo `image_stale`).

`--report` ghi lần kiểm tra vào ứng dụng (worker `image_check`, trang
SuperAdmin Pháp lý → Thu thập dữ liệu) qua `deploy/dcs-sync/run_track.py`:
tất cả khớp → lần chạy `ok`; có image cũ → lần chạy lỗi, dòng cuối là
"image cũ hơn mã nguồn: docreader, frontend (chưa có nhãn: sherpa-asr)", và
SuperAdmin nhận cảnh báo `legal_sync_alert` như các nguồn khác. Quá 26 giờ
không có lần kiểm tra nào thì cũng cảnh báo (cron mất). Cần `WERAG_API`
(mặc định `http://127.0.0.1:18080/api/v1`) và `WERAG_API_KEY`: API key của
tổ chức nền tảng (10000) có quyền ingest, như dcs-sync. Khoá không ghi trong
crontab: script đọc tệp `WERAG_IMAGE_CHECK_ENV` (mặc định
`~/.werag-secrets/image-check.env`, quyền 0600) khi có:

```bash
mkdir -p ~/.werag-secrets && chmod 700 ~/.werag-secrets
printf 'WERAG_API_KEY=%s\nWERAG_IMAGE_CHECK_SKIP=app,sandbox\n' 'sk-…' > ~/.werag-secrets/image-check.env
chmod 600 ~/.werag-secrets/image-check.env
```

Cron trên host (hằng ngày và sau mỗi lần khởi động):

```cron
17 6 * * * cd /home/WeRAG && scripts/check-images.sh --report >>/home/orca/logs/image-check.log 2>&1
@reboot sleep 300 && cd /home/WeRAG && scripts/check-images.sh --report >>/home/orca/logs/image-check.log 2>&1
```

Dựng lại một image (luôn `-p spec-airag-refactor`, `--no-deps`, kèm nhãn và
`GIT_COMMIT`; tự dùng `sudo -n docker` khi `docker` không được phép):

```bash
scripts/build-image.sh docreader --up
```

**frontend**: thư mục `frontend-next/` thường có thay đổi dở dang của phiên
khác; dựng image frontend từ một bản checkout sạch của commit cần phát hành,
rồi chạy `scripts/warm-frontend.sh` sau `--up`. Dựng từ cây có thay đổi thì
hash có đuôi `-dirty-…` và lần kiểm tra sau báo `stale`.

Khi một Dockerfile đổi `COPY`, sửa bảng trong `scripts/image-sources.sh`
cho khớp: thiếu đường dẫn thì bỏ sót thay đổi, thừa thì báo cũ nhầm.
Kiểm thử: `bash scripts/check-images_test.sh`.
