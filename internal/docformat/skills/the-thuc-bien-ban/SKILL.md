---
name: the-thuc-bien-ban
description: Yêu cầu thể thức riêng của biên bản (NĐ30/2020/NĐ-CP) — tên loại BIÊN BẢN, thời gian địa điểm thành phần, chủ trì và thư ký, thời gian kết thúc, chữ ký hai bên.
---

# Biên bản

Áp dụng cùng skill `the-thuc-chung`.

## Thành phần

- Dòng tên loại `BIÊN BẢN`, dòng dưới là trích yếu ("Họp ...", "Bàn giao ...", "Kiểm tra ...").
- Ký hiệu số có `BB-`.
- Không có `Kính gửi`; nơi nhận không bắt buộc.

## Nội dung (xét trên các dòng mốc)

Hồ sơ đưa các dòng mốc riêng của biên bản: "thời gian, địa điểm", "thành phần, chủ trì, thư ký", "kết thúc biên bản". Kiểm tra:
1. Có dòng nêu **thời gian** bắt đầu hoặc **địa điểm** ("Hôm nay, …", "Vào hồi …", "Thời gian:", "Địa điểm:").
2. Có dòng nêu **thành phần** tham dự, **chủ trì** (chủ tọa) và **thư ký**.
3. Có dòng **kết thúc** ("Biên bản kết thúc vào hồi ... giờ ..." hoặc "Biên bản được lập thành ... bản").

Không có dòng mốc tương ứng thì **cảnh báo** "không thấy …" (dòng có thể được viết theo cách khác), không kết luận là lỗi.

## Ký

- Hai khối ký đặt ngang nhau: `THƯ KÝ` (một cột) và `CHỦ TỌA`/`CHỦ TRÌ` (cột kia), kèm họ tên; biên bản nhiều bên thì đại diện các bên cùng ký. Hai khối ký ở hai cột là **đúng**, không phải lỗi vị trí.
