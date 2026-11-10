# Văn bản của Đảng (dangcongsan.vn) → WeRAG

Lấy văn bản của Đảng từ hai cổng cùng một CMS và đẩy vào WeRAG:

* **tulieuvankien.dangcongsan.vn/he-thong-van-ban/van-ban-cua-dang**: 1998 →,
  khoảng 1.330 dòng, 30 dòng/trang;
* **dangcongsan.org.vn/van-ban-cua-dang**: 12/2024 →, khoảng 300 dòng, 12
  dòng/trang. Gần như trùng hẳn với Tư liệu; được gộp theo số hiệu + ngày.

Trang chi tiết chỉ có metadata (trích yếu, số hiệu, loại, lĩnh vực, ngày ban
hành/hiệu lực, cơ quan, người ký). Toàn văn nằm trong file đính kèm.

| Tệp | Việc |
|---|---|
| `dcs_fetch.py` | danh sách `?pageNo=N` → trang chi tiết → file đính kèm; mọi thứ được cache, mỗi request cách nhau ≥ `--delay` giây; tên file có dấu được mã hoá như trình duyệt |
| `dcs_classify.py` | đọc từng file: loại thật theo byte, PDF theo từng trang (chữ / scan / lớp OCR của máy scan / trắng), Word (chữ, ảnh dán), zip/rar, bảng mã tiếng Việt |
| `dcs_text.py` | chữ của Word (.docx, .doc kể cả bảng) và PDF có chữ; đổi font TCVN3 (.VnTime, .VnTimeH viết hoa) và VNI sang Unicode |
| `dcs_build.py` | gộp hai cổng, chọn nguồn nội dung, dựng HTML chuẩn v1 (`prov-*` khi có ≥ 2 Điều), ghi `plan.jsonl` |
| `dcs_push.py` | đẩy theo kế hoạch; `push-results.jsonl` ghi kết quả, chạy lại thì bỏ qua văn bản đã xong |
| `test_dcs.py` | `python3 -I -m unittest discover -s scripts/dangcongsan -p 'test_*.py'` |

## Các trường hợp

Mỗi văn bản có đúng một **case**:

| case | Khi nào | Đẩy thế nào |
|---|---|---|
| `text` | mọi phần đều có chữ thật: Word, hoặc PDF sinh từ máy (Aspose…) | `POST /system/legal-documents/push`, mode `full`: dòng danh mục (chưa xác minh) và toàn văn vào kho **"Văn bản của Đảng"** |
| `ocr` | có phần là bản scan: không có lớp chữ, hoặc có lớp OCR của máy scan (PaperPort, OmniPage…; lớp này **không bao giờ** được dùng vì toàn lỗi kiểu "QUYET D!NH chfrc näng") | `POST /system/legal-documents/push-scan` với các PDF ghép theo thứ tự (văn bản chính rồi phụ lục): dòng danh mục và một **bản nháp OCR** để SuperAdmin duyệt tại `/platform/admin/legal-drafts/{id}`; văn bản không có Điều vẫn duyệt được |
| `catalogue` | không có file, link hỏng (404 ở nguồn), file không dùng được | `push` mode `catalogue`: chỉ dòng danh mục |
| `skip` | văn bản Nhà nước lọt vào danh sách (`1225/QĐ-TTg`, `35/2021/QH15`), văn bản không số | không đẩy |

Quy tắc chọn phần:

* **Word + bản scan ký số của cùng văn bản** (Word ghi đúng số hiệu ở phần
  đầu): một phần, đọc chữ từ Word.
* **Phụ lục, đề cương, biểu mẫu** (phần đầu là "PHỤ LỤC", "Kèm theo…", "ĐỀ
  CƯƠNG", "Mẫu…", hoặc tên file có `phu_luc`, `de_cuong`…): nối sau văn bản
  chính trong `<div class="doc-region-appendix">`.
* Thành viên của zip/rar được đọc như file riêng. Host này không giải nén
  được RAR5: 7z không có bộ giải RAR, nên các file đó báo lỗi trích xuất. Cần
  `unrar` trong môi trường chạy.
* docx hỏng, trang HTML trả về thay cho PDF: không dùng được.

Định danh — **văn bản là chuẩn**, trang danh sách thì không:

* Số hiệu viết sai dạng được chuẩn hoá: `76/KL/TW`, `36/NQ-TW`, `08/CT-TW`
  thành `76-KL/TW`… `Ð` (eth) đổi thành `Đ`, bỏ khoảng trắng.
* Danh sách ghi ngày vào chỗ số hiệu, hoặc thiếu ngày: lấy lại từ địa chỉ trang
  (`…-so-245-qd-tw-ngay-24-01-2025-…`).
* Loại văn bản: ký hiệu trong số hiệu quyết định (KL = Kết luận, KH = Kế
  hoạch, CV = Công văn…; trang hay ghi "Văn bản khác"). Riêng `QĐ` của Đảng
  vừa là Quyết định vừa là Quy định, nên theo loại trên trang, hoặc theo
  tiêu đề văn bản.
* Văn bản có chữ: số hiệu, tiêu đề loại và ngày ở phần đầu văn bản ("Số
  40-HD/BTCTW", "Hà Nội, ngày 23 tháng 5 năm 2026") thắng trang danh sách;
  chỗ lệch ghi vào `identity_conflicts`. Với bản scan, người duyệt nháp so
  với PDF.
* Văn bản Đảng không có trên vbpl.vn, nên tình trạng hiệu lực là "Chưa xác
  định" (không bao giờ tự gọi là còn hiệu lực).

## Chạy

```bash
python3 -m venv /tmp/dcs-venv && /tmp/dcs-venv/bin/pip install pymupdf beautifulsoup4 lxml olefile
D=/home/orca/dcs-data            # cache + kết quả, ngoài repo
S=scripts/dangcongsan
/tmp/dcs-venv/bin/python -I $S/dcs_fetch.py --cache-dir $D/cache --out-dir $D/run --site tulieu
/tmp/dcs-venv/bin/python -I $S/dcs_fetch.py --cache-dir $D/cache --out-dir $D/run-org --site org
/tmp/dcs-venv/bin/python -I $S/dcs_classify.py $D/run/records.jsonl $D/run/classified.jsonl
/tmp/dcs-venv/bin/python -I $S/dcs_classify.py $D/run-org/records.jsonl $D/run-org/classified.jsonl
/tmp/dcs-venv/bin/python -I $S/dcs_build.py $D/run/classified.jsonl $D/run-org/classified.jsonl --out-dir $D/build
/tmp/dcs-venv/bin/python -I $S/dcs_push.py $D/build --dry-run
WERAG_API_KEY=sk-... /tmp/dcs-venv/bin/python -I $S/dcs_push.py $D/build --case text --limit 5
```

Cập nhật văn bản mới: `dcs_fetch.py --refresh-lists` (trang danh sách tải
lại, trang chi tiết và file đã có lấy từ cache), rồi classify → build → push.
Văn bản đã đẩy thành công thì được bỏ qua.

Trước khi đẩy lần đầu: tạo kho công khai tên đúng **"Văn bản của Đảng"**
(SuperAdmin). API key phải được tạo trong tổ chức nền tảng 10000, có quyền
`ingest`, và có kho đó trong danh sách KB.

robots.txt của hai cổng (bản chép chung) chặn bot tải `*.pdf`/`*.doc`.
Người dùng đã đồng ý tải chậm (một request mỗi lần, ≥ 0,7 giây) để dùng nội
bộ ngày 2026-11-10.
