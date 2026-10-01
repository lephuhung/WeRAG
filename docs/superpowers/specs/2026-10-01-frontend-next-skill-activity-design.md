# Frontend-next: hiển thị skill đã dùng trong thinking pipeline

Ngày: 2026-10-01

## Phạm vi và định nghĩa

Chỉ sửa giao diện chat `frontend-next`; không sửa Vue `frontend/`, backend, API hay quyền skill. Trong thinking pipeline của mỗi lượt Smart Reasoning, cho người dùng biết **tên skill thực sự đã dùng** bằng nhãn ngắn trên chính dòng tool tương ứng:

- `read_file` đọc thành công đúng `skill://<name>/SKILL.md`: **Dùng skill: <name>** (skill tích hợp chỉ-đọc và skill tenant đều tính). Đọc tài nguyên khác như `FORMS.md` không tạo nhãn này.
- `shell_exec` thành công với `skill_name` không rỗng: **Chạy skill: <name>** (skill tenant). Lệnh shell không có `skill_name` không tạo nhãn skill.
- Skill chỉ được nhắc trong prompt, `@mention`, tool call đang pending, bị ngắt, hoặc tool result thất bại **không được ghi là đã dùng thành công**. Trạng thái pending/lỗi giữ cách hiển thị phù hợp, không đánh đồng với thành công.

Không tạo dòng sự kiện thứ hai hoặc bảng tổng hợp. Dòng skill nằm trong timeline tool đang có; nội dung chi tiết tool không bị mất. Nhãn skill không in lệnh shell, biến môi trường, đầu ra tool hay dữ liệu nhạy cảm. Sử dụng i18n hiện có cho `en`, `vi`, `zh`.

## Phương án và lý do chọn

1. **Chọn: suy ra từ kết quả tool đã xác nhận trong `frontend-next`**. SSE mang cặp `tool_call` (arguments/path/skill_name) và `tool_result` (success/tool_call_id); `agent_steps` lịch sử lưu args/result. Một hàm nhận diện/định dạng dùng chung cho hai nhánh hiển thị sẽ tạo nhãn có cùng ngữ nghĩa mà không đổi giao thức.
2. Backend phát `skill_used` event mới: audit chặt hơn, nhưng cần đồng bộ stream, lưu lịch sử và UI, dễ nhân đôi một thao tác trong timeline; không cần cho nhu cầu chỉ nhìn tên skill.
3. Suy ra từ văn bản thinking/metadata/`@mention`: không đáng tin vì agent có thể nêu tên mà chưa đọc/chạy skill.

## Dữ liệu và hiển thị

Nguồn sự thật cho thành công là **kết quả tool đã nhận**, không phải trạng thái `success` do `finalizeSteps` gán cho pending khi lượt kết thúc. Tại `applyChunkToSteps` của `frontend-next/components/chat/agent-steps.tsx`, ghép tool result với tool call bằng `tool_call_id` và lưu dấu xác nhận thành công; khi dựng `stepsFromHistory`, chỉ coi `call.result?.success === true` là xác nhận. Các lượt chỉ có tool call không có result, lỗi, hoặc bị ngắt không có nhãn “Dùng/Chạy skill”. Nếu stream phát lại sự kiện, một tool call ID vẫn chỉ có một dòng.

Chỉ nhận diện tên là một đoạn duy nhất dài 1–64 ký tự chữ/số Unicode, `_` hoặc `-`; không chấp nhận dấu `/`, path traversal, query/fragment hay tên rỗng. Lấy tên từ kết quả `read_file` (`skill_name` cùng `file_path=SKILL.md`) hoặc đối chiếu path ở args theo **đúng** dạng `skill://<name>/SKILL.md`; với `shell_exec`, lấy `skill_name` ở args. Không dựa vào text của lệnh, output hoặc regex quét nội dung thinking. Khi có tên skill hợp lệ và result thành công, `toolStepTitle` thay nhãn mặc định của tool bằng nhãn skill tương ứng; các tool không liên quan vẫn hiển thị như cũ. Đường đọc tài liệu khác, path sai, tên rỗng, hay `shell_exec` không mang `skill_name` không được gắn nhãn skill. `read_skill`/`execute_skill_script` lịch sử cũ có thể giữ cách hiển thị hiện tại; không cần thay đổi hành vi cũ để hoàn tất yêu cầu này.

## Kiểm thử và an toàn worktree

- Unit test phân loại cho hai hành động, tool result success/failure, pending bị finalize, trùng ID khi stream lại, path SKILL.md hợp lệ/không hợp lệ, tool call thiếu args. Chứng minh không đưa command/secret vào nhãn.
- Test cùng một hành động cho live (`applyChunkToSteps`) và lịch sử (`stepsFromHistory`) ra cùng tên/hành động; kết quả không có `success: true` không được gắn nhãn.
- Chạy `frontend-next` typecheck/build và test runner hiện có. Không sửa `frontend-next/package-lock.json` đang có chỉnh sửa không liên quan.
- Trước khi sửa symbol, chạy GitNexus upstream impact và báo blast radius/rủi ro. Trước commit, `detect-changes --scope staged` phải xác nhận chỉ các file tính năng bị ảnh hưởng. Giữ nguyên toàn bộ công việc không liên quan đang dở trong worktree.
