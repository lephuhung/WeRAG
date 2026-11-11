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
| T3 | Bộ đếm sử dụng: lỗi ghi `RecordQuestion` / OCR chỉ ghi log Warn, lượt đó mất khỏi hạn mức. Thử lại 2 lần (200 ms, 1 s; câu lệnh upsert là nguyên tử nên không đếm trùng), hết lượt thử thì log Error liệt kê từng bộ đếm để cộng bù tay (`usage_meter.go`) | Xong |
| T4 | Gỡ 8 adapter IM không còn chào bán (dingtalk, feishu, mattermost, qqbot, slack, wechat, wecom, yunzhijia) và đăng nhập QR WeChat (chỉ dùng cho kênh IM). Chỉ giữ Telegram. DB dev không có kênh IM nào | Xong |
| T5 | Sửa chú thích `rbac.go` `LegalUploader` và `RegisterLegalPushRoutes`: đường push dùng API key và kiểm tra trong handler | Xong |
| T6 | Xoá tài liệu gốc WeKnora đã lỗi thời: `docs/zh/`, `docs/wiki/`, `docs/ROADMAP.md`, `docs/LITE.md` (Lite đã gỡ), `IM集成开发文档.md` (chỉ còn Telegram), `使用其他向量数据库.md` (chỉ Postgres) | Xong |
| T7 | Dịch sang tiếng Việt các tài liệu tiếng Trung còn mô tả tính năng đang chạy: `docs/api/*` (phần lớn), `QA.md`, `RBAC说明.md`, `Langfuse集成.md`, `OIDC认证调用流程.md`, `MCP功能使用说明.md`, `日志配置.md`, `开发指南.md`, `快速开发模式说明.md`, `共享空间说明.md`, `开启知识图谱功能.md`, `添加新的网络搜索引擎.md`, `agent-skills.md`, `sandbox-*.md`. Việc lớn, làm sau giai đoạn 1 | Chưa làm |

Cần người dùng quyết, không tự làm: đẩy 23 commit của dev lên origin; gộp `feat/federation` (đánh số lại migration trước).

## Giai đoạn 1: ba tính năng ưu tiên — XONG, đã gộp vào dev 2026-11-11

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
- Migration 000195: bảng người đề nghị (request_id, user_id, tenant_id, session_id, message_id, created_at). Ghi tại chỗ tạo hoặc tăng đề nghị. Migration 000196: bảng thông báo trong ứng dụng `user_notifications`.
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

- 2026-11-11: lập kế hoạch; T1–T6 xong trên `chore/tech-debt-2026-11`, hai vòng Fable review (GO-with-changes, đã sửa hết MED và LOW), gộp vào dev. Còn nợ: sinh lại swagger toàn bộ (file sinh sẵn đã cũ), README còn liệt kê vector DB/storage đã gỡ.
- 2026-11-11: giai đoạn 1 xong, mỗi tính năng ba vòng Fable review, đã gộp vào dev và thử API thật trên dev:
  - F2 (`648e62dd`, migration 000194 `legal_lookup_misses` + index `number_key text_pattern_ops`): `GET /legal-lookup`, trang `/platform/legal-lookup`, tab "Tra cứu không thấy"; tải tệp kèm `external_id`/`source` mà không phải SuperAdmin/khoá nền tảng → 403.
  - F1 (`2cabc1a6`): `POST …/documents/:doc_id/legal-basis-check`, nút trong trợ lý văn bản; trạng thái mới `needs_review`; số hiệu không năm chỉ chốt khi ngày ban hành khớp.
  - F3 (`0e3b40a1`, migration 000195 `legal_request_requesters`, 000196 `user_notifications`): thông báo trong ứng dụng (web + add-in `809e8ed` trên main add-in), không email; giữ 7 ngày; thông báo đã đọc xoá sau 90 ngày.
  - Còn lại: M6 — F1 dùng chung bộ phân loại `types.ClassifyLegalStatus` của F2; công cụ chat "rà soát căn cứ" cho trợ lý; xem UI trên trình duyệt/ONLYOFFICE/Word; nhánh federation phải đổi migration lên ≥ 000197.
