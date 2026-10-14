---
name: the-thuc-nghi-quyet
description: Yêu cầu thể thức riêng của nghị quyết cá biệt (NĐ30/2020/NĐ-CP) — tên loại NGHỊ QUYẾT, thẩm quyền ban hành của tập thể, căn cứ, "QUYẾT NGHỊ:", bố cục Điều, ký TM. tập thể.
---

# Nghị quyết

Áp dụng cùng skill `the-thuc-chung`.

## Phần mở đầu

- Dòng tên loại `NGHỊ QUYẾT`, dòng dưới là trích yếu.
- **Thẩm quyền ban hành** là tập thể (ví dụ `HỘI ĐỒNG NHÂN DÂN TỈNH HÀ TĨNH`); nghị quyết của HĐND ghi thêm dòng khóa, kỳ họp (`KHÓA XVIII, KỲ HỌP THỨ ...`).
- Căn cứ có thẻ `[nghiêng]`, mỗi căn cứ một dòng, kết thúc `;`, căn cứ cuối `.`; thường có "Xét Tờ trình số ... của ...; Báo cáo thẩm tra ...; ý kiến thảo luận của ...".
- Dòng `QUYẾT NGHỊ:` trước Điều 1 (dòng mốc "dòng QUYẾT ĐỊNH:/QUYẾT NGHỊ:"). Không có dòng mốc này → lỗi.

## Nội dung (xét trên các dòng mốc "Điều")

- Bố cục theo Điều, đánh số liên tục `Điều 1.`, `Điều 2.` … (hồ sơ liệt kê các dòng Điều; khi quá nhiều chỉ giữ các Điều đầu và Điều cuối — số bị bỏ giữa chừng do lược không phải lỗi).
- Điều cuối nêu tổ chức thực hiện và hiệu lực (nghị quyết HĐND ghi ngày thông qua: "Nghị quyết này đã được Hội đồng nhân dân ... khóa ..., kỳ họp thứ ... thông qua ngày ..."). Xét dòng Điều cuối và các dòng "đoạn cuối"; chỉ báo lỗi khi các dòng này hiện đủ mà không có ý hiệu lực/thi hành.

## Ký

- Ký **thay mặt tập thể**: `TM. HỘI ĐỒNG NHÂN DÂN` / `CHỦ TỊCH` (hoặc `TM. BAN THƯỜNG VỤ` …). Thiếu `TM.` → lỗi. Phó chủ tịch ký thì `TM. ...` / `KT. CHỦ TỊCH` / `PHÓ CHỦ TỊCH`.
- Ký hiệu số phải có `NQ-`.
