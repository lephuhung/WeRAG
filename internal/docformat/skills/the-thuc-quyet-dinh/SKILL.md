---
name: the-thuc-quyet-dinh
description: Yêu cầu thể thức riêng của quyết định (NĐ30/2020/NĐ-CP) — tên loại QUYẾT ĐỊNH, trích yếu, thẩm quyền ban hành, căn cứ, "QUYẾT ĐỊNH:", bố cục Điều, nơi nhận "Như Điều ...".
---

# Quyết định

Áp dụng cùng skill `the-thuc-chung`.

## Phần mở đầu

- **Tên loại** `QUYẾT ĐỊNH`, dòng dưới là **trích yếu**: "Về việc ...", "Ban hành ...", "Phê duyệt ...".
- **Thẩm quyền ban hành**: dòng ghi chức vụ người đứng đầu hoặc tên cơ quan (ví dụ `GIÁM ĐỐC SỞ Y TẾ`, `ỦY BAN NHÂN DÂN TỈNH HÀ TĨNH`, `CHỦ TỊCH ỦY BAN NHÂN DÂN ...`). Thiếu → lỗi. Phải khớp người ký: thẩm quyền là "ỦY BAN NHÂN DÂN" thì ký `TM. ỦY BAN NHÂN DÂN`; thẩm quyền "CHỦ TỊCH ..." thì ký `CHỦ TỊCH` hoặc `KT. CHỦ TỊCH`.
- **Căn cứ ban hành**: có thẻ `[nghiêng]`; mỗi căn cứ một dòng, bắt đầu "Căn cứ ...", kết thúc bằng `;`, căn cứ cuối kết thúc bằng `.`; căn cứ pháp lý xếp trước, "Theo đề nghị của ..." (hoặc "Xét đề nghị") đứng cuối.
- Dòng `QUYẾT ĐỊNH:` trước Điều 1 (dòng mốc "dòng QUYẾT ĐỊNH:/QUYẾT NGHỊ:"). Không có dòng mốc này → lỗi.

## Nội dung (xét trên các dòng mốc "Điều")

- Bố cục theo **Điều**: `Điều 1.`, `Điều 2.` … đánh số liên tục (hồ sơ liệt kê các dòng Điều; khi quá nhiều chỉ giữ các Điều đầu và Điều cuối — số bị bỏ giữa chừng do lược không phải lỗi).
- Điều 1 nêu nội dung quyết định; nếu ban hành kèm theo văn bản khác (quy chế, quy định, kế hoạch) phải ghi "ban hành kèm theo Quyết định này".
- **Điều cuối** (hoặc các Điều cuối) giao trách nhiệm thi hành ("... chịu trách nhiệm thi hành Quyết định này.") và nêu hiệu lực ("Quyết định này có hiệu lực kể từ ngày ký" hoặc ngày cụ thể). Xét dòng Điều cuối và các dòng "đoạn cuối"; thiếu hẳn ý hiệu lực/thi hành → lỗi. Dòng Điều cuối bị lược ("…") trước phần đó → "không xác định được từ dữ liệu".

## Nơi nhận

- `- Như Điều <số điều thi hành>;` thay cho liệt kê lại các đối tượng thi hành; số Điều này phải có trong các dòng Điều. `- Lưu: VT, <đơn vị soạn thảo>.`
- Ký hiệu số phải có `QĐ-`.
