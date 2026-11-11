# Kế hoạch 2026-11-11: dọn nợ kỹ thuật, rồi làm tính năng mới

Nguồn: rà soát toàn repo của Fable ngày 11/11/2026 (18 đề xuất và danh sách nợ kỹ thuật).
Cách làm: Opus viết code, Fable review từng phần. Sửa xong các lỗi review mới sang phần tiếp theo.

## Quy tắc chung

- Mỗi phần làm trên worktree riêng `/home/WeRAG-wt/<tên>`, tách từ HEAD của dev (`feat/legal-upload-permission`). Không sửa file Go trong `/home/WeRAG` khi người dùng đang thử (air sẽ khởi động lại backend).
- Gộp vào dev sau khi Fable duyệt, chạy đủ test Go của gói liên quan và `tsc` của frontend-next. Chưa đẩy lên origin khi người dùng chưa cho phép.
- Migration mới bắt đầu từ **000194** (bản cao nhất đã commit trên dev là 000193). Lưu ý: `feat/federation` đang giữ 000193–000195 và phải đánh số lại trước khi gộp.
- Chuỗi giao diện: thêm vào cả `en` và `vi`. Tenant gọi là "Tổ chức". Giờ GMT+7.
- Hành vi người dùng phải thấy thì ép bằng code, không dựa vào prompt (Qwen bỏ qua quy tắc trong prompt).

## Giai đoạn 0: nợ kỹ thuật (nhánh `chore/tech-debt-2026-11`)

| # | Việc | Trạng thái |
|---|------|-----------|
| T1 | Lấy bản sửa "LLM lỗi thành câu trả lời rỗng" (b3190204) từ `feat/federation` sang dev | Cherry-pick sạch |
| T2 | Gộp `fix/degenerate-final-answer` (5fadb335). Xung đột ở `engine.go`: giữ cả hai nhóm trường; khi hết lượt thử vì rỗng thì kết thúc bằng thông báo lỗi mô hình, không dùng câu dự phòng. Stream đứt khi bộ kiểm tra đầu câu còn giữ chữ thì chưa có gì hiện ra, nên thử lại lượt (test mới `TestModelFailure_BreakWhileLeadHeldRetries`) | Xong, test xanh |
| T3 | Bộ đếm sử dụng: lỗi ghi `RecordQuestion` / OCR chỉ ghi log Warn, lượt đó mất khỏi hạn mức. Thử lại có giãn cách, hết lượt thử thì log Error có đủ scope để đối soát (`usage_meter.go`) | Chưa làm |
| T4 | Gỡ 8 adapter IM không còn chào bán (dingtalk, feishu, mattermost, qqbot, slack, wechat, wecom, yunzhijia). Chỉ giữ Telegram. DB dev không có kênh IM nào | Chưa làm |
| T5 | Sửa chú thích `rbac.go` `LegalUploader`: đường push dùng API key và kiểm tra trong handler | Chưa làm |
| T6 | Xoá tài liệu tiếng Trung của WeKnora gốc: `docs/zh/`, `docs/wiki/` (thư mục tên Trung), các tệp tên Trung trong `docs/`, `docs/ROADMAP.md`. Tài liệu tiếng Anh chỉ có ví dụ chữ Hán thì giữ | Chưa làm |

Cần người dùng quyết, không tự làm: đẩy 23 commit của dev lên origin; gộp `feat/federation` (đánh số lại migration trước).

## Giai đoạn 1: ba tính năng ưu tiên

### F1. Rà soát căn cứ pháp lý trong văn bản đang soạn (S–M)
- **Bước 0**: gộp các regex số hiệu thành một bộ nhận diện dùng chung. Hiện có ít nhất 5 bản: `vietnamese_legal/entity.go`, `legal_amendment_history.go`, `document_search.go`, `document_template_facts.go`, `domainpacks/formulas.go`. Có test so sánh để không đổi hành vi.
- **Backend**: route `POST …/documents/:id/legal-basis-check` cạnh `format-check` và `spelling` (`routes_chat.go`). Quét toàn văn, tra danh mục, dùng lại `LegalValidityNote` / `fullyExpiredDocs`. Không gọi LLM.
- **Kết quả**: mỗi viện dẫn có trạng thái còn hiệu lực / hết hiệu lực (kèm văn bản thay thế) / bị sửa đổi / trùng số hiệu (danh sách chọn) / chưa có trong danh mục ("chưa rõ", không bao giờ ghi "hết hiệu lực").
- **Giao diện**: nút trên thanh trợ lý văn bản (web ONLYOFFICE), thẻ kết quả, gạch chân bằng hợp đồng findings sẵn có. Chỉ đánh dấu, không tự sửa.
- **Add-in**: dùng cùng route, làm sau khi web xong (repo `/home/werag-word-addin`).

### F2. Tra cứu hiệu lực không qua chat (S)
- Endpoint đọc cho member và guest trên các hàm repository sẵn có (`FindByNumberKey`, `FindByNumberPrefix`, `SearchByTitle`, `ListLinks`).
- Guest: rate limit theo IP. Ghi rõ ngày dữ liệu đồng bộ từ vbpl.
- Giao diện: ô tra cứu (command palette, trang cổng guest).

### F3. Báo cho người hỏi khi văn bản đề nghị đã được bổ sung (S–M)
- Migration 000194: bảng người đề nghị (request_id, user_id, tenant_id, session_id, message_id, created_at). Ghi tại chỗ tạo hoặc tăng đề nghị.
- Hook khi đóng đề nghị là "added": gửi thư (loại mới trong `EmailNotification`) kèm đường dẫn về hội thoại. Guest không có email thì bỏ qua cho đến khi có thông báo trong ứng dụng (F12).

## Giai đoạn 2: dữ liệu đang chỉ ghi vào, công sức S

- **F4** Hộp phản hồi "không thích" kèm nhận xét (Tenant Admin xem trong Tổ chức mình; SuperAdmin xem chéo Tổ chức thì cần người dùng quyết).
- **F5** Xuất nhãn chính tả và thể thức (JSONL) cho SuperAdmin.
- **F7** Lưu cờ "không có căn cứ" trên message, trang danh sách câu hỏi kho đang thiếu.
- **F6** Tìm theo nội dung hội thoại cũ.
- **F8** Tải câu trả lời ra .docx kèm trích dẫn (code dựng, `docxwrite`).
- **F9** Báo cáo sử dụng theo thành viên cho Tenant Admin.

## Giai đoạn 3: công sức M

- **F12** Trung tâm thông báo trong ứng dụng (làm trước F10).
- **F10** Theo dõi văn bản hoặc lĩnh vực, bản tin hằng ngày.
- **F11** Hỏi theo thời điểm (TODO `legal_amend_wiring.go:132`; trích ngày bằng code).
- **F13** Quét kho nội bộ tìm viện dẫn văn bản hết hiệu lực (dùng bộ quét của F1).
- **F15** Chia sẻ hội thoại chỉ đọc trong Tổ chức.
- **F16** Micro đọc chính tả trong add-in.
- **F17** Chạy bộ đánh giá hằng đêm và so baseline.

Chờ người dùng quyết: **F14** nhà cung cấp đối soát chuyển khoản; **F18** mẫu .docx riêng của Tổ chức (L).

## Nhật ký

- 2026-11-11: lập kế hoạch; T1, T2 xong trên `chore/tech-debt-2026-11`.
