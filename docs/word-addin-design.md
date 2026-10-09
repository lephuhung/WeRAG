# WeRAG cho Word — thiết kế add-in

Ngày 2026-10-09. Bản nháp để duyệt, chưa có code.

> **Cập nhật 2026-10-09:** taskpane đã tách khỏi `frontend-next` thành repo riêng **werag-word-addin** (Vite + React). Bản build phục vụ ở `/word/` trên cùng tên miền với WeRAG. Backend (workspace `word_addin`, route upload nội dung) vẫn nằm trong WeRAG. Những chỗ dưới đây ghi `frontend-next`/`app/word`/`deploy/word-addin` nay tương ứng với repo add-in (`src/`, `manifest/`).

## 1. Mục tiêu và phạm vi

Đưa trợ lý "Soạn thảo văn bản" của WeRAG vào Word 2021 dưới dạng một taskpane, giữ nguyên agent, tool và kiểm tra thể thức NĐ30 ở backend. Add-in chỉ làm ba việc: gửi tài liệu lên, hiển thị chat, và áp dụng `document_ops` vào Word.

**Làm:**

- Đăng nhập bằng tài khoản WeRAG (gồm 2FA), chọn tenant như web.
- Chat với agent `builtin-document-assistant`: hỏi về nội dung, viết lại đoạn chọn, chèn đoạn, đánh dấu lỗi chính tả, sửa thể thức.
- Áp dụng các op `replaceText`, `replaceParagraph`, `insertAfter`, `mark`, `formatParagraph` trực tiếp trong Word; người dùng hoàn tác bằng Ctrl+Z.
- Kiểm tra thể thức NĐ30 chạy nền, vòng tiến độ, thẻ "Xem đánh giá / Sửa lỗi thể thức".
- Phần Word không cho add-in ghi (lề trang, khổ giấy): theo dõi và hướng dẫn thao tác; tùy chọn mở bản đã sửa bằng `Word.createDocument`.

**Chưa làm ở giai đoạn này:**

- Track changes và comment (cần WordApi 1.4–1.6, Office 2021 không có).
- Nhiều tab tài liệu như web: mỗi cửa sổ Word là một tài liệu đích; tài liệu nguồn tải lên qua nút đính kèm như chat thường.
- Chạy LLM trong taskpane như Word GPT Plus: mọi lời gọi model đi qua backend WeRAG.
- Word trên Mac, Word Web, Office 2016/2019.

Nền tảng mục tiêu: **Word 2021 trên Windows 10/11, WebView2, baseline WordApi 1.3**. Tính năng cao hơn chỉ bật khi `Office.context.requirements.isSetSupported(...)` trả về true.

## 2. Tham khảo Word GPT Plus

Lấy từ Word GPT Plus khung add-in (manifest, khởi động Office.js, các hàm `Word.run`); agent, đăng nhập và giao diện dùng của WeRAG. Bản tham khảo: [Kuingsmile/word-GPT-Plus](https://github.com/Kuingsmile/word-GPT-Plus) v2.0.2, commit `8b0dcf5` (29/01/2026), giấy phép MIT, khoảng 5.300 dòng (Vue 3 + Vite + Tailwind + LangChain).

| Thành phần Word GPT Plus | Nội dung | Quyết định cho WeRAG |
| --- | --- | --- |
| `release/self-hosted/manifest.xml` | Manifest XML: taskpane, nút ribbon, quyền `ReadWriteDocument` | **Giữ**, đổi ID, tên, icon, URL sang máy chủ WeRAG |
| `index.html` + `main.ts` | Tải `office.js` từ CDN, khởi động app sau `Office.onReady` | **Giữ cách làm**, viết lại thành layout React cho route Next.js |
| `utils/wordTools.ts` (900 dòng) | 25+ hàm `Word.run`: đọc vùng chọn, chèn đoạn, tìm-thay, font, bảng, danh sách | **Giữ phần thân hàm** làm tham khảo khi viết bộ thực thi `document_ops`; bỏ lớp LangChain tool |
| `utils/wordFormatter.ts` | Chuyển Markdown sang style Word | **Giữ**, dùng cho nút "Chèn vào văn bản" dưới câu trả lời |
| `pages/HomePage.vue` | Chat, nút thao tác nhanh, chèn/thay kết quả | **Thay** bằng giao diện React dùng `Composer`, `Markdown` của frontend-next |
| `pages/SettingsPage.vue` | Nhập API key từng nhà cung cấp | **Bỏ**: model do SuperAdmin cấu hình trong WeRAG |
| `api/union.ts`, LangChain/LangGraph | Agent chạy trong taskpane, gọi thẳng OpenAI/Gemini/Ollama | **Bỏ**: agent chạy ở backend, trả về `document_ops` qua SSE |
| `api/checkpoints.ts` (Dexie/IndexedDB) | Lưu lịch sử hội thoại trong trình duyệt | **Bỏ**: phiên và tin nhắn lưu ở backend |
| Vue 3, vue-i18n, vue-router | Khung UI | **Thay** bằng React 19 + `lib/i18n.tsx` (en, vi) theo quy định repo |

Khác biệt lớn nhất: Word GPT Plus để model gọi tool ngay trong taskpane, còn WeRAG để tool chạy ở server rồi gửi kế hoạch sửa xuống. Nhờ vậy add-in không cần khóa API, và dữ liệu không rời máy chủ nội bộ.

Khi chép mã từ Word GPT Plus cần giữ ghi chú bản quyền MIT trong `THIRD_PARTY_NOTICES.md`.

## 3. Kiến trúc tổng thể

Add-in chỉ thay phần hiển thị và phần thực thi: ONLYOFFICE + plugin được thay bằng Word + taskpane, còn backend giữ nguyên cách làm việc với file docx và `document_ops`.

Luồng một lượt chat:

```
 Word 2021                    Taskpane                         Backend WeRAG
 (giữ bản gốc .docx)          (frontend-next: app/word)        (agent, tools, NĐ30, MinIO)
     |                              |                                |
     |  1. getFileAsync: lấy .docx  |                                |
     |<-----------------------------|                                |
     |                              |  2. PUT .../content            |
     |                              |     (chỉ khi SHA-256 đổi)      |
     |                              |------------------------------->|
     |                              |  3. POST tin nhắn              |
     |                              |     + document_selection       |
     |                              |------------------------------->|
     |                              |  4. SSE: document_ops          |
     |                              |     (tool sửa trả về)          |
     |                              |<-------------------------------|
     |  5. Word.run: áp dụng op     |                                |
     |<=============================|                                |
     |                              |  6. PUT .../content            |
     |                              |     + ops_batch_id (mốc AI)    |
     |                              |------------------------------->|
     |                    +---------|--------------------------------|---------+
     |                    | lặp khi |  7. SSE: document_snapshot_    |         |
     |                    | tool kế |     request                    |         |
     |                    | tiếp cần|<-------------------------------|         |
     |                    | bản mới |  8. PUT .../content + nonce    |         |
     |                    |         |------------------------------->|         |
     |                    +---------|--------------------------------|---------+
     |                              |  9. SSE: câu trả lời,          |
     |                              |     kết thúc lượt              |
     |                              |<-------------------------------|
```

Bước 7–8 thay cho `forcesave` của ONLYOFFICE: backend không tự kéo được file từ Word, nên yêu cầu taskpane gửi lên và chờ tối đa 20 giây.

## 4. Đăng nhập và phiên

Taskpane là một route của frontend-next, chạy cùng origin với web, nên dùng lại nguyên `lib/api/auth.ts` và `lib/api-client.ts`: cùng JWT, refresh token, khóa tenant và cùng luồng 2FA. Không cần API key hay cơ chế đăng nhập mới.

| Bước | Web hiện tại | Taskpane |
| --- | --- | --- |
| Đăng nhập mật khẩu | `app/login`, gọi `login()` | Màn hình đăng nhập gọn trong taskpane, gọi cùng `login()` |
| 2FA (TOTP) | Nhập mã sau mật khẩu | Như web, cùng bước nhập mã |
| OIDC / SSO | Chuyển hướng sang IdP | Mở bằng `Office.context.ui.displayDialogAsync`, vì trang IdP thường chặn nhúng trong taskpane; dialog trả token về bằng `messageParent` |
| Lưu token | `localStorage` (`weknora_token`, `weknora_refresh_token`) | Cùng khóa, nằm trong hồ sơ WebView2 của Word; đăng nhập một lần cho mọi cửa sổ Word trên máy |
| Chọn tenant | `weknora_selected_tenant_id` | Dùng chung; có màn hình chọn khi tài khoản thuộc nhiều tenant |
| Hết hạn | 401 → refresh một lần rồi gọi lại | Giữ nguyên; refresh thất bại thì quay về màn đăng nhập của taskpane, không chuyển sang `/login` của web |
| Đăng xuất | Menu tài khoản | Nút trong menu taskpane |

Ràng buộc RBAC vẫn áp dụng: người dùng phải là thành viên của tenant có bật trợ lý soạn thảo, nếu không backend trả 409 "Workspace required" và taskpane phải hiển thị thông báo rõ ràng thay vì lỗi chung.

WebView2 của Word không chia sẻ cookie hay `localStorage` với Edge, nên người dùng đã đăng nhập web vẫn phải đăng nhập lại một lần trong Word.

## 5. Đồng bộ tài liệu

Word giữ bản gốc trên máy người dùng, nên add-in phải chủ động đẩy bản docx lên, thay cho cơ chế `forcesave` + callback của ONLYOFFICE. Backend vẫn xử lý trên file docx như hiện nay.

**Lấy file:** `Office.context.document.getFileAsync(Office.FileType.Compressed, {sliceSize: 4194304})` trả về nguyên file .docx theo từng slice tối đa 4 MB. API này thuộc Common API, chạy trên Word 2021. File trên 10 MB hoặc có hơn 3 MB ảnh bị từ chối như web.

**Gắn tài liệu với phiên:** lưu `session_id` và `document_id` vào `Office.context.document.settings` rồi `saveAsync`. Thiết lập này nằm trong chính file docx, nên mở lại file hôm sau sẽ vào đúng phiên cũ. Nếu file được gửi cho người dùng khác, backend trả 404 cho phiên cũ; add-in bỏ thiết lập đó và tạo phiên mới.

**Khi nào gửi lên:**

1. Mở taskpane lần đầu: tạo phiên với agent soạn thảo, gửi docx, tạo workspace kiểu `word_addin`. Kiểm tra thể thức chạy nền như khi tải lên trên web.
2. Trước mỗi lượt chat: lấy file, tính SHA-256; chỉ gửi khi khác bản đã gửi gần nhất.
3. Giữa một lượt, khi backend cần bản mới (sau khi add-in đã áp dụng op của tool trước): backend phát sự kiện SSE `document_snapshot_request {document_id, nonce}`, add-in gửi file kèm `nonce`, backend chờ tối đa 20 giây như `proposalSnapshotWait`. Hết giờ thì dùng bản gần nhất và ghi cảnh báo vào kết quả tool.
4. Sau khi áp dụng một lô op: gửi file kèm `ops_batch_id` để backend ghi mốc `document_revisions` nguồn `ai`, thay vai trò của callback status 6.
5. Khi đang mở thẻ hướng dẫn sửa lề trang: cứ 3–5 giây lấy file một lần, chỉ đọc `sectPr` (xem mục 6).

**Đoạn đã chọn:** lắng nghe `Office.EventType.DocumentSelectionChanged`, đọc `context.document.getSelection().text` sau khi vùng chọn đứng yên 120 ms, rồi gửi vào trường `document_selection`, như plugin ONLYOFFICE đang làm.

## 6. Áp dụng document_ops bằng Word API 1.3

Ngoài `pageSetup`, mọi op hiện có đều làm được trên Word 2021. Hợp đồng op giữ nguyên (`internal/agent/tools/document_ops.go`), nên backend không cần biết bên thực thi là ONLYOFFICE hay Word.

**Tìm đoạn theo anchor:** nạp `body.paragraphs` (`items/text`), chuẩn hóa bằng chính `normalizeAnchorText` của `lib/api/document-ops.ts` (NFC, bỏ ký tự rộng 0, gộp khoảng trắng), rồi lấy đoạn thứ `occurrence` có cùng văn bản. `{atStart: true}` là đoạn đầu tiên của thân văn bản. Cả lô chạy trong một `Word.run`; op nào không tìm thấy anchor thì báo vào `failed` như `ops_result` hiện nay.

| Op | Word API (WordApi 1.3) | Ghi chú |
| --- | --- | --- |
| `replaceText` | `paragraph.search(old, {matchCase: true})` → `items[0].insertText(new, "Replace")` | Search tối đa 255 ký tự: chuỗi dài hơn thì tìm đoạn đầu và đoạn cuối rồi nối bằng `range.expandTo` |
| `replaceParagraph` | `paragraph.insertText(new, "Replace")` | Giữ định dạng đoạn; chữ nhận định dạng của run đầu. Đoạn nhiều kiểu chữ thì backend gửi OOXML và add-in dùng `insertOoxml` |
| `insertAfter` | `paragraph.insertParagraph(text, "After")` | `like`: chép `alignment`, `firstLineIndent`, `leftIndent`, `lineSpacing`, `spaceBefore/After`, `font.*` từ đoạn mẫu |
| `mark` underline | `range.font.underline = "Wave"`, `font.color = "#C00000"` | Tốt hơn ONLYOFFICE: có gạch lượn sóng như lỗi chính tả |
| `mark` highlight / color | `range.font.highlightColor`, `range.font.color` | Vị trí theo `textOccurrence` trong kết quả `search` |
| `formatParagraph` | `paragraph.alignment` (Left, Centered, Right, Justified), `paragraph.font.name/size/bold/italic` | Đủ cho lỗi font, cỡ chữ, căn lề |
| `pageSetup` | `section.pageSetup.*Margin` | Chỉ khi `isSetSupported("WordApiDesktop", "1.3")`; Word 2021 không có → theo dõi + hướng dẫn |

**Nên mở rộng hợp đồng op:** thêm `firstLineIndentPt`, `lineSpacingPt`, `spaceBeforePt`, `spaceAfterPt` vào `formatParagraph`. Word 1.3 ghi được cả bốn, và đây là nhóm lỗi NĐ30 hay gặp. Plugin ONLYOFFICE cũng nên hỗ trợ cùng lúc.

**Hoàn tác:** Word không cho add-in gộp các thay đổi thành một bước undo, nên một lô nhiều op có thể cần bấm Ctrl+Z nhiều lần. Bù lại bằng nút "Khôi phục trước lần sửa AI" trong taskpane: lấy bản `document_revisions` nguồn `ai` và ghi lại bằng `body.insertFileFromBase64(base64, "Replace")` (WordApi 1.1). Cần thử trong spike xem header, footer và lề trang có được giữ không.

**Phần không ghi được (lề trang, khổ giấy, hướng trang):**

1. Phát hiện: kết quả kiểm tra NĐ30 đã có các số đo này.
2. Hướng dẫn: thẻ trong taskpane ghi giá trị hiện tại, giá trị cần đạt và các bước: Layout → Margins → Custom Margins. Nhập kèm đơn vị (`3 cm`) để không phụ thuộc cài đặt inch/cm của máy.
3. Theo dõi: khi thẻ đang mở, cứ 3–5 giây lấy file bằng `getFileAsync`, giải nén ngay trong taskpane, đọc `w:sectPr` (`w:pgSz`, `w:pgMar`) của `word/document.xml`. Đạt chuẩn thì thẻ chuyển sang trạng thái đã đạt, không cần chạy lại đánh giá bằng LLM.
4. Tùy chọn: nút "Mở bản đã sửa toàn bộ" → backend sửa bằng `docxedit` → `Word.createDocument(base64).open()` mở cửa sổ mới.

## 7. Giao diện taskpane

Taskpane là route `app/word/` trong frontend-next, rộng khoảng 320–450 px, dùng lại các component của chat web thay vì nhân bản `chat-client.tsx` (2.656 dòng). Trang `app/embed/[channelId]` là mẫu gần nhất vì nó đã là một khung chat gọn dùng `Composer` và `Markdown`.

| Thành phần web | Trong taskpane | Ghi chú |
| --- | --- | --- |
| `composer.tsx` | Dùng lại | Bỏ chọn mode và agent: taskpane luôn dùng trợ lý soạn thảo |
| `markdown.tsx`, `chat/thinking-display.tsx`, `chat/agent-steps.tsx` | Dùng lại | Các bước của agent mặc định hiển thị thu gọn |
| `chat/rewrite-proposal-card.tsx` | Dùng lại | Nút "Bấm để thay vào văn bản" gọi bộ thực thi Word |
| Chip "Đoạn đã chọn" | Dùng lại | Nguồn là `DocumentSelectionChanged` thay cho plugin |
| `doc-workspace/format-check-ring.tsx` + `format-check-report.tsx` | Dùng lại | Vòng tiến độ ở header taskpane |
| `doc-workspace/use-editor-ops.ts` | **Tách giao diện** | Tạo interface `OpsExecutor` với hai bản: ONLYOFFICE (postMessage, như hiện nay) và Word (`Word.run`) |
| `doc-workspace/revision-history.tsx` | Dùng lại, rút gọn | Khôi phục bằng `insertFileFromBase64` |
| `chat/follow-up-suggestions.tsx`, `chat/references-drawer.tsx` | Dùng lại | Nguồn trích dẫn mở trong trình duyệt ngoài bằng `Office.context.ui.openBrowserWindow` |
| `onlyoffice-editor.tsx`, `split-pane*`, `document-pane.tsx`, `sidebar.tsx` | Không dùng | Word chính là trình soạn thảo |
| Mới | Thẻ hướng dẫn thao tác | Lề trang, khổ giấy; tự chuyển sang đã đạt khi theo dõi thấy đúng |
| Mới | Danh sách phiên của tài liệu này | Một tài liệu có thể có nhiều phiên; mặc định mở phiên lưu trong `settings` |
| Từ Word GPT Plus | Nút "Chèn vào văn bản" dưới câu trả lời | Dùng `wordFormatter` để chèn văn bản Markdown thành đoạn Word tại con trỏ |

Bố cục từ trên xuống:

```
+--------------------------------------+
| Tên tài liệu      (o) NĐ30   [≡ TK]  |  header: vòng kiểm tra, menu tài khoản
+--------------------------------------+
| [!] Lề trái 25 mm, cần 30–35 mm      |  thẻ hướng dẫn (nếu có)
|     Layout → Margins → Custom ...    |
+--------------------------------------+
|  tin nhắn / bước agent / thẻ đề xuất |
|  ...                                 |
+--------------------------------------+
| [Đoạn đã chọn: "Điều 3. ..."]   (x)  |  chip đoạn chọn
| [ Nhập yêu cầu...            ] [>]   |  ô soạn tin
+--------------------------------------+
```

Chuỗi giao diện mới chỉ thêm vào `lib/i18n.tsx` cho `en` và `vi`.

### Trang cài đặt

Nút bánh răng ở header mở trang cài đặt của taskpane. Taskpane là một trang duy nhất và chuyển màn bằng state, vì `office.js` xóa `history.pushState` trong Word.

| Mục | Nội dung | Dùng lại |
| --- | --- | --- |
| Tài khoản | Ảnh đại diện (tải lên), tên hiển thị, email | `components/settings/profile-settings.tsx` |
| Mật khẩu & bảo mật | Đổi mật khẩu (đăng nhập lại ngay trong taskpane), bật/tắt 2FA | `components/settings/security-settings.tsx` |
| Không gian làm việc | Danh sách tenant của tài khoản, chuyển tenant. Mỗi tenant có phiên riêng của văn bản (lưu trong `settings` của file theo tenant) | `useAuth().setSelectedTenant` |
| Lịch sử | Các cuộc trò chuyện về văn bản này (mở lại trong taskpane); các phiên bản đã lưu (khôi phục vào Word bằng `insertFileFromBase64`, hoặc mở bản đã lưu ở cửa sổ mới); mọi cuộc trò chuyện gần đây (mở trong taskpane hoặc trên web) | API sessions, revisions |
| Ngôn ngữ | Tiếng Việt / English | `useT().setLocale` |
| Đăng xuất | Xóa token trong WebView2 | `useAuth().logout` |

## 8. Thay đổi backend

Thay đổi lớn nhất nằm ở `document_workspace.go`: workspace phải biết bên nào đang giữ bản gốc. Agent, tool, `document_ops` và kiểm tra NĐ30 không cần sửa.

| Thay đổi | Vị trí | Nội dung |
| --- | --- | --- |
| Cột `editor_kind` | `document_workspaces`, migration 000125 (postgres) + migration sqlite tương ứng | `onlyoffice` (mặc định) hoặc `word_addin` |
| Tạo workspace từ add-in | `POST /sessions/:id/documents` | Nhận thêm `editor_kind: "word_addin"`; không tạo cấu hình ONLYOFFICE, không cần `ONLYOFFICE_*` |
| Upload bản mới | `PUT /sessions/:id/documents/:doc/content` (mới) | Thân là file docx, kèm `sha256`, `nonce?`, `ops_batch_id?`; cập nhật `CurrentRef`, `last_saved_at`; có `ops_batch_id` thì ghi mốc `document_revisions` nguồn `ai` |
| Flush trước khi tool đọc | `flushEditor`, `PrepareExternalWrite`, `ForceSave` | Với `word_addin`: phát SSE `document_snapshot_request` rồi chờ upload có đúng `nonce`, tối đa 20 giây; không gọi Document Server |
| Kiểm tra bật tắt | `Enabled()` ở các hàm dòng 187, 395, 495, 582 | Chỉ bắt buộc với workspace `onlyoffice` |
| Khôi phục revision | `POST …/revisions/:seq/restore` | Với `word_addin`: trả file docx để add-in tự ghi vào Word, không xoay `editor_key` |
| Bản đã sửa toàn bộ | `GET …/format-check/fixed-docx` (mới) | Dùng `docxedit` áp dụng mọi sửa thể thức, kể cả lề trang, trả docx cho `Word.createDocument` |
| Mở rộng op | `DocumentOp` trong `tools/document_ops.go` | Thêm `firstLineIndentPt`, `lineSpacingPt`, `spaceBeforePt`, `spaceAfterPt` cho `formatParagraph` |
| CORS | `internal/router/router.go:125` | Taskpane cùng origin với frontend-next nên không cần mở thêm; nên thu hẹp `*` về danh sách origin cụ thể |

Trước khi sửa `flushEditor`, `PrepareExternalWrite`, `ForceSave` và `DocumentOp` phải chạy `impact` của GitNexus và báo mức rủi ro, theo quy định trong `CLAUDE.md`.

## 9. Triển khai

Word 2021 bản volume không có Centralized Deployment của M365, nên add-in được phân phối qua thư mục chia sẻ (shared folder catalog). Mỗi máy trạm cần có đủ các điều kiện sau:

| Điều kiện | Cách đáp ứng |
| --- | --- |
| Manifest XML | Sinh từ mẫu `release/self-hosted/manifest.xml` của Word GPT Plus; `SourceLocation` = `https://<máy chủ WeRAG>/word/`; ID riêng của WeRAG; quyền `ReadWriteDocument`; lưu trong repo tại `deploy/word-addin/manifest.xml` |
| Thư mục catalog | Thư mục chia sẻ UNC (`\\server\werag-addin`) chứa manifest; đẩy vào Trusted Add-in Catalogs bằng GPO (registry `HKCU\Software\Microsoft\Office\16.0\WEF\TrustedCatalogs`). Người dùng thêm add-in một lần tại Insert → My Add-ins → Shared Folder |
| HTTPS | Chứng chỉ cho tên miền máy chủ WeRAG, do CA nội bộ cấp và được tin cậy trên máy trạm (đẩy qua GPO). Taskpane không chạy trên `http://` |
| `office.js` | Tải từ CDN của Microsoft (xem mục 10) |
| WebView2 Runtime | Word 2021 trên Windows 10 (1903 trở lên) hoặc 11 dùng WebView2 khi đã cài; cài Evergreen Runtime cho máy còn thiếu. Không có WebView2 thì Word rơi về EdgeHTML/IE11 và React 19 không chạy |

Phía máy chủ: frontend-next phải được triển khai thật (hiện mới chạy dev trên `:3100`), đặt sau nginx có TLS, với `/api` và `/files` chuyển về backend như `next.config.ts` đang làm. Chỉ cần một origin cho cả web và taskpane.

## 10. Office.js có phải tải mỗi lần không?

Có. **Mỗi lần mở taskpane, trang add-in nạp lại `office.js`.** Thực tế việc nạp này nhẹ hơn tên gọi:

- `office.js` chỉ là file khởi động nhỏ. Nó nạp thêm vài file riêng cho Word trên Windows từ cùng CDN (`appsforoffice.microsoft.com`).
- WebView2 có HTTP cache riêng, nên từ lần thứ hai trở đi file thường lấy từ cache hoặc chỉ hỏi lại máy chủ xem có bản mới không. Thời gian chờ không đáng kể.
- Tuy vậy, cache có hạn. Microsoft cũng chỉ hỗ trợ chính thức việc nạp từ CDN, để các bản sửa lỗi đến add-in nhanh. Vì vậy máy trạm (hoặc proxy) **phải luôn truy cập được** `appsforoffice.microsoft.com`, không chỉ ở lần đầu.

Nếu mạng nội bộ không được ra Internet, có ba cách:

| Cách | Ưu | Nhược |
| --- | --- | --- |
| Mở riêng `appsforoffice.microsoft.com` qua proxy (khuyến nghị) | Đúng cách Microsoft hỗ trợ; máy trạm không ra Internet ở chỗ khác | Cần quản trị mạng đồng ý |
| Tự host bản sao `office.js` trên máy chủ WeRAG (gói npm `@microsoft/office-js`) | Chạy được trong mạng kín | Microsoft ghi rõ gói npm "no longer officially supported"; phải tự cập nhật. Rủi ro thấp hơn với Office 2021 LTSC vì bản này không nhận tính năng mới, nhưng vẫn cần thử trong spike |
| Chuyển sang VSTO (C#) | Chạy offline hoàn toàn | Viết lại giao diện; cài MSI trên từng máy |

Nguồn: [OfficeDev/office-js](https://github.com/OfficeDev/office-js) ("The Office CDN is the official supported source for Office Add-ins").

## 11. Rủi ro và câu hỏi mở

| Rủi ro | Ảnh hưởng | Cách xử lý |
| --- | --- | --- |
| Máy trạm không ra được `appsforoffice.microsoft.com` | Add-in không chạy | Mở riêng tên miền qua proxy; nếu không thể thì tự host (mục 10) hoặc xét VSTO |
| Thiếu WebView2 trên một số máy | Taskpane trắng | Kiểm kê và cài Evergreen Runtime trước |
| Người dùng gõ trong lúc AI đang trả lời | Anchor lệch, một số op không áp dụng được | Báo rõ op lỗi như `ops_result.failed`; đề nghị chạy lại |
| Ctrl+Z phải bấm nhiều lần cho một lô op | Khó hoàn tác trọn vẹn | Nút khôi phục từ `document_revisions` |
| Gửi docx mỗi lượt với file gần 10 MB | Chậm 1–2 giây mỗi lượt | Chỉ gửi khi SHA-256 đổi |
| `insertFileFromBase64` có thể không giữ header, footer, lề trang trên Word 2021 | Khôi phục không trọn vẹn | Thử trong spike; nếu thiếu thì khôi phục bằng cách mở bản cũ qua `Word.createDocument` |
| Mã chép từ Word GPT Plus | Nghĩa vụ giấy phép | MIT: giữ ghi chú bản quyền trong `THIRD_PARTY_NOTICES.md` |

**Câu hỏi mở:**

- [ ] Máy trạm của cơ quan có truy cập được `appsforoffice.microsoft.com` không?
- [ ] Máy chủ WeRAG sẽ dùng tên miền và chứng chỉ HTTPS nào?
- [ ] Ai quản trị GPO để đẩy catalog, chứng chỉ và WebView2?
- [ ] Office 2021 là bản volume (LTSC) hay retail? Bản retail có thể có WordApi cao hơn và sửa được lề trang.
- [ ] Taskpane sẽ thay hẳn giao diện ONLYOFFICE hay chạy song song?

## 12. Lộ trình

Bắt đầu bằng spike 2–3 ngày trên máy Word 2021 thật. Các giai đoạn sau chưa ước lượng thời gian cho tới khi spike xong.

```
[0. Spike trên máy thật — 2–3 ngày]
    Manifest + taskpane tối thiểu; thử getFileAsync, replaceText, mark
    Thử insertOoxml, đọc sectPr, insertFileFromBase64, office.js tự host
        |
     <CỔNG> chạy được trên máy cơ quan (WebView2, office.js, HTTPS)?
        |     không → xét VSTO
        v
[1. Backend — workspace word_addin]
    Cột editor_kind, PUT .../content, document_snapshot_request trong flush
    Khôi phục revision trả docx; chạy impact GitNexus trước khi sửa
        |
     <CỔNG> test service xanh; tool đọc đúng bản do add-in gửi lên
        v
[2. Taskpane cơ bản — app/word]
    Đăng nhập, 2FA, tenant; chat, chip đoạn chọn, thẻ đề xuất viết lại
    OpsExecutor cho Word, tách từ use-editor-ops.ts
        |
     <CỔNG> người dùng sửa trọn một công văn thật trong Word
        v
[3. Thể thức NĐ30 — theo dõi + hướng dẫn]
    Vòng kiểm tra; thẻ hướng dẫn lề trang tự chuyển sang đã đạt; mở bản đã sửa
    Mở rộng formatParagraph: thụt dòng, giãn dòng, khoảng cách đoạn
        |
     <CỔNG> sau khi sửa, báo cáo NĐ30 không còn lỗi mà API sửa được
        v
[4. Triển khai — GPO, HTTPS]
    Deploy frontend-next sau nginx TLS; manifest trong catalog chia sẻ
    GPO: Trusted Catalog, chứng chỉ CA nội bộ, WebView2 Runtime
```

Giai đoạn 1 và 2 có thể chạy song song sau cổng đầu, vì taskpane có thể làm trước với workspace tạo tay.

## Nguồn

- [Word JavaScript API requirement sets](https://learn.microsoft.com/en-us/javascript/api/requirement-sets/word/word-api-requirement-sets): Office 2021 volume dừng ở WordApi 1.3
- [Word.PageSetup](https://learn.microsoft.com/en-us/javascript/api/word/word.pagesetup): lề trang thuộc WordApiDesktop 1.3
- [OfficeDev/office-js](https://github.com/OfficeDev/office-js): CDN là nguồn được hỗ trợ chính thức
- [Word GPT Plus](https://github.com/Kuingsmile/word-GPT-Plus): bản tham khảo, MIT
- Trong repo: `internal/agent/tools/document_ops.go`, `docker/onlyoffice/plugins/werag-assistant/`, `internal/application/service/document_workspace.go`, `frontend-next/lib/api/document-workspace.ts`
