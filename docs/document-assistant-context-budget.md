# Trợ lý soạn thảo: nạp văn bản vào LLM theo nhu cầu

Thiết kế đã chốt ngày 2026-10-08. Phạm vi: chỉ agent trợ lý soạn thảo (`document_assistant`), nhánh `feat/document-assistant`.

## 1. Vấn đề (đã xác minh trong code)

| # | Chỗ | Hiện trạng |
|---|---|---|
| 1 | `internal/agent/tools/open_document_prompt.go` `BuildOpenDocumentPrompt` | Mỗi lượt tải lại từng .docx từ storage, parse lại (`InspectDocx`), chèn mọi tab khi không @ (commit 50d7291f). 4 tab ≈ 40k rune ≈ 11–12k token mỗi lượt. Không cache theo revision. |
| 2 | `internal/application/service/temporary_document.go` `ResolveForPrompt`; `internal/handler/session/qa.go` `persistResolvedAttachmentContent`; `internal/application/service/agent_history.go` `buildUserHistoryMessage` | File upload tại chat được nạp tới 12k token (chia đều theo số file trong tin nhắn; file nhỏ nạp nguyên văn). Nội dung đó ghi vào message và phát lại ở **mọi** lượt sau qua `BuildPrompt()`. |
| 3 | `frontend-next/components/chat-client.tsx` (`attachmentIds`), `doc-workspace.tsx` (`openAttachment`) | .docx upload tại chat vừa là attachment (mục 2) vừa thành tab (mục 1): chèn hai lần. |
| 4 | `internal/application/service/document_format_precheck.go` `Start`; `check_document_format.go` `Prewarm` | Một goroutine mỗi tài liệu, không giới hạn song song; mỗi tài liệu 2 cuộc gọi LLM (gán nhãn thinking tắt, thẩm định thinking bật tới 3,5 phút). |
| 5 | `internal/agent/tools/persist.go` `CompactToolOutputForHistory` | Output tool phát lại nguyên văn trong lịch sử (báo cáo thể thức, outline 80 đoạn, chính tả). |

File upload tại chat: tối đa 5 file một tin nhắn, parse khi upload (docparser, OCR, VLM), chunk 1600/160, TTL 24 giờ (`WEKNORA_CHAT_ATTACHMENT_TTL_HOURS`).

## 2. Vai trò tài liệu

- **target** (văn bản làm việc): file mở trong tab ONLYOFFICE. Thường là file có sẵn dữ liệu cần đối chiếu, cập nhật từ văn bản khác; không nhất thiết là bản nháp. Được kiểm tra thể thức; là đích duy nhất của tool sửa và đánh dấu.
- **source** (tài liệu nguồn): file upload tại chat (docx, pdf, scan, xlsx, ảnh). Không tab, không kiểm tra thể thức, không bao giờ nạp nguyên văn; chỉ có thẻ hồ sơ và tra cứu.
- Vai trò lấy theo **cách mở**: tab = target, upload tại chat = source. Không theo thứ tự upload. Có nút chuyển hai chiều (Word mới nâng lên target được).
- Source phải sống lâu bằng phiên: lưu như hàng workspace với cột `role`, không dùng TTL 24 giờ của `temporary_documents`. Cùng một dãy handle vb1…vbN, cùng cơ chế @ và `find_in_documents` cho cả hai vai trò. Giới hạn đề xuất: 10 source một phiên; vượt thì báo, không nạp lén.
- Cùng file vừa upload tại chat vừa mở tab: một danh tính (workspace đã có `attachment_id`); khi đã là tab thì bỏ nội dung attachment của nó.

## 3. Quy tắc nạp văn bản vào prompt

| Tình huống | Nạp |
|---|---|
| Target được @, có đoạn bôi đen, hoặc là tab duy nhất | Nạp đầy đủ tới 16k rune như hiện nay; với bôi đen: cửa sổ quanh đoạn bôi đen, không từ đầu file |
| Nhiều tài liệu, không gọi tên | Thẻ hồ sơ mọi tài liệu + đoạn khớp từ khóa (ngân sách chung ≈ 6k rune) + ~10 đoạn đầu mỗi tài liệu khi chưa có hồ sơ |
| Source | Chỉ thẻ + đoạn khớp; phần còn lại qua `find_in_documents` |
| Kiểm tra thể thức | Không nạp văn bản; tool chạy trên file |
| Chính tả | Cửa sổ 60 đoạn như hiện nay |
| Tóm tắt | Outline + từng mục theo lượt |
| Sửa, viết lại | Chỉ đoạn bôi đen hoặc khoảng đoạn người dùng chỉ |

Layout đã parse (`InspectDocx`) cache theo (document id, revision); chỉ tải và parse lại khi revision đổi.

## 4. Hồ sơ tài liệu (`DocumentProfile`)

Job nền chạy cho mọi tài liệu của phiên ngay sau parse, chung hàng đợi với kiểm tra thể thức nhưng độc lập, xếp **trước** thẩm định thể thức. Mở rộng `types.KnowledgeProfile` (`internal/types/knowledge_profile.go`; prompt sinh ở `knowledge_process.go` và `knowledge_legal_identity.go`):

- Không cần LLM: số ký hiệu, cơ quan ban hành, ngày, loại văn bản, trích yếu từ thành phần đầu (phân đoạn heuristic `docformat`, không cần model); danh sách mục (Căn cứ, Điều, Chương, Phụ lục) kèm khoảng đoạn.
- Một cuộc gọi LLM thinking tắt (như `docformat/chat_completer.go`): đầu vào tiêu đề các mục + vài đoạn đầu mỗi mục, trần ≈ 8k token; văn bản rất dài tóm tắt theo cụm mục rồi gộp. Lưu ý bước gán nhãn thể thức lược phần giữa thân văn bản (`labeling.go` `elide`) nên không tóm tắt từ nó được.
- Đầu ra JSON: `document_number`, `issuer`, `date`, `doc_type`, `subject`, `gist`, `key_points[]`, `sections[]{title, from, to, summary}`, `topics[]`, `entities{units[], cited_numbers[], dates[], figures[]}`, `typical_questions[]`. Với source, ghi rõ các bảng số liệu và mục chứa chúng.
- Cache Redis theo hash **văn bản thuần** (không phải hash file), tiền tố `werag:docprofile:`, TTL như kết quả thể thức. Vòng tiến độ header thêm trạng thái "đang đọc nội dung".
- Cập nhật cho target: lịch cố định **5 phút sau lần lưu đầu chưa xử lý**; lưu thêm trong 5 phút không dời mốc. Tới mốc so hash văn bản: không đổi thì giữ; đổi trong một mục thì làm mới mục đó; mục thêm bớt thì làm mới toàn bộ. Lượt chat gặp hồ sơ cũ thì dùng kèm cờ "đã sửa sau lần đọc" và đẩy job lên đầu hàng đợi. Đóng tab hoặc kết thúc phiên: hủy lịch, giữ hồ sơ cuối. Không dùng cơ chế yên lặng 30 giây của `Recheck`.
- Ngay sau upload, chat hiện thẻ tóm tắt kèm 2–3 chip `typical_questions`.

## 5. Đoán tài liệu khi câu hỏi mơ hồ

1. **Khớp xác định, không LLM**: từ khóa, mã, số trong câu hỏi đối chiếu `document_number`, `subject`, `topics`, `entities`, tiêu đề mục. Một tài liệu trội rõ thì chọn, kèm mục trội.
2. **Bộ định tuyến nhỏ**: khi hòa hoặc trống, một cuộc gọi thinking tắt nhận câu hỏi, tóm tắt 3 lượt gần nhất và thẻ các tài liệu (≈1k token); trả về tài liệu, mục, loại việc, độ tin cậy.
3. **Hỏi người dùng** chỉ khi tin cậy thấp, thẻ làm rõ điền sẵn dự đoán tốt nhất.

Kết quả ở mọi tầng ghi thành **phạm vi của phiên** (tài liệu, khoảng đoạn, loại việc), hiện như chip "Phạm vi: vb1, Điều 3–5 · Đối chiếu" phía trên ô soạn (giống chip đoạn bôi đen hiện có). Lượt sau nạp theo phạm vi, không đoán lại. Bỏ bằng X, hoặc tự đổi khi bôi đen hay @ tài liệu khác.

## 6. Cổng làm rõ cho văn bản dài

Chạy trước khi dựng prompt, không cần LLM. **Hỏi** khi đủ ba điều kiện: tài liệu phải nạp vượt ngưỡng (một tài liệu ≥ 16k rune hoặc ≥ 150 đoạn, hoặc tổng vượt ngưỡng); câu hỏi không có @, bôi đen, mã, số, tên mục; câu hỏi chung chung ("xem giúp", "góp ý", "kiểm tra", "tóm tắt"). **Không hỏi** khi có mốc thu hẹp, việc đã rõ (thể thức, chính tả), đã trả lời cho tài liệu và việc đó trong phiên, người dùng nói "cả văn bản" (đọc theo mục, báo rõ), hoặc tài liệu ngắn.

Thẻ làm rõ: một dòng tình trạng (số đoạn, ước lượng trang); chip việc: thể thức, chính tả, tóm tắt, một phần, đối chiếu với nguồn, việc khác; chọn "một phần" hoặc "tóm tắt" thì hiện danh sách mục để tích; nhiều tài liệu thì hàng chọn tài liệu, mặc định tab đang xem. Hiện thực nhẹ: trả về tin nhắn trợ lý có chip (loại gợi ý `clarify` trong `frontend-next/lib/api/chat.ts`) và kết thúc lượt, không chạy agent; lượt sau gửi thêm trường `document_scope`. Không dùng máy trạng thái của cổng viết tắt. Prompt hệ thống thêm quy tắc: việc đòi đọc cả văn bản dài mà chưa có phạm vi thì đề xuất mục và hỏi, không lặp `read_document_outline` qua hàng trăm đoạn.

## 7. Tool `find_in_documents`

Tên ban đầu là `search_document`; đổi thành `find_in_documents` vì tool tra cứu của kho tri thức đã có tên gần như vậy (`search_document_section`, tài liệu kho tri thức với handle dN, điều/khoản/điểm), còn tool này chỉ tìm trong tài liệu của phiên (handle vb1…vbN). Hai tên khác hẳn để model không nhầm phạm vi.

Tham số `query`, `document` (handle; trống = tất cả), `limit`. Nguồn: target tìm theo đoạn `[i]` từ layout cache; source tìm theo chunk đã lưu (`ContextHeader`, `Seq`). Chấm điểm từ vựng: chữ thường, khớp có và không dấu, khớp chính xác số ký hiệu, Điều/khoản, mã đơn vị, con số (ưu tiên). Cùng một hàm chấm điểm dùng cho chèn "đoạn khớp" tự động và cho tool. Kết quả theo từng tài liệu: chỉ số đoạn hoặc chunk, văn bản kèm đoạn trước và sau, nhãn thành phần NĐ30 nếu có, tổng số khớp; trần ≈ 4k rune mỗi lần gọi; rút gọn trong lịch sử thành "đã tìm X trong vb2: n kết quả". Chưa dùng embedding (giai đoạn 2).

## 8. Kịch bản đối chiếu và cập nhật (chính)

| # | Tình huống | Xử lý |
|---|---|---|
| Đ1 | "Đối chiếu số liệu của vb1 với vb2, vb3" | Đọc target theo mục; mỗi đoạn có số liệu, tên, ngày: rút từ khóa **từ chính đoạn đó** để gọi `find_in_documents` trên source; báo khớp hoặc lệch, đánh dấu chỗ lệch bằng `mark_passages`, không sửa |
| Đ2 | "Cập nhật số liệu của vb1 theo vb2" | Như Đ1 rồi `rewrite_paragraphs` kiểu old/new trên target; mỗi thay đổi dẫn nguồn (số ký hiệu, Điều hoặc chunk) |
| Đ3 | Bôi đen bảng hoặc đoạn, "kiểm tra với các văn bản đã gửi" | Chỉ nạp đoạn bôi đen; truy vấn source bằng thực thể trong đoạn; kết quả theo từng source |
| Đ4 | Source mâu thuẫn nhau | Nêu cả hai kèm nguồn, không tự chọn; ngày ban hành trong hồ sơ gợi ý bản mới hơn |
| Đ5 | Target dài, "đối chiếu toàn bộ" | Theo mục, mỗi lượt báo mục đã xong, chip "tiếp tục mục sau" |
| Đ6 | Hỏi chỉ về source | Không đụng target; thẻ + `find_in_documents` |

Các case upload tại chat: kèm câu hỏi cụ thể và file ngắn thì nạp nguyên văn **chỉ lượt đó**; file dài hoặc nhiều file thì thẻ + đoạn khớp, không chia đều 12k/N; kèm câu chung chung thì cổng làm rõ; không kèm câu hỏi thì chỉ index và trả lời bằng thẻ; lượt sau hỏi về file cũ thì tìm trên chunk đã lưu; lượt không liên quan thì không nạp gì từ file.

## 9. Lịch sử và hàng đợi

- `buildUserHistoryMessage`: giữ nội dung attachment chỉ ở lượt mới nhất có attachment; lượt cũ giữ tên, loại và ghi chú "tra cứu bằng find_in_documents".
- Rút gọn output trong lịch sử cho `check_document_format` (tóm tắt), `read_document_outline` (khoảng đoạn đã đọc), `check_spelling` (số lỗi), `find_in_documents` (số kết quả).
- Kiểm tra thể thức: semaphore toàn cục 1–2 luồng, ưu tiên tab đang xem; gán nhãn chạy ngay (rẻ, cần cho outline), thẩm định thinking chỉ cho tab đang xem hoặc khi được hỏi.
- Prompt hệ thống (`config/prompt_templates/agent_system_prompt.yaml`, id `document_assistant`): mô tả target là văn bản cần kiểm tra, đối chiếu, cập nhật; sửa chỉ khi được yêu cầu; cập nhật số liệu phải dẫn nguồn.

## 10. Thứ tự thực hiện

1. Việc nhanh: cache layout theo revision; bỏ chèn hai lần cho file đã là tab; semaphore cho kiểm tra thể thức.
2. Cốt lõi: vai trò target/source, thẻ, hồ sơ tài liệu, `find_in_documents`, định tuyến ba tầng.
3. Cổng làm rõ và chip phạm vi ở `frontend-next`.
4. Cắt lịch sử attachment và rút gọn output tool.

Ước lượng 4 tài liệu, không gọi tên: khối văn bản mỗi lượt từ ≈40k rune xuống ≈11k rune; attachment lặp trong lịch sử từ tới 12k token mỗi lượt xuống chỉ lượt mới nhất; upload 4 file từ 8 cuộc gọi LLM cùng lúc xuống 4 hồ sơ xếp hàng và 1 thẩm định.

Ràng buộc chung: UI chỉ sửa ở `frontend-next` (en, vi); backend dev chạy bằng air, không reload khi sửa `.md`/`.json`; khóa Redis tiền tố `werag:`; mỗi bước có test Go và test frontend đi kèm.
