# Thiết kế: skill pháp luật tích hợp chỉ-đọc cho mọi Smart Reasoning agent

Ngày: 2026-09-30

## Mục tiêu và phạm vi

Tích hợp bốn Agent Skills pháp luật Việt Nam vào ứng dụng WeKnora như tài nguyên chỉ-đọc, khả dụng **mặc định trong mọi lượt Smart Reasoning thông thường**, không cần sandbox hoặc thao tác cài zip/cấu hình tenant:

- `legal-document-summary`
- `legal-document-comparison`
- `legal-latest-guidance`
- `legal-question-abbreviations`

Các `SKILL.md` vẫn được lưu trong Git để chỉnh sửa, review, phát hành cùng phiên bản ứng dụng. Chúng chỉ chứa hướng dẫn, không có script. Nội dung skill không thay thế cơ chế tìm kiếm, quyền KB, nguồn trích dẫn hoặc chốt phân giải từ viết tắt đã có ở backend. Không thay đổi Quick Answer/RAG hay luồng cài skill nội bộ. Không tự thêm tool web, shell, sandbox hay quyền truy cập dữ liệu.

## Bối cảnh và phương án

Hiện runtime Smart Reasoning tạo skill manager khi `SkillsEnabled && (SkillDirs hoặc TenantSkills)`, với `TenantSkills` đến từ snapshot của cấu hình sandbox; một agent không bật sandbox hoặc cấu hình skill `none` không thấy skill. `read_file` có khả năng đọc `skill://` mà không cần sandbox filesystem, nhưng hiện chỉ được gắn skill manager khi nguồn cũ tồn tại. Cấu hình `none`/`selected` hiện chi phối tất cả skill; cần tách quyền xem skill tích hợp khỏi quyền skill tenant.

Các phương án đã đối chiếu:

1. **Chọn:** đóng gói `SKILL.md` từ repo trong binary, thêm nguồn skill tích hợp chỉ-đọc vào đường đọc/danh mục của Smart Reasoning. Metadata hiển thị trước, body tải qua `skill://` theo nhu cầu. Không cài vào image.
2. Nhúng nguyên văn bốn hướng dẫn vào system prompt mỗi lượt: dễ nối nhưng làm phình prompt, trái progressive disclosure.
3. Tự cài qua catalog/sandbox: tái dùng luồng tenant nhưng vẫn phụ thuộc sandbox và có tác dụng phụ trên cấu hình người dùng.

## Nguồn nội dung và cấu trúc

Giữ **một nguồn nội dung duy nhất** là bốn thư mục `examples/skills/legal-*/SKILL.md` hiện có (skill từ viết tắt hiện chưa commit). Thêm Go asset package ngay trong `examples/skills/` với danh sách `go:embed` **tường minh** của bốn file này, không wildcard bắt nhầm ví dụ PDF hay tài liệu khác. Nguồn nội bộ của `internal/agent/skills` nhận `fs.FS` chứa các file đó, parse frontmatter bằng parser hiện có, kiểm tra tên mỗi file khớp tên thư mục, tên không trùng, body không trống và chỉ công bố file hợp lệ. Lỗi asset tích hợp là lỗi cấu hình/build: báo rõ khi khởi tạo engine hoặc trong test build, không âm thầm bỏ qua.

Nguồn tích hợp trả metadata (Level 1), body `SKILL.md` (Level 2) và danh sách file tối thiểu. Không cấp host path có thể dùng làm script, không staging vào sandbox. Trong lần đầu chỉ bốn gói markdown, không thêm tài nguyên Level 3. Có thể giữ giao diện `SkillSource` chung để đọc, nhưng phải thêm phân loại nguồn **read-only, không executable**; `PrepareShellEnvironment` và `SandboxSkillDir` không bao giờ biến built-in thành host-skill script. Bất kể manager có shell cho skill tenant, `shell_exec(skill_name=<built-in>)` phải trả lỗi và không stage/exec.

## Ghép nguồn và quyền

Bốn built-in luôn hiển thị trong Smart Reasoning bình thường, kể cả `SkillsSelectionMode=none` hoặc `selected` và không có `SandboxConfigID`. `none`/`selected` tiếp tục điều khiển **skill tenant** như hiện tại; không đổi quyền của các gói tenant đã cài hoặc các công cụ sandbox. Agent chế độ SkillInstallMode không nhận bốn built-in để không làm nhiễu installer.

Khi có `TenantSkills`, manager phải ghép danh mục nội bộ + tenant theo tên duy nhất và thứ tự ổn định. Tên built-in là **tên dành riêng**: nếu tenant có gói cùng tên, metadata/`read_file`/pinned mention lấy built-in, tenant duplicate bị ẩn khỏi agent và không được chạy bằng `shell_exec(skill_name=...)`; không xóa hoặc tự sửa row tenant. Không hồi tiếp sang loader host để lấy file cùng tên. Lỗi trùng tên được ghi rõ trong tài liệu và kiểm thử.

`read_file(path="skill://<name>/SKILL.md")` phải được đăng ký khi có built-in dù không có `SessionFileStore` hay shell; nguồn đọc chỉ gồm các skill được liệt kê, vẫn chặn đường dẫn vượt scope và giới hạn phân trang/byte hiện có. Không thêm `list_sandbox_files`, writer, `shell_exec` hoặc sandbox manager chỉ vì built-in tồn tại. Prompt giới thiệu tên + mô tả, yêu cầu agent đọc SKILL.md phù hợp; không nhúng toàn bộ nội dung mọi lượt. `@Skill` (nếu có) chỉ được pin tên thực sự trong danh mục đang khả dụng; tên trùng pin tới bản built-in.

## Tương tác với các chế độ và triển khai

Tích hợp ở **đường tạo Agent Engine Smart Reasoning**. Custom system prompt của agent vẫn nhận metadata theo cơ chế lắp prompt hiện tại; hướng dẫn không vượt quyền/tool của cấu hình phiên. Khi cập nhật file Markdown, cần build và triển khai binary mới; không tự đổi nội dung của phiên đang chạy hoặc gói tenant đã cài. Skill catalog theo tenant và UI cấu hình skill không cần thay đổi để dùng bốn built-in; `examples/skills/README.md` phải nêu bốn skill này nay có sẵn trong Smart Reasoning và **không còn yêu cầu zip/sandbox** ở chế độ tích hợp, trong khi phần hướng dẫn cài skill tùy biến vẫn đúng cho gói khác. Giao diện picker nếu chỉ hiển thị skill tenant không phải điều kiện kích hoạt built-in; document rõ khác biệt này, không khẳng định UI có nút cài built-in.

## Kiểm thử và nghiệm thu

- Test nguồn nhúng: đủ đúng bốn tên, frontmatter/body đọc được, tên/thư mục khớp, không lẫn `pdf-processing`, phát hiện gói sai/duplicate; đọc không phụ thuộc filesystem repo lúc runtime.
- Test Smart Reasoning engine **không sandbox**, `SkillsSelectionMode=none`: metadata bốn tên trong prompt và `read_file(skill://.../SKILL.md)` thành công; không có shell/list/write/file sandbox.
- Test `selected` với tenant skill, `all` với tenant skill, không sandbox, tên trùng: danh mục, allowlist, `read_file`, pinned @mention, và môi trường `shell_exec(skill_name=...)` đúng nguồn; built-in không thể được thực thi dù tenant có bản trùng tên.
- Test SkillInstallMode không thấy bốn built-in; Quick Answer/RAG không đổi; các test đọc file từ web/sandbox và tenant source hiện có vẫn đạt.
- Replay các bài thử hành vi của bốn skill (trích dẫn, ưu tiên văn bản mới trong cùng phạm vi, không tra web, không đoán nghĩa viết tắt; phân biệt `ready` không mapping với `needs_definition`). Báo rõ đây là thử agent mô phỏng, chưa phải chạy live WeKnora nếu không có môi trường.
- Chạy test Go liên quan và đối chiếu `go test ./...` với hai lỗi đã tái hiện ở `internal/application/service` và `internal/application/service/memory`; không sửa các thay đổi đang dở của công việc khác chỉ để làm xanh bộ test.

## Điều kiện triển khai / an toàn worktree

Repo yêu cầu **GitNexus impact upstream trước khi sửa bất kỳ function/class/method** và `detect_changes` trước commit. Index hiện stale và CLI báo WAL không đọc được sau lần analyze quá thời gian; trước khi sửa code cần khôi phục index an toàn rồi chạy impact cho từng symbol dự định sửa, báo blast radius và cảnh báo HIGH/CRITICAL cho người dùng. Không xóa file index/WAL hoặc ghi đè công việc hiện tại để 'sửa' index. Nếu GitNexus không hoạt động, dừng trước khi sửa symbol và xin chỉ dẫn, không tự bỏ qua gate. Chỉ stage/commit file thuộc phạm vi tính năng sau khi `detect_changes` thành công; giữ nguyên các chỉnh sửa không liên quan đang có trong worktree.
