---
name: sample-pack
description: >-
  Gói mẫu cho test: dùng khi câu hỏi về thủ tục đăng ký hộ kinh doanh; không dùng
  cho câu hỏi hình sự.
metadata:
  version: 1.0.0
  as_of: 2026-10-17
  review_by: 2027-04-17
  domain: doanh-nghiep-nho
  licence: CC-BY-4.0
  attribution: "Nội dung gốc: tác giả mẫu, CC BY 4.0, đã chỉnh sửa cho WeRAG"
  origin: https://example.invalid/sample-pack@0000000
  required_tools: [search_knowledge]
  answer_footer: >-
    Thông tin tham khảo, không phải tư vấn pháp lý.
  draft_documents:
    - title: Dự thảo Luật mẫu
      version: Lần 2
      date: 2026-08-28
---
# Gói mẫu

1. Hỏi người dùng loại hình kinh doanh.
2. Tra căn cứ bằng search_knowledge; hiệu lực bằng check_legal_status.
3. Đọc bình luận tại references/a.md khi cần.
