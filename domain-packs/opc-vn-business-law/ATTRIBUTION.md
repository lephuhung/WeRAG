# opc-vn-business-law — nguồn gốc và giấy phép

## Nguồn

- Tác phẩm gốc: skill `opc-vn-business-law` của Diginno ("Diginno — opc-vn-business-law"), bản 1.0.2, mốc dữ liệu 17/10/2026.
- Lấy từ bản fork https://github.com/lephuhung/opc-vn-business-law (fork của https://github.com/diginno-net/opc-vn-business-law), commit `1cb90548d6d8385cffdfd475798b0cfd1c01688f`.
- Ngày nhập: 18/10/2026. Bản trong WeRAG: 1.1.0 (xem CHANGELOG).

## Giấy phép

- Nội dung (SKILL.md, references/, assets/, evals/): Creative Commons Attribution 4.0 International (CC BY 4.0), https://creativecommons.org/licenses/by/4.0/legalcode. Bản quyền © 2026 Diginno.
- Script gốc `scripts/so_sanh_thue.py`: MIT License, © 2026 Diginno. Không ship trong gói; công thức được viết lại bằng Go tại `internal/agent/domaintools/opcvn` (tool `compare_tax_methods`), kèm thông báo MIT sau:

  > MIT License — Copyright (c) 2026 Diginno. Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions: The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software. THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

- Trích văn bản quy phạm pháp luật (Điều 85 Luật Doanh nghiệp, Điều 20 khoản 8 dự thảo) không thuộc đối tượng bảo hộ quyền tác giả.
- Miễn trừ trách nhiệm của bản gốc được giữ dưới dạng `answer_footer` (do mã chèn cuối mỗi câu trả lời): nội dung tham khảo, không phải tư vấn pháp lý/thuế/kế toán, không bảo đảm.

## Những gì đã thay đổi (CC BY 4.0 yêu cầu nêu rõ)

Theo docs/domain-pack-standard.md §5 và §10:

- `SKILL.md`: viết lại. Giữ quy trình A–E, câu hỏi khai thác dữ kiện, phạm vi loại trừ, cấu trúc trả lời. Bỏ khối miễn trừ (→ `answer_footer`), mẫu câu trả lời cứng, mục "Cập nhật skill", bản đồ tới `00`/`08`/script, các câu "luôn/bắt buộc" về hành vi. Thêm tên tool WeRAG: `search_knowledge` (căn cứ), `check_legal_status`/`find_related_legal_documents` (hiệu lực), `compare_tax_methods` (so sánh thuế). Manifest theo §3.3.
- `references/00-legal-status.md`: không giữ. 13 số hiệu → `catalogue.yaml` (bỏ cột trạng thái và ngày hiệu lực); 3 dự thảo/dự án → `draft_documents`. Hai dự án luật (Kế toán – Kiểm toán độc lập; Thuế TNDN – TNCN – SDĐ phi nông nghiệp) không có lần/ngày dự thảo trong bản gốc ("chưa rõ"): manifest ghi `version: chưa rõ lần dự thảo` và không có `date` (validator cho phép bỏ ngày đúng trường hợp này; không bịa ngày). "Ba lưu ý" chuyển thành bình luận: lưu ý 1 và 3 → `02`, lưu ý 2 → `04`; mục "Khi nào phải nói không chắc" → `07`; mục "Cách kiểm tra nhanh" (toàn đường dẫn web) bỏ.
- `references/01`, `03`, `06`, `07`, `assets/*`: thêm dòng đầu "Bình luận tham khảo — không phải căn cứ"; bỏ tham chiếu tới `00-legal-status.md`, `DISCLAIMER.md`. `01`: bỏ câu hộ kinh doanh vượt ngưỡng lao động phải lên doanh nghiệp, bỏ số thành viên TNHH (2–50) và số cổ đông sáng lập (≥ 3) → tra KB; số điều Luật Doanh nghiệp ghi "theo bản gốc, kiểm tra văn bản sửa đổi". `03`: bỏ thời hạn 30 ngày vắng mặt của người đại diện và 10 năm lưu trữ → tra KB; Nghị định 01/2021/NĐ-CP ghi "có thể đã được sửa đổi hoặc thay thế". `06`, checklist: thủ tục đăng ký doanh nghiệp hiện hành phải tra KB; bỏ 90 ngày góp vốn (checklist, điều lệ mẫu). `06`: bỏ thời hạn xử lý hồ sơ cụ thể; đề xuất 01 ngày ghi là dự thảo Lần 2 ngày 28/8/2026. `07`: bỏ mục "Giới hạn trách nhiệm" (thay bằng footer).
- `references/02`: mọi chỗ nói tới Điều 20 (khoản 8 và các khoản khác), Điều 4, Điều 33–35 của dự thảo ghi "dự thảo Lần 2 ngày 28/8/2026". Bỏ bảng ngưỡng siêu nhỏ của Nghị định 80/2021/NĐ-CP và các trần của Luật Hỗ trợ DNNVV 2017 (số liệu đổi theo văn bản → tra KB). Ngưỡng/trần của chính dự thảo được giữ vì đã gắn nhãn dự thảo.
- `references/04`: bỏ bảng "Khung pháp lý" có cột trạng thái (IN FORCE/DRAFT), thay bằng danh sách văn bản không ghi trạng thái; ngày hiệu lực Thông tư 58/2026/TT-BTC (ở `02` và `04`) chỉ còn dạng "bản gốc ghi …; xác nhận bằng check_legal_status"; bỏ 10 năm lưu trữ → tra KB.
- `references/05`: bỏ bảng thuế suất TNDN theo doanh thu, mức lệ phí môn bài, số năm miễn môn bài, thuế suất TNCN trên lợi nhuận chia, các hạn nộp cụ thể; bỏ khối lệnh chạy script. **Sửa một lỗi của bản gốc**: bản gốc viết "biên lợi nhuận cao hơn biên hòa vốn thì kê khai có lợi hơn" — ngược với công thức và với self-test của chính script (8 tỷ doanh thu, biên 15 % > 7,5 % → tính theo % doanh thu có lợi hơn). Bản này viết đúng chiều: biên thấp hơn biên hòa vốn thì kê khai có lợi hơn. Ví dụ dùng số học thuần, không dùng mức thuế suất.
- `assets/dieu-le-tnhh-mtv-toi-gian.md`: thêm "Mẫu tham khảo, chưa đối chiếu Luật Doanh nghiệp hiện hành"; bỏ ký hiệu cảnh báo emoji.
- `scripts/so_sanh_thue.py` → tool Go `compare_tax_methods`: cả 4 tham số bắt buộc (bản gốc mặc định thuế suất 0,20 và tỷ lệ 1,5 — đã bỏ); khối "Giả định"; số tiền viết kiểu Việt Nam. Khác biệt có chủ ý: tỷ lệ % doanh thu chỉ nhận dạng phần trăm (0 < x < 100; 1.5 = 1,5 %, 0.5 = 0,5 %) — script nhận cả 1.5 lẫn 0.015 nên 0.5 thành 50 %; thêm kiểm tra NaN/vô cực và trần số tiền 10^15 đ; thuế suất hiển thị tới 2 chữ số thập phân (script làm tròn về số nguyên).
- `evals/evals.json` → `evals/cases.jsonl`: giữ 6 câu gốc (đánh số `orig-1`…`orig-6`), thêm câu bẫy dự thảo, thuế suất, trốn nợ cũ, ngoài phạm vi, hiệu lực văn bản; kỳ vọng tự do → kiểm tra nhị phân.
- Bỏ không ship: `README.md`, `PROMO.md` (quảng cáo nhóm và dịch vụ), `MAINTAINING.md`, `CHANGELOG.md`, `LICENSE` (nội dung giấy phép ghi ở đây), `DISCLAIMER.md` (→ footer), `references/08-sources.md` (nguồn ghi ở đây), `scripts/` (gồm `build_dist.sh`, `audit_public.sh`, `package_skill.sh`, `so_sanh_thue.py`), `.gitignore`.

## Nguồn chính thức bản gốc liệt kê (chỉ để SuperAdmin kiểm chứng, model không dùng)

vbpl.vn (Cơ sở dữ liệu quốc gia về pháp luật), vanban.chinhphu.vn, duthaoonline.quochoi.vn, xaydungchinhsach.chinhphu.vn, gdt.gov.vn, mof.gov.vn, dangkykinhdoanh.gov.vn, baohiemxahoi.gov.vn.

## Rà soát an toàn khi nhập (18/10/2026)

Bản gốc được clone vào thư mục riêng ngoài repo; không chạy script nào. Quét bằng ripgrep trên toàn bộ cây (trừ `.git`):

- URL / tên miền: có trong `DISCLAIMER.md`, `README.md`, `LICENSE`, `PROMO.md`, `MAINTAINING.md`, `references/00-legal-status.md`, `references/08-sources.md` — tất cả là trang cơ quan nhà nước, trang giấy phép, repo và kênh quảng bá của tác giả. Không tệp nào trong số đó được ship; tệp ship không còn URL (test §3.4 kiểm lại).
- Mẫu nghi chèn lệnh ("bỏ qua hướng dẫn", "ignore previous/above", "disregard", "system prompt", "lời nhắc hệ thống", "you are now", "act as", "developer mode", thẻ script, base64, curl/wget, eval/exec): **không có kết quả**.
- Ký tự ẩn (zero-width, điều khiển hướng chữ, BOM giữa tệp): **không có kết quả**.
- Lệnh thực thi trong văn bản: chỉ hai khối `python3 scripts/so_sanh_thue.py …` ở `references/05` và `08` — đã bỏ.
- Yêu cầu gọi tool lạ: không có (bản gốc không nhắc tool nào ngoài script của chính nó).
- Script gốc được đọc như dữ liệu để port công thức; `audit_public.sh`/`build_dist.sh`/`package_skill.sh` chỉ đóng gói và quét, không liên quan nội dung.

Lint của registry (`Pack.Lint`, chạy trong `TestShippedPacksAreValid`) trên gói đã chuẩn hoá: 0 phát hiện.

## CHANGELOG

- 1.1.0 (18/10/2026): theo kết quả đối chiếu danh mục trên môi trường dev ngày 18/10/2026 (13/13 có trong danh mục, 12 có toàn văn trong KB công khai). `as_of` giữ mốc của bản gốc (17/10/2026).
  - Bỏ 01/2021/NĐ-CP (danh mục: hết hiệu lực toàn bộ 01/07/2025, bị thay thế bởi 168/2025/NĐ-CP); thêm 168/2025/NĐ-CP. `03`, `06`, checklist dẫn "tra Nghị định 168/2025/NĐ-CP trong KB".
  - 58/2026/TT-BTC: danh mục ghi ngày hiệu lực khác bản gốc (01/7/2026) → văn bản ghi "bản gốc ghi …; danh mục ghi ngày khác — dùng check_legal_status".
  - 132/2018/TT-BTC: danh mục ghi còn hiệu lực, không có quan hệ thay thế; bản gốc cho rằng đã được 58/2026/TT-BTC thay thế → giữ trong danh mục, văn bản ghi rõ hai nguồn khác nhau.
  - Văn bản danh mục ghi "Hết hiệu lực một phần" (đã được sửa đổi): 59/2020/QH14, 88/2015/QH13, 133/2016/TT-BTC, 67/2025/QH15, 48/2024/QH15, 41/2024/QH15, 22/2023/QH15. Các luật sửa đổi không đưa vào danh mục; mỗi tệp trích điều của các văn bản này có ghi chú đọc bản hiện hành bằng `search_knowledge` và nêu sửa đổi. `articles` ghi các điều gói trích (59/2020/QH14, 88/2015/QH13, 174/2016/NĐ-CP, 80/2021/NĐ-CP, 22/2023/QH15); 67/2025, 48/2024, 41/2024, 133/2016 không có điều cụ thể trong bản gốc nên để trống.
  - Thêm `formulas.yaml` (4 công thức của `compare_tax_methods`, căn cứ 67/2025/QH15 không ghi điều vì bản gốc không nêu); 05 dẫn 48/2024/QH15 và 41/2024/QH15 như bảng trạng thái bản gốc.
  - Thêm "theo bản gốc" cho lộ trình 2025–2026 (01, 05, 07) và Nghị định 80/2021/NĐ-CP (02).
- 1.0.0 (18/10/2026): bản chuẩn hoá đầu tiên cho WeRAG từ bản gốc 1.0.2. Danh mục 13 số hiệu: 59/2020/QH14, 88/2015/QH13, 174/2016/NĐ-CP, 133/2016/TT-BTC, 58/2026/TT-BTC, 132/2018/TT-BTC, 80/2021/NĐ-CP, 67/2025/QH15, 48/2024/QH15, 198/2025/QH15, 41/2024/QH15, 01/2021/NĐ-CP, 22/2023/QH15.
