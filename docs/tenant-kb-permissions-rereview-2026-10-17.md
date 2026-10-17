# Review lại phân quyền tenant/KB — 2026-10-17

## Phạm vi và kết luận

Review working tree tại HEAD `4289c616`, gồm nhánh phân quyền đã merge và các thay đổi tiếp theo. Working tree còn thay đổi chưa commit từ công việc khác; phần sửa lần này chỉ chạm các file liên quan tới ba phát hiện bên dưới và giữ nguyên các thay đổi khác.

**Đã sửa ba phát hiện trong review:** file KB hạn chế hiện được kiểm quyền cả qua handle và physical alias; các resolver đọc xét lời mời cá nhân; Guest tới được route ảnh/đồ thị nhưng quyền vẫn được quyết định theo KB. Những thay đổi còn chưa commit để chủ repo review.

## R1 — P1: đường dẫn vật lý bỏ qua quyền KB trên `/files`

Vị trí chính: `internal/router/files.go:203` và `internal/application/service/resource.go:127`.

`newGatedFileServeHandler` chỉ gọi `kbGate.Authorize` khi catalog trả về `resourceResolved=true`. `resourceCatalog.ResolvePath` chỉ resolve chuỗi `resource://`; với đường dẫn vật lý như `local://1/exports/a.png`, nó trả về resource nil. Handler sau đó chỉ kiểm tra tenant của đường dẫn và mở file.

Tái hiện bằng test handler với gate thật và các storage/catalog stub:

1. Member tenant 1, không ACL trên KB `restricted` cùng tenant.
2. File có resource handle và binding tới KB đó.
3. `GET /files?file_path=resource://...` trả 403 như mong muốn.
4. Cùng file, `GET /files?file_path=local://1/exports/a.png` trả **200** và nội dung `restricted-secret`.

Điều kiện: người gọi biết đường dẫn vật lý hợp lệ, ví dụ đã lưu từ trước khi KB bị hạn chế hoặc trước khi bị thu hồi quyền. Đây không chỉ là vấn đề của file legacy chưa đăng ký: đổi cách tham chiếu sang physical path cũng bỏ qua lookup của file đã đăng ký.

Đã thêm `ResolveTenantPath` để tra physical locator theo tenant, chuẩn hóa alias `local://` tương đương, rồi kiểm tra cùng KB binding và `DecideKB` như resource handle. `/files` từ chối raw locator không resolve được khi KB gate đang hoạt động; tài nguyên đã đăng ký nhưng không gắn KB vẫn theo hợp đồng hiện tại cho artifact tenant. Response trên route gated chuyển sang `private, no-store` để cache không giữ nội dung sau thu hồi quyền. Regression test bao phủ handle, physical alias, thu hồi lời mời, phân biệt ảnh dẫn xuất/file gốc và raw path không đăng ký.

Giới hạn tương thích: raw file legacy không có bản ghi resource hiện bị từ chối qua `/files` khi gate hoạt động. Cần chuyển chúng sang resource handle hoặc route có phạm vi session/KB tương ứng.

## R2 — P2: resolver cho query/body không xét lời mời cá nhân

Vị trí chính: `internal/handler/kb_access.go:66`.

`resolveHandlerKBAccessFor` gọi `access.ResolveKB`, chỉ truyền tenant grants; không truyền invite lookup. Middleware trên route KB dùng `ResolveKBWithInvite`, nên cùng người dùng và KB có thể được phép trên route detail nhưng bị từ chối khi resolver xử lý KB ID từ query/body mà chưa có grant upstream.

Tái hiện: lời mời cá nhân hợp lệ trên KB restricted được `ResolveKBWithInvite` chấp nhận; cùng caller và KB được `resolveHandlerKBAccess` trả Permission denied. Test kiểm tra resolver, chưa chạy HTTP end-to-end của mọi endpoint dùng resolver này.

Đã cập nhật cả `resolveHandlerKBAccessFor` và resolver đọc tài liệu để dùng `ResolveKBWithInvite` khi grant service hỗ trợ invite. Quyền Editor và tải file gốc vẫn qua quyết định cũ, không nhận quyền từ lời mời. Test HTTP `/knowledge/batch?kb_id=...` xác nhận lời mời đang hiệu lực cho phép đọc và bị thu hồi thì trả 403; test resolver tài liệu xác nhận tương tự.

## R3 — P2: Guest bị chặn ảnh và đồ thị của KB công khai

Vị trí chính: `internal/middleware/auth.go:287` (`matchTenantlessKBReadShape`).

`isTenantOptionalAPI` vẫn trả false với GET:

- `/api/v1/knowledge-bases/:id/files`
- `/api/v1/knowledge-bases/:id/entity-graph`
- `/api/v1/knowledge-bases/:id/legal-graph`

Guest được đọc KB công khai nhưng các request này bị tenant gate chặn trước khi kiểm tra KB. Test tái hiện xác nhận cả ba path không được admit; chưa kiểm thử trình duyệt. Điều này làm nội dung công khai hiển thị thiếu ảnh/đồ thị.

Đã thêm ba GET shape vào tenantless allowlist; middleware vẫn chuyển request qua KB guard. Test integration cho cả ba route xác nhận KB public và published được đọc, KB private/restricted và anonymous bị từ chối.

## Kết quả kiểm chứng

- `go test ./internal/application/access ./internal/application/service ./internal/handler ./internal/middleware ./internal/router -count=1`: pass sau các thay đổi.
- Các test hồi quy mới chạy riêng trước đó cũng pass ở service, handler, middleware và router.
- Frontend: `kb-access`, `kb-capabilities`, `kb-public`, `tenant-roles`, `auth-recovery`: **70 test pass** bằng Node `--experimental-strip-types --test`.
- R1 test với SQLite resource catalog thật; R2 test route handler với service stub; R3 test auth + role + KB guard. Chưa chạy browser/live API hoặc backend storage thật.

GitNexus đã được rebuild tại HEAD trước khi sửa. Impact upstream: `newGatedFileServeHandler` LOW (9 symbols, 2 direct callers); `matchTenantlessKBReadShape` LOW (9 symbols, 1 direct caller); `resolveHandlerKBAccessFor` HIGH (20 symbols, 6 direct callers, 4 flows); `resolveKnowledgeAndValidateKBAccess` HIGH (13 direct callers, 3 flows). Thay đổi R2 chỉ đưa invite vào nhánh Viewer; các luồng quản trị/tải bản gốc giữ nguyên. Không commit trong lần review này.

## Những phần đã có cải thiện

- Có `DecideKB` dùng chung, Guest không còn mặc định nhận Member, scope restricted và member_default.
- Có route/platform scope cho SuperAdmin không membership và test tương ứng.
- Lời mời kiểm tra owner hiện tại và membership active; chuyển chủ cập nhật và thu hồi shares trong transaction.
- UI có tab quyền truy cập dựa trên kết quả backend.

Đây là ghi nhận phạm vi đã đọc và test đã chạy, không phải chứng nhận mọi endpoint đã an toàn.
