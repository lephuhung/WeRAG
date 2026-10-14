---
name: the-thuc-cong-van
description: Yêu cầu thể thức riêng của công văn (NĐ30/2020/NĐ-CP) — không có tên loại, trích yếu "V/v" dưới số ký hiệu, "Kính gửi", ký hiệu cơ quan-đơn vị soạn thảo, "Như trên" ở nơi nhận.
---

# Công văn

Áp dụng cùng skill `the-thuc-chung`.

## Thành phần bắt buộc

- **Không có dòng tên loại** ("CÔNG VĂN" không được in như một tiêu đề). Ô [5b] có dòng `CÔNG VĂN` → lỗi.
- **Trích yếu**: dòng bắt đầu bằng `V/v` + nội dung tóm tắt, đặt dưới số ký hiệu ở cột trái (vị trí, cỡ chữ đã được đo). Trích yếu viết "Về việc ..." thay cho `V/v` → cảnh báo. **Thiếu trích yếu là lỗi** — đề xuất câu trích yếu ngắn từ dòng mốc mở đầu (ví dụ "V/v cung cấp số liệu phục vụ lập đề án ...").
- **Số ký hiệu**: `Số: .../<viết tắt cơ quan>-<viết tắt đơn vị soạn thảo>` — không có mã loại văn bản (ví dụ `45/UBND-VP`, `/PA05-Đ4`); ghi `CV-` là lỗi.
- **Kính gửi**: `Kính gửi:` + tên cơ quan, đơn vị, cá nhân nhận; gửi một nơi thì ghi cùng dòng, nhiều nơi thì mỗi nơi một dòng có gạch đầu dòng, kết thúc bằng `;` và dòng cuối `.`.

## Nội dung (xét trên các dòng mốc)

- Mở đầu thường nêu căn cứ/lý do ("Thực hiện ...", "Theo đề nghị của ...").
- Kết thúc bằng câu đề nghị hoặc "./." ở cuối nội dung (dòng mốc "câu kết"/"đoạn cuối"); không thấy thì **cảnh báo**.

## Nơi nhận

- Có `Kính gửi` thì dòng đầu nơi nhận là `- Như trên;`.
- Cuối cùng `- Lưu: VT, <đơn vị soạn thảo>.`
