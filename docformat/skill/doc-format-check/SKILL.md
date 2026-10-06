---
name: doc-format-check
description: Use when a user uploads a .docx văn bản hành chính (công văn, quyết định, tờ trình, báo cáo, nghị quyết, biên bản, thông báo, chỉ thị, nghị định, thông tư) and asks to check, review or evaluate its thể thức / format / bố cục / căn lề against Nghị định 30/2020/NĐ-CP.
---

# Kiểm tra thể thức văn bản (NĐ30/2020/NĐ-CP)

Skill này đi kèm package Python `docformat` (stdlib thuần, không cần cài thêm
gói). Pipeline: **inspect** bóc tách layout + thành phần → **check** chấm theo
bộ luật của loại văn bản.

## Cách dùng

Cỡ chữ, font, căn lề được chấm **theo từng thành phần**; gán sai thành phần
(nhầm người ký với nội dung, nhầm cơ quan với số ký hiệu...) thì chấm sai.
Vì vậy **bạn tự gán nhãn thành phần** trước khi chấm — heuristic của
script chỉ là gợi ý (`hint`).

1. Xác định file `.docx` cần kiểm tra — thường là attachment đã stage dưới
   `/workspace/input/...`. Chỉ hỗ trợ `.docx` (file `.doc`/PDF thì báo rõ
   không hỗ trợ, đừng đoán nội dung).
2. Lấy đề bài gán nhãn:

   ```
   PYTHONPATH="$WEKNORA_SKILL_DIR" python3 -m docformat label-task "<đường dẫn .docx>"
   ```

   JSON trả về gồm `instructions`, `components` (định nghĩa từng thành phần
   theo NĐ30), `document_types` và `units` — mỗi unit có `id`, `text`,
   `zone` (left/right/full), `align`, `size`, `b`/`i` (đậm/nghiêng),
   `upper`, `tbl`, `hint`.
3. Gán **mọi** unit `id` vào đúng một thành phần theo **vai trò** của dòng
   (không theo định dạng — văn bản đang kiểm tra có thể định dạng sai), sửa
   những `hint` sai. Ghi kết quả ra file:

   ```
   cat > /tmp/labels.json <<'JSON'
   {"document_type": "quyet_dinh",
    "labels": {"0": "co_quan_chu_quan", "1": "co_quan_ban_hanh", "...": "..."}}
   JSON
   ```

4. Chấm với nhãn vừa gán:

   ```
   PYTHONPATH="$WEKNORA_SKILL_DIR" python3 -m docformat check "<đường dẫn .docx>" --labels /tmp/labels.json --format json
   ```

   - `--type` ép bộ luật nếu cần: một trong 29 loại văn bản hành chính của
     Điều 7 NĐ30 (`cong_van`, `quyet_dinh`, `ke_hoach`, `giay_moi`, … — liệt kê
     bằng `python3 -m docformat types`), hoặc `nghi_dinh`, `thong_tu`
     (mặc định `auto` = theo `document_type`).
   - Kiểm tra `segmentation` trong báo cáo: `missing`/`invalid` khác rỗng
     nghĩa là nhãn thiếu/sai tên → sửa labels.json và chấm lại.
   - Không gán nhãn được (văn bản quá dài, lỗi...) thì chạy `check` không
     có `--labels` (heuristic) và nói rõ kết quả có thể lệch thành phần.

5. Đọc JSON báo cáo:
   - `summary` — số check pass/fail/warn/skip.
   - `components` — các thành phần tìm được (quoc_hieu, tieu_ngu,
     co_quan_ban_hanh, so_ky_hieu, dia_danh_ngay_thang, trich_yeu, noi_dung,
     signature, noi_nhan...) kèm đoạn text làm bằng chứng.
   - `checks[]` — từng quy tắc: `status` pass/fail/warn, `desc`, `expected`,
     `actual` và `evidence` (đoạn văn bản vi phạm cụ thể).

## Trả lời người dùng

- Trình bày kết quả theo nhóm: (1) thành phần thiếu/sai vị trí, (2) font &
  cỡ chữ, (3) căn lề & vùng (cột trái/phải), (4) khổ giấy & lề trang.
- Với mỗi lỗi `fail`: nêu quy định (desc), giá trị thực tế và trích đoạn
  vi phạm từ `evidence` — không liệt kê máy móc toàn bộ 40+ check, tập trung
  vào fail trước rồi mới đến warn.
- `skip` nghĩa là không đo được (ví dụ thành phần không tìm thấy) — nêu rõ
  thay vì coi như đạt.
- Nhắc người dùng đây là kiểm tra tự động theo NĐ30/2020 Phụ lục I; một số
  chi tiết (con dấu, độ mật ghi ở góc phải, chữ ký số) cần rà soát thủ công.
