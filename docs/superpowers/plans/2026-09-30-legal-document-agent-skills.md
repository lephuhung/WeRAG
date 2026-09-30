# Legal Document Agent Skills Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Thêm ba Agent Skills cài chọn lọc cho agent WeKnora để tóm tắt, so sánh và ưu tiên hướng dẫn mới hơn trong các văn bản pháp luật đã có trong hệ thống.

**Architecture:** Ba `SKILL.md` độc lập dưới `examples/skills/`, không script, không thay đổi backend/UI. Hướng dẫn chỉ dùng nguồn nội bộ được agent phép đọc; README giải thích cách cài vào cấu hình sandbox/agent. Mỗi skill trải qua vòng thử RED → GREEN → cải thiện riêng, không tạo hàng loạt.

**Tech Stack:** Markdown + YAML frontmatter theo `docs/agent-skills.md`; các công cụ truy xuất tài liệu được cấp cho agent; Go tests sẵn có để kiểm tra parser (`go test ./internal/agent/skills`).

**Spec:** `docs/superpowers/specs/2026-09-30-legal-document-agent-skills-design.md`

## Global Constraints

- Nguồn chứng cứ chỉ từ tài liệu agent đọc được trong WeKnora; không web, không suy diễn rằng kho bao phủ toàn bộ pháp luật.
- Văn bản mới hơn chỉ được ưu tiên khi cùng vấn đề/phạm vi, đủ căn cứ thời điểm; ngày mới hơn không tự chứng minh sửa đổi/thay thế/hết hiệu lực.
- Các nhận định phải có nguồn đến mức điều/khoản/điểm hoặc đoạn khi có; không bịa nguồn, không xem đoạn tìm kiếm sơ lược là toàn văn.
- Frontmatter gồm `name` ASCII kebab-case (<50 ký tự) và `description` kích hoạt theo tình huống (<500 ký tự); mỗi skill là một thư mục `SKILL.md`.
- Không sửa backend, frontend, các file đang thay đổi từ công việc khác; không dùng ví dụ hư cấu như căn cứ pháp luật thật trong skill.
- Trước mọi commit, chạy `node .gitnexus/run.cjs detect-changes --scope staged`, `git diff --cached --check`, xác nhận chỉ stage file của task. Index GitNexus có thể stale; không sửa code symbols trong kế hoạch này.

## File Map

- Create `examples/skills/legal-document-summary/SKILL.md`: phương pháp và dạng trả lời tóm tắt.
- Create `examples/skills/legal-document-comparison/SKILL.md`: phương pháp và bảng đối chiếu.
- Create `examples/skills/legal-latest-guidance/SKILL.md`: chọn hướng dẫn mới hơn trong kho và nêu giới hạn.
- Modify `examples/skills/README.md`: liệt kê ba skill, cài qua upload zip/URL công khai vào cấu hình sandbox rồi bật/select Skills ở agent; không coi thư mục examples là tự động cài.

## Test Harness (áp dụng tuần tự cho từng task)

Dùng một agent/subagent **ngữ cảnh mới** để chạy mỗi tình huống RED khi `SKILL.md` của task chưa tồn tại, chỉ đưa dữ liệu giả lập và câu hỏi. Ghi phản hồi nguyên văn và chấm tiêu chí (không mặc định baseline phải thất bại; nếu baseline đã đúng hoàn toàn, đổi tình huống để lộ thiếu sót thực). Sau đó tạo skill tối thiểu đáp ứng thiếu sót đã quan sát, chạy lại tình huống trong ngữ cảnh mới có đính kèm nội dung skill, chấm GREEN; nếu còn sai thì chỉnh skill và chạy lại. Đây là thử hành vi mô phỏng, **không** thay cho kiểm thử live agent được cài trong sandbox; không khẳng định đã có kiểm thử live nếu chưa thực hiện.

Mỗi tình huống dùng đoạn **giả lập để thử cách lập luận**, không phải văn bản pháp luật có thật; ví dụ cụ thể ở từng task dưới đây. Điều kiện chung: agent không truy cập web; trích dẫn bằng nhãn tài liệu + điều/khoản có trong đề bài; không được đoán phần văn bản chưa cung cấp. Có thể dùng `subagent` sau khi `action:list`, chỉ agent executable; giữ một writer ở cwd này. Không cần chép toàn bộ spec vào tình huống RED vì sẽ làm sai baseline.

---

### Task 1: Tóm tắt văn bản

**Files:** Create `examples/skills/legal-document-summary/SKILL.md`.

**Interface:** Consumes đoạn/tài liệu truy xuất nội bộ cùng yêu cầu tóm tắt; produces tóm tắt có thông tin nhận diện, phạm vi/đối tượng, các điểm chính và nguồn; không phán đoán hiệu lực hiện tại.

- [ ] **Step 1 RED:** Chạy agent ngữ cảnh mới với prompt: `Chỉ dùng trích đoạn trong kho: VB-A (giả lập), số 01/2025, cơ quan X, ban hành 01/02/2025; Điều 1: hướng dẫn đối tượng nhóm A nộp mẫu M; Điều 2: có hiệu lực từ 01/04/2025. Hãy tóm tắt và cho biết đây có phải hướng dẫn mới nhất hiện nay không? Không có tài liệu khác.` Tiêu chí: tóm tắt Điều 1/2 có nguồn, từ chối khẳng định mới nhất. Ghi đầu ra và thiếu sót; nếu baseline hoàn hảo, biến thể chỉ cấp tiêu đề/đoạn Điều 1 mà không cấp Điều 2 để thử giới hạn thông tin.
- [ ] **Step 2 GREEN:** Viết frontmatter `name: legal-document-summary`, mô tả `Use when a user asks to summarize a Vietnamese legal document or identify its scope, key provisions, and stated dates from documents available inside WeKnora.` Nội dung gọn gồm: xác định văn bản/đọc đoạn đủ; ghi thông tin chưa thấy; tóm tắt theo điều khoản có trích nguồn; ngày hiệu lực chỉ khi có căn cứ; không khẳng định mới nhất/hiện hành chỉ từ một tài liệu; ví dụ định dạng kết quả ngắn.
- [ ] **Step 3 verify:** Chạy lại prompt RED với skill được cung cấp rõ qua `SKILL.md` ở agent thử, đối chiếu hai kết quả; kiểm tra không tự tra web. Nếu sai, điều chỉnh và thử lại trước task 2. Chạy `go test ./internal/agent/skills` từ root (parser regression) và kiểm tra frontmatter thủ công.
- [ ] **Step 4 commit (nếu người dùng muốn commit):** Chỉ stage file skill vừa tạo; chạy GitNexus staged + diff check + `git diff --cached --name-only`; commit `docs(skills): add legal document summary`.

### Task 2: So sánh văn bản

**Files:** Create `examples/skills/legal-document-comparison/SKILL.md`.

**Interface:** Consumes ít nhất hai tài liệu và vấn đề đối chiếu; produces bảng đối chiếu từng vấn đề với nguồn riêng từng bên, khác biệt/phạm vi, không tự kết luận thay thế.

- [ ] **Step 1 RED:** Prompt thử: `Chỉ dùng hai đoạn giả lập trong kho: VB-A Điều 1 (nhóm A nộp mẫu M, hiệu lực 2024); VB-B Điều 1 (nhóm B nộp mẫu N, hiệu lực 2025). So sánh hai văn bản và nói VB-B đã thay thế VB-A chưa. Không có điều khoản thay thế nào được cung cấp.` Tiêu chí: bảng đối chiếu có trích Điều 1 hai bên; khác đối tượng; không suy luận thay thế. Nếu baseline đúng, thử biến thể VB-B không có điều khoản tương ứng: câu trả lời phải nói chưa tìm thấy chứ không nói đã bãi bỏ. Ghi nguyên văn lỗi nền nếu có.
- [ ] **Step 2 GREEN:** Viết frontmatter `name: legal-document-comparison`, mô tả `Use when comparing two or more Vietnamese legal documents, provisions, or versions on the same issue using documents available inside WeKnora.` Nội dung gọn: đọc đủ hai bên, so phạm vi/đối tượng/thời điểm, bảng `Vấn đề | Văn bản A + nguồn | Văn bản B + nguồn | Nhận xét`, phân biệt khác biệt với quan hệ sửa đổi/thay thế, nêu chưa tìm thấy khi thiếu đoạn.
- [ ] **Step 3 verify:** Thử lại prompt RED + biến thể với skill, sửa và chạy lại nếu không đạt; kiểm tra frontmatter, `go test ./internal/agent/skills`.
- [ ] **Step 4 commit (nếu người dùng muốn commit):** Chỉ stage file mới, GitNexus staged + diff check + name-only; commit `docs(skills): add legal document comparison`.

### Task 3: Hướng dẫn mới hơn trong kho

**Files:** Create `examples/skills/legal-latest-guidance/SKILL.md`.

**Interface:** Consumes câu hỏi pháp luật theo thời điểm và những tài liệu nội bộ có thể truy xuất; produces trả lời ưu tiên văn bản mới hơn khi đủ căn cứ cùng phạm vi, nêu lý do và giới hạn tập nguồn.

- [ ] **Step 1 RED:** Prompt thử: `Chỉ dùng hai đoạn giả lập trong kho: VB-A Điều 3 (nhóm A dùng mẫu M, có hiệu lực 01/01/2024); VB-B Điều 3 (nhóm A dùng mẫu N, có hiệu lực 01/06/2025; không ghi thay thế VB-A). Hỏi: nhóm A dùng mẫu nào vào 01/07/2025? Đây là toàn bộ tài liệu bạn truy cập.` Tiêu chí: ưu tiên N, dẫn VB-B Điều 3, nêu VB-A khác, không kết luận VB-A hết hiệu lực và không tuyên bố là mới nhất ngoài kho. Biến thể: hỏi ngày 01/03/2025 → không ưu tiên N; biến thể thiếu ngày hiệu lực VB-B → chỉ so ngày ban hành, chưa kết luận hiệu lực. Ghi đầu ra/lỗi nền.
- [ ] **Step 2 GREEN:** Viết frontmatter `name: legal-latest-guidance`, mô tả `Use when answering a Vietnamese legal question where multiple internal documents address the same issue and a later document may give newer guidance.` Nội dung: tìm nhiều văn bản trong kho được phép, đọc điều khoản, lọc cùng phạm vi/đối tượng/cấp văn bản, so ngày hiệu lực với thời điểm hỏi (nếu thiếu chỉ nêu thứ tự ban hành), chọn mới hơn khi đủ căn cứ, dẫn nguồn nội bộ, nêu sự khác biệt với văn bản cũ, không suy luận trạng thái pháp lý nếu thiếu điều khoản, không tìm web, báo giới hạn kho.
- [ ] **Step 3 verify:** Thử lại RED và biến thể với skill; sửa đến khi đạt; kiểm tra frontmatter, `go test ./internal/agent/skills`.
- [ ] **Step 4 commit (nếu người dùng muốn commit):** Chỉ stage file mới, GitNexus staged + diff check + name-only; commit `docs(skills): add internal legal latest guidance`.

### Task 4: Tài liệu cài đặt và xác minh tổng thể

**Files:** Modify `examples/skills/README.md` ở phần danh sách/cách cài skill (không động vào hướng dẫn pdf hiện có).

**Interface:** Produces hướng dẫn cho người vận hành cài ba skill riêng vào cấu hình sandbox rồi gắn agent ở chế độ smart-reasoning, với điều kiện cần có tài liệu và quyền truy vấn kho; không tạo cơ chế cài tự động.

- [ ] **Step 1:** Đối chiếu `docs/agent-skills.md` về cấu hình, luồng upload zip/URL công khai, `read_file(skill://.../SKILL.md)` và `skills_enabled` trước khi ghi hướng dẫn; nếu README hiện có gây hiểu nhầm nơi đặt file đồng nghĩa tự cài, ghi rõ cần cài trong cấu hình sandbox.
- [ ] **Step 2:** Bổ sung bảng ba tên và trigger tiếng Việt, cách cài chọn lọc và giới hạn dữ liệu nội bộ; không nêu tên công cụ truy vấn chưa được đảm bảo có trong cấu hình agent.
- [ ] **Step 3:** Kiểm tra file đã tồn tại/links đúng và không có placeholder: `find examples/skills/legal-* -name SKILL.md -print; rg -n 'legal-document-summary|legal-document-comparison|legal-latest-guidance' examples/skills/README.md; go test ./internal/agent/skills; git diff --check`. Dùng `git status --short` để thấy các thay đổi có sẵn, không nhận chúng là thay đổi của mình.
- [ ] **Step 4 commit (nếu người dùng muốn commit):** Chỉ stage README, GitNexus staged + diff check + name-only; commit `docs(skills): document legal agent skills`.

## Completion gate

Kiểm tra lại ba frontmatter, ba tình huống GREEN, link README, diff và kết quả `go test ./internal/agent/skills`. Báo rõ thử bằng subagent mô phỏng hay agent WeKnora thật; không đánh đồng. Không khẳng định hệ thống tự biết có văn bản mới hơn ngoài kho. Trước commit cuối, `node .gitnexus/run.cjs detect-changes --scope staged` và chỉ commit file trong phạm vi kế hoạch.
