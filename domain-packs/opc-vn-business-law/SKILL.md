---
name: opc-vn-business-law
description: >-
  Cẩm nang cho chủ doanh nghiệp nhỏ: chọn hộ kinh doanh, doanh nghiệp tư nhân hay
  TNHH một thành viên; chuyển hộ kinh doanh lên công ty; Chủ tịch công ty, kế toán
  trưởng, điều lệ; thuế TNDN theo % doanh thu hay kê khai; kế toán siêu nhỏ; dự thảo
  Luật Phát triển doanh nghiệp nhỏ và vừa ("doanh nghiệp một người"). Không dùng cho
  tranh chấp, tố tụng, M&A, đầu tư nước ngoài, phá sản.
metadata:
  version: 1.0.0
  as_of: 2026-10-17
  review_by: 2027-04-17
  domain: doanh-nghiep-nho
  licence: CC-BY-4.0
  attribution: "Nội dung gốc: Diginno (opc-vn-business-law), CC BY 4.0, đã chỉnh sửa cho WeRAG"
  origin: https://github.com/lephuhung/opc-vn-business-law@1cb90548d6d8385cffdfd475798b0cfd1c01688f
  required_tools: [search_knowledge, compare_tax_methods]
  answer_footer: >-
    Thông tin tham khảo dựa trên cẩm nang của Diginno (CC BY 4.0), không phải tư vấn
    pháp lý, thuế hay kế toán; hãy đối chiếu văn bản gốc và hỏi luật sư hoặc kế toán
    hành nghề trước khi quyết định.
  draft_documents:
    - title: Dự thảo Luật Phát triển doanh nghiệp nhỏ và vừa
      version: Lần 2
      date: 2026-08-28
    - title: Dự án Luật sửa đổi, bổ sung Luật Kế toán và Luật Kiểm toán độc lập
      version: chưa rõ lần dự thảo
    - title: Dự án Luật sửa đổi, bổ sung Luật Thuế thu nhập doanh nghiệp, Luật Thuế thu nhập cá nhân và Luật Thuế sử dụng đất phi nông nghiệp
      version: chưa rõ lần dự thảo
---
# Cẩm nang doanh nghiệp một người

## Căn cứ và hiệu lực

- Căn cứ: `search_knowledge`; hiệu lực một số hiệu: `check_legal_status`; văn bản thay thế/hướng dẫn: `find_related_legal_documents`.
- `references/`, `assets/` (đọc bằng `read_file`): bình luận tham khảo, không phải căn cứ.
- Thuế suất, tỷ lệ % doanh thu, ngưỡng siêu nhỏ, lệ phí: tra trong kho tri thức, cẩm nang không giữ các số này.
- Điều 20 khoản 8 Luật Phát triển DNNVV: gọi là "dự thảo Lần 2 ngày 28/8/2026", không trình bày như quy định đang áp dụng.

## Hỏi dữ kiện (tối đa 5 câu, chỉ khi thiếu)

Ngành nghề (có điều kiện?); doanh thu, lao động đóng BHXH; loại hình hiện tại; mục tiêu 12 tháng; nợ, hợp đồng dài hạn, giấy phép con, tài sản đứng tên cá nhân. Thiếu thì trả lời theo kịch bản, không đoán.

## Quy trình A — Chọn mô hình

Đọc `references/01-entity-models.md`; so sánh pháp nhân, trách nhiệm tài sản, chi phí tuân thủ, thuế, vay/đấu thầu/gọi vốn. Đưa 2–3 phương án: tối giản, tăng trưởng, an toàn pháp lý.

## Quy trình B — "Doanh nghiệp một người" theo dự thảo

Đọc `references/02-draft-sme-law-2026.md`. Kiểm tra ba điều kiện đồng thời của Điều 20 khoản 8 dự thảo: chuyển đổi từ hộ kinh doanh; TNHH một thành viên do một cá nhân làm chủ; quy mô siêu nhỏ. Tách phần đã áp dụng (kế toán trưởng: Nghị định 174/2016/NĐ-CP, Thông tư 58/2026/TT-BTC) khỏi phần mới (bỏ Chủ tịch công ty, xung đột Điều 85 Luật Doanh nghiệp). Không khuyên chờ dự thảo.

## Quy trình C — Chuyển hộ kinh doanh lên công ty

Đọc `references/06-conversion-playbook.md` và `assets/checklist-chuyen-doi.md`. Chọn loại hình đích; rà soát nợ, hợp đồng, giấy phép, lao động, tài sản trước khi nộp hồ sơ; đánh dấu bước cần luật sư/kế toán. Chuyển đổi không xoá nghĩa vụ cũ.

## Quy trình D — Điều lệ, Chủ tịch công ty

Đọc `references/03-governance-tnhh-mtv.md`; tra Điều 85 Luật Doanh nghiệp 59/2020/QH14 trong kho tri thức. Tối giản hợp pháp: chủ sở hữu kiêm Chủ tịch và Giám đốc, điều lệ phân quyền rõ. Mẫu: `assets/dieu-le-tnhh-mtv-toi-gian.md`.

## Quy trình E — Kế toán và thuế

Đọc `references/04-accounting.md`, `references/05-tax.md`. Xác định siêu nhỏ theo ngưỡng tra được (Nghị định 80/2021/NĐ-CP). Không bắt buộc kế toán trưởng không có nghĩa là không cần kế toán. So sánh % doanh thu và kê khai bằng `compare_tax_methods` (doanh thu, lợi nhuận từ người dùng; tỷ lệ %, thuế suất tra trong kho tri thức); thiếu số thì hỏi hoặc tra trước. Đưa khối "Giả định" vào câu trả lời, nhắc GTGT đầu vào.

## Từ chối và chuyển chuyên gia

- Trốn thuế, hai sổ, chuyển tài sản né nợ, công ty rỗng đấu thầu: từ chối phần đó, nêu rủi ro, đưa phương án hợp pháp gần nhất.
- Cờ đỏ (tranh chấp, nợ quá hạn, yếu tố nước ngoài, M&A, đất đai, phá sản): xem `references/07-risk-and-red-flags.md`, khuyên gặp luật sư/kế toán hành nghề.

## Cấu trúc trả lời

Kết luận ngắn (luật hiện hành hay dự thảo) → Căn cứ (số hiệu, điều khoản, hiệu lực tra được) → 2–3 phương án → Rủi ro → Việc cần làm. Câu tra cứu ngắn thì rút gọn. Không hù doạ, không hứa "tiết kiệm X%" khi chưa có số liệu.
