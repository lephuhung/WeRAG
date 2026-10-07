---
name: the-thuc-quyet-dinh
description: Yêu cầu thể thức riêng của quyết định (NĐ30/2020/NĐ-CP) — tên loại QUYẾT ĐỊNH, trích yếu, thẩm quyền ban hành, căn cứ, "QUYẾT ĐỊNH:", bố cục Điều, nơi nhận "Như Điều ...".
---

# Quyết định

Áp dụng cùng skill `the-thuc-chung`.

## Phần mở đầu

- **Tên loại** `QUYẾT ĐỊNH` (in hoa, đậm, căn giữa), dòng dưới là **trích yếu** chữ thường, đậm, căn giữa: "Về việc ...", "Ban hành ...", "Phê duyệt ...".
- **Thẩm quyền ban hành**: dòng in hoa, đậm, căn giữa ghi chức vụ người đứng đầu hoặc tên cơ quan (ví dụ `GIÁM ĐỐC SỞ Y TẾ`, `ỦY BAN NHÂN DÂN TỈNH HÀ TĨNH`, `CHỦ TỊCH ỦY BAN NHÂN DÂN ...`). Thiếu → lỗi. Phải khớp người ký: thẩm quyền là "ỦY BAN NHÂN DÂN" thì ký `TM. ỦY BAN NHÂN DÂN`; thẩm quyền "CHỦ TỊCH ..." thì ký `CHỦ TỊCH` hoặc `KT. CHỦ TỊCH`.
- **Căn cứ ban hành**: in nghiêng; mỗi căn cứ một dòng, bắt đầu "Căn cứ ...", kết thúc bằng `;`, căn cứ cuối kết thúc bằng `.`; căn cứ pháp lý xếp trước, "Theo đề nghị của ..." (hoặc "Xét đề nghị") đứng cuối. Căn cứ văn bản phải ghi đủ loại, số ký hiệu, ngày, cơ quan ban hành, trích yếu.
- Dòng `QUYẾT ĐỊNH:` in hoa, đậm, căn giữa trước Điều 1. Thiếu → lỗi.

## Nội dung

- Bố cục theo **Điều**: `Điều 1.` (đậm), `Điều 2.` …; đánh số liên tục.
- Điều 1 nêu nội dung quyết định; nếu ban hành kèm theo văn bản khác (quy chế, quy định, kế hoạch) phải ghi "ban hành kèm theo Quyết định này".
- **Điều cuối** giao trách nhiệm thi hành: "... chịu trách nhiệm thi hành Quyết định này." và hiệu lực ("Quyết định này có hiệu lực kể từ ngày ký" hoặc ngày cụ thể) — thiếu điều hiệu lực/thi hành → lỗi.

## Nơi nhận

- `- Như Điều <số điều thi hành>;` thay cho liệt kê lại các đối tượng thi hành; `- Lưu: VT, <đơn vị soạn thảo>.`
- Ký hiệu số phải có `QĐ-`.
