# Phân quyền tài khoản, tenant và KB — tài liệu bàn giao

- Ngày: 2026-10-17.
- Mục đích: giao agent khác review thiết kế, xác minh lại source và triển khai.
- Trạng thái: người dùng đã đồng ý mô hình tổng thể; tài liệu này chưa phải xác nhận code đã đáp ứng mô hình.
- Phạm vi: danh tính, membership, quyền trên KB, chia sẻ, chuyển chủ sở hữu và giao diện quản lý.
- Tài liệu này không thực hiện thay đổi source, migration hoặc dữ liệu triển khai.

## 1. Quyết định của người dùng

Người dùng xác nhận:

> “Đồng ý với ý kiến của bạn, guest không có KB, KB có chế độ hạn chế trong tenant, SuperAdmin có mọi quyền với KB mà không cần membership.”

Các nguyên tắc đã chốt:

1. Tài khoản tồn tại độc lập với tenant. Không bắt buộc gia nhập tenant để đăng nhập, quản lý hồ sơ hoặc bảo mật tài khoản.
2. Có bốn loại người dùng: SuperAdmin, Admin tenant, Member tenant và Guest.
3. SuperAdmin quản trị nền tảng và có mọi quyền với KB, không cần membership trong tenant sở hữu.
4. KB hỗ trợ chế độ hạn chế ngay trong cùng tenant; không mặc nhiên mở mọi KB cho mọi Member.
5. Guest không có KB. Đề xuất trước đó cho phép cấp KB trực tiếp cho Guest được rút lại.
6. Chọn tenant là chọn phạm vi làm việc, không phải cấp quyền hoặc tự động thêm membership.

### Cách hiểu về Guest trong tài liệu

Guest là tài khoản đã đăng nhập nhưng không có membership tenant đang hoạt động; không phải người chưa đăng nhập. SuperAdmin không có membership vẫn là SuperAdmin, không trở thành Guest.

“Guest không có KB” được diễn giải là không sở hữu/tạo KB, không nhận ACL hoặc lời mời riêng vào KB. Tài liệu giữ quyền **đọc KB công khai trong hệ thống** từ đề xuất đã được đồng ý trước đó. Đây là cách diễn giải của người viết, chưa có câu xác nhận riêng về việc Guest có được đọc KB công khai hay không. Agent review cần làm rõ điểm này nếu sản phẩm muốn cấm Guest truy cập mọi KB; không tự mở quyền riêng cho Guest.

## 2. Mô hình danh tính và tenant

### 2.1. Danh tính độc lập với tenant

- Một tài khoản có thể có 0, 1 hoặc nhiều membership.
- Một người có thể là Admin tenant A, Member tenant B.
- Quyền SuperAdmin nằm ở cấp hệ thống; Admin/Member nằm trên từng membership.
- Guest là trạng thái suy ra, không cần lưu thành một role tenant.
- Membership có trạng thái active, invited, suspended hoặc đã bị xóa. Chỉ active cấp quyền.
- Quyền từ membership bị thu hồi phải mất hiệu lực ở request tiếp theo, kể cả khi JWT cũ còn hạn.

Không dùng `users.tenant_id`, tenant trong JWT hoặc tenant đang chọn làm bằng chứng duy nhất rằng tài khoản có quyền. Các trường này có thể là lựa chọn mặc định hoặc ngữ cảnh thao tác, nhưng phải được kiểm tra với danh tính và membership hiện tại.

### 2.2. Ma trận vai trò

| Vai trò | Quyền hệ thống | Quản lý tenant | Quyền KB |
| --- | --- | --- | --- |
| SuperAdmin | Toàn quyền | Tạo/cấu hình/khóa/xóa tenant, chỉ định Admin | Toàn quyền mọi KB, không cần membership |
| Admin tenant | Không có quyền quản trị nền tảng | Quản lý thành viên và tài nguyên tenant được giao | Quản lý mọi KB thuộc tenant đó, kể cả KB hạn chế |
| Member tenant | Hồ sơ và bảo mật cá nhân | Không quản trị tenant | Theo phạm vi/ACL từng KB |
| Guest | Hồ sơ và bảo mật cá nhân | Không có | Không có KB riêng hoặc quyền cấp riêng; quyền đọc công khai theo diễn giải ở mục 1 |

Admin tenant không được cấp SuperAdmin, quản lý tenant khác hoặc thay đổi cấu hình nền tảng. SuperAdmin thực hiện thao tác nhạy cảm phải có audit phù hợp.

### 2.3. Vòng đời tenant

- SuperAdmin tạo tenant, cấu hình, chỉ định Admin ban đầu; không tự gia nhập tenant vừa tạo.
- Admin tenant mời/thêm/xóa thành viên, cấp hoặc thu hồi Admin/Member trong tenant của mình.
- Không cho loại bỏ hoặc hạ quyền Admin cuối cùng bằng thao tác thông thường. Các thao tác vòng đời đặc biệt của SuperAdmin cần có quy tắc rõ ràng.
- Không tự nâng người dùng thành Admin trong middleware xác thực để “tự sửa” tenant thiếu membership. Bootstrap/khôi phục phải là luồng được cấp quyền rõ ràng.
- Khi mất membership cuối cùng, tài khoản thường trở thành Guest; không xóa danh tính hoặc hồ sơ cá nhân.
- Nếu tắt/khóa tenant, không tiếp tục cấp quyền qua JWT cũ hoặc ACL phụ thuộc tenant đó. Agent cần xác minh mô hình trạng thái tenant hiện có trước khi triển khai.
- Tự tạo tenant bởi người dùng thường không thuộc luồng mặc định của thiết kế này. Cần rà soát và vô hiệu hóa hoặc tách rõ chính sách self-service cũ.

### 2.4. SuperAdmin không phụ thuộc tenant

- API quản trị nền tảng hoạt động khi không có tenant đang chọn.
- API hồ sơ, đổi mật khẩu và 2FA không yêu cầu membership.
- Với tài nguyên KB đã tồn tại, backend xác định chủ sở hữu và nơi lưu dữ liệu từ bản ghi KB.
- Với thao tác tạo tài nguyên thuộc một tenant, phải chỉ định tenant đích hợp lệ.
- Không dùng tenant giả, membership tạm hoặc tenant mặc định tùy tiện để né lỗi thiếu tenant.
- UI phải có trạng thái quản trị nền tảng không chọn tenant. Chọn một tenant đích không làm thay đổi danh tính SuperAdmin.

## 3. Mô hình quyền trên KB

### 3.1. Tách các khái niệm

| Khái niệm | Ý nghĩa |
| --- | --- |
| Chủ sở hữu | Nền tảng hoặc một tenant; quyết định trách nhiệm quản trị |
| Người tạo | Thông tin truy vết; không tự tạo quyền vĩnh viễn |
| Phạm vi đọc | Toàn tenant, hạn chế hoặc công khai trong hệ thống |
| ACL/chia sẻ | Cấp quyền cụ thể cho đối tượng cụ thể trên một KB |
| Nơi lưu dữ liệu | Phân vùng thực thi/storage/index; không phải bằng chứng quyền |
| Đăng ký tìm kiếm | Tùy chọn đưa KB đã được phép đọc vào phạm vi tìm kiếm mặc định |

Trong source hiện tại, `owner_tenant_id` là chủ sở hữu; `tenant_id` có thể là phân vùng dữ liệu bất biến. Không gộp hai trường này bằng thay đổi tên hoặc migration sơ sài: cần rà soát storage, index, file và worker.

### 3.2. Phạm vi KB

| Phạm vi | Quyền đọc mặc định |
| --- | --- |
| Toàn tenant | Mọi membership active của tenant sở hữu |
| Hạn chế | Chỉ đối tượng được cấp quyền, cộng quyền quản trị của Admin tenant sở hữu và SuperAdmin |
| Công khai trong hệ thống | Mọi tài khoản đăng nhập; không đồng nghĩa Internet/anonymous |

KB nền tảng do SuperAdmin quản trị. KB thuộc tenant có thể được công bố mà vẫn giữ nguyên chủ sở hữu.

Đề xuất triển khai ban đầu: UI buộc thể hiện phạm vi khi tạo KB; hỗ trợ cá nhân và tenant trước. Nhóm/phòng ban là mở rộng có ích nhưng không được tự suy ra là tính năng đã tồn tại. Nếu chưa có mô hình nhóm phù hợp, không mở rộng dự án sang xây hệ thống nhóm trong cùng đợt sửa.

### 3.3. Quyền tài nguyên

Các quyền này không tạo thêm loại tài khoản ngoài bốn loại đã chốt:

| Quyền trên KB | Hành động |
| --- | --- |
| Xem | Xem nội dung, tìm kiếm, hỏi đáp theo KB |
| Đóng góp | Quyền xem, thêm tài liệu, sửa/xóa tài liệu do mình tải lên |
| Quản lý | Quản lý toàn bộ nội dung, cấu hình và chia sẻ KB |
| Tải bản gốc | Quyền riêng; không tự động bật chỉ vì được xem |

SuperAdmin có tất cả quyền. Admin tenant sở hữu có tất cả quyền trên KB của tenant. Member chỉ được hưởng các quyền được cấp hoặc kế thừa từ chính sách KB.

Đề xuất an toàn: chia sẻ ra ngoài tenant mặc định chỉ đọc, không tải bản gốc và không cấp lại quyền. Cấp quyền đóng góp/quản lý cho bên ngoài là mở rộng cần chính sách riêng; không mặc nhiên mở trong lần triển khai đầu.

Chế độ “toàn tenant” không bắt buộc phải cho mọi Member upload: cần biểu diễn quyền mặc định của tenant một cách rõ ràng. Khi migration, bảo toàn hành vi hợp lệ hiện tại hoặc ghi rõ thay đổi; không âm thầm mở rộng quyền.

### 3.4. Cấp và thu hồi quyền

- Có thể cấp cho toàn tenant nhận hoặc một cá nhân có membership active.
- Cá nhân nhận quyền không cần gia nhập tenant sở hữu KB.
- Không cấp ACL riêng cho Guest.
- ACL cá nhân cần quy định rõ quan hệ với membership. Đề xuất: lưu membership/tenant nhận làm điều kiện cấp quyền và kiểm tra lại khi đọc; mất điều kiện thì quyền không còn hiệu lực. Không tự chuyển quyền sang tenant mới của người đó.
- Một người có thể có nhiều nguồn quyền. Thu hồi một nguồn không đồng nghĩa chặn người đó nếu nguồn khác vẫn hợp lệ.
- UI phải chỉ ra nguồn còn hiệu lực, ví dụ “vẫn được đọc do KB đang công khai”.
- Không đưa explicit deny vào phiên bản đầu nếu chưa có nhu cầu; giảm xung đột giữa deny, ACL và kế thừa.
- Lời mời pending không cấp quyền. Accepted chỉ cấp quyền khi còn hạn, chưa thu hồi và các điều kiện chủ sở hữu/membership vẫn hợp lệ.

### 3.5. Công bố, chuyển chủ và đăng ký tìm kiếm

- **Công bố:** đổi người được đọc, giữ chủ sở hữu; quyền tải gốc cần hiển thị riêng.
- **Chuyển chủ:** SuperAdmin thực hiện; mặc định vô hiệu hóa các chia sẻ/lời mời cũ, chủ mới cấp lại.
- **Chia sẻ:** cấp quyền cụ thể, không đổi chủ hoặc tự thêm membership tenant sở hữu.
- **Đăng ký tìm kiếm:** chỉ thay đổi lựa chọn sử dụng; không cấp quyền. Khi quyền đọc mất hiệu lực, tìm kiếm phải loại KB dù subscription vẫn tồn tại.

Không dùng luồng “tenant A → public → tenant B” làm cách chuyển chủ tiêu chuẩn: nó có thể mở dữ liệu cho tất cả tài khoản trong giai đoạn trung gian. Thiết kế một thao tác chuyển chủ riêng nếu cần chuyển tenant-to-tenant.

## 4. Kết quả rà soát source hiện tại

Các phát hiện dưới đây được ghi nhận ở working tree ngày 2026-10-16/17. Repo có nhiều thay đổi chưa commit từ công việc khác. Agent tiếp nhận phải xác minh lại; không coi số dòng hoặc chỉ mục GitNexus cũ là nguồn chân lý.

### F1 — Cao: quyền chéo tenant qua cờ legacy vượt cấu hình tắt

- `SwitchTenant` bỏ kiểm tra membership khi `CanAccessAllTenants=true`.
- Với JWT mang tenant khác và không có header `X-Tenant-ID`, `resolveTenantRole` vẫn cấp Admin mà không kiểm tra `EnableCrossTenantAccess` hoặc `IsSystemAdmin`.
- Đã tái hiện nhánh resolve target/role bằng test tạm: cờ cross-tenant tắt, tài khoản không phải SuperAdmin vẫn nhận Admin.
- Luồng `CreateTenant` cũng dùng trực tiếp cờ legacy để mở nhánh quản trị catalog.
- Hướng xử lý: thống nhất quyền nền tảng theo SuperAdmin; cờ legacy không tự cấp quyền. Không tự migrate mọi người có cờ legacy thành SuperAdmin vì sẽ mở rộng quyền.

Source: `internal/middleware/auth.go` (`resolveTargetTenant`, `resolveTenantRole`), `internal/middleware/access.go`, `internal/application/service/user.go` (`SwitchTenant`), `internal/handler/tenant.go` (`CreateTenant`).

### F2 — Cao khi tắt RBAC: membership suspended trở thành Admin

- `resolveTenantRole` trả Admin khi không tìm được membership active và RBAC bị tắt.
- Đã tái hiện với membership suspended và tenant vẫn còn Admin khác.
- Guard Admin phía sau không cứu được vì danh tính đã bị gán Admin trước đó.
- RBAC mặc định hiện bật; đây là vấn đề của nhánh opt-out/fail-open, không phải khẳng định mọi deployment đang bị ảnh hưởng.
- Hướng xử lý: không cho cấu hình rollout vô hiệu hóa kiểm tra membership, đình chỉ hoặc thu hồi quyền.

Source: `internal/middleware/auth.go`, `internal/middleware/rbac.go`, `internal/config/config.go`.

### F3 — Trung bình: API nền tảng/tài khoản bị ép có tenant

- SuperAdmin không có membership nhận `409 TENANT_REQUIRED` tại `/api/v1/system/admin/settings`, `/api/v1/system/admin/stats` và `/api/v1/auth/2fa/status`.
- Đã tái hiện tại middleware xác thực.
- `isTenantOptionalAPI` chỉ cho phép một phần các API quản trị chạy không có tenant.
- Hướng xử lý: phân loại API theo phạm vi danh tính/nền tảng/tài nguyên, không chỉ thêm ngoại lệ rời rạc; vẫn giữ guard quyền ở route/service.

Source: `internal/middleware/auth.go`, `internal/router/routes_auth_tenant.go`, `frontend-next/lib/auth.tsx`, `frontend-next/lib/auth-recovery.ts`.

### F4 — Khoảng trống thiết kế: Guest bị chuẩn hóa thành Member

- Role rỗng/không hợp lệ trong `Caller.Normalize` thành Member.
- Hiện quyền Guest bị giới hạn nhờ tenant gate và resource guard khác, không phải nhờ biểu diễn rõ trạng thái không có membership.
- Không kết luận đây tự thân là một exploit; đây là rủi ro khi thêm route hoặc bỏ tenant gate.
- Hướng xử lý: biểu diễn rõ không có vai trò tenant; không suy ra Member chỉ từ thiếu dữ liệu.

Source: `internal/types/caller.go`, `internal/types/tenant_member.go`, `frontend-next/lib/tenant-roles.ts`.

### F5 — Cao: lời mời KB cũ còn quyền sau chuyển chủ nhưng không thu hồi được

- Thay đổi scope/owner chỉ cập nhật KB, không vô hiệu hóa lời mời cũ.
- `HasAcceptedInvite` kiểm tra KB ID, user ID, status và expiry; không đối chiếu chủ hiện tại.
- `Revoke` lại từ chối nếu chủ KB khác `OwnerTenantID` của lời mời.
- Đã tái hiện trạng thái sau chuyển chủ bằng test tạm dùng repository SQLite thật và access/service: người nhận lời mời cũ vẫn đọc được KB riêng của chủ mới; chủ mới thu hồi bị `ErrKBInviteNotFound`.
- Test này seed trạng thái sau chuyển chủ, chưa phải end-to-end qua HTTP và toàn bộ chuỗi chuyển chủ.
- Tenant-wide grant đã có kiểm tra chủ hiện tại trong `ApprovedKBPermission`; lời mời cá nhân chưa đồng nhất.
- Hướng xử lý: vô hiệu hóa chia sẻ cũ khi chuyển chủ, kiểm tra chủ ở read path và bảo đảm quản trị viên hiện tại thu hồi được bản ghi stale. Cần transaction hoặc cơ chế version quyền để không có khoảng hở giữa đổi chủ và vô hiệu hóa ACL.

Source: `internal/application/service/knowledgebase.go` (`persistKBScopeChange`), `internal/application/repository/kb_invitation.go` (`HasAcceptedInvite`), `internal/application/service/kb_invitation.go` (`Revoke`), `internal/application/access/kb_invites.go`, `internal/application/service/kb_access_grant.go` (`ApprovedKBPermission`).

### F6 — Khoảng trống thiết kế: chưa hạn chế KB trong cùng tenant

- `ResolveKB` nhận cùng owner tenant là có quyền tài nguyên; route role guards quyết định tiếp các thao tác ghi.
- Mọi Member cùng tenant được đọc và có luồng upload; không có ACL chọn lọc cùng tenant theo mô hình đã chốt.
- Không được hiểu `KBPermissionAdmin` trong resolver là tự thân cho mọi Member toàn quyền HTTP: vẫn có các guard bổ sung.
- Hướng xử lý: thêm phạm vi hạn chế và ACL; áp dụng cho list/search/chat/files, không chỉ trang chi tiết.

Source: `internal/application/access/knowledgebase.go`, `internal/router/routes_knowledge.go`, `frontend-next/lib/kb-capabilities.ts`.

### F7 — Trung bình: SuperAdmin không nhất quán giữa chia sẻ và sửa KB

- `CanManageKBSharing` cho SuperAdmin quản lý chia sẻ bất kỳ KB.
- `ResolveKBForManage` với KB thuộc tenant vẫn cần caller tenant trùng owner tenant.
- UI capability trên KB foreign vẫn đặt manage/share/invite=false, kể cả SuperAdmin.
- Hướng xử lý: thực thi thống nhất quyết định mới “SuperAdmin có mọi quyền KB không cần membership”; không biến execution tenant thành danh tính mới để đạt điều đó.

Source: `internal/application/access/ownership.go`, `internal/application/access/knowledgebase.go`, `frontend-next/lib/kb-capabilities.ts`.

### F8 — Khó quản lý: scope, sở hữu và quyền sử dụng bị trộn

- `tenant`, `published`, `public` vừa biểu diễn phạm vi đọc vừa ràng buộc owner.
- `public` là KB nền tảng; `published` là KB tenant mở cho mọi tài khoản đăng nhập.
- Public/published cho tải bản gốc, trong khi chia sẻ riêng không cho tải bản gốc.
- Có panel chia sẻ tenant, panel lời mời cá nhân, modal công bố và subscription tìm kiếm.
- Lời mời cá nhân hiện bắt người nhận có membership ở tenant khác; không phục vụ ACL hạn chế trong cùng tenant.
- Hướng xử lý: tách khái niệm ở backend và diễn giải thống nhất trên một màn hình quyền truy cập.

Source: `internal/types/knowledgebase.go`, `internal/application/service/kb_invitation.go`, `internal/application/service/knowledgebase_subscription.go`, `frontend-next/components/knowledge/kb-access-panel.tsx`, `kb-invite-panel.tsx`, `kb-share-modal.tsx` trong cùng thư mục.

## 5. Đề xuất triển khai

### 5.1. Một chính sách quyền dùng chung

Tập trung quyết định quyền KB theo các đầu vào: danh tính, SuperAdmin, membership active, KB owner, phạm vi đọc, ACL còn hiệu lực và hành động yêu cầu.

Thứ tự đề xuất cho human:

1. Xác thực danh tính và trạng thái tài khoản.
2. SuperAdmin được mọi hành động KB, vẫn kiểm tra tài nguyên tồn tại và tính hợp lệ của thao tác.
3. Admin active của tenant sở hữu được quản lý KB.
4. Xét phạm vi công khai/toàn tenant hoặc ACL cá nhân/tenant còn hiệu lực.
5. Xét quyền hành động và quyền tài liệu do chính người đó tải lên.
6. Không có nguồn quyền hợp lệ thì từ chối.

API key, embed, IM và trusted worker là các loại principal riêng. Không biến chúng thành human SuperAdmin; phải giữ capability, KB allowlist và resource binding đang có.

Đề xuất backend trả `effective_permissions` và nguồn quyền cho UI. Frontend dùng để hiển thị; backend vẫn kiểm tra lại mọi request. Tránh để UI tự suy ra quyền chỉ từ `tenant_id` và role.

### 5.2. Bề mặt phải áp dụng cùng chính sách

- Danh sách KB, catalog, chi tiết, số lượng tài liệu và metadata.
- Tìm kiếm/hybrid search, lựa chọn KB cho agent, chat, citations và snippets.
- Preview, tải bản gốc, file proxy, batch API, message references/artifacts liên quan nguồn KB.
- Upload, sửa/xóa tài liệu, chunk, FAQ, tag, Wiki, reparse và clear-content.
- Copy/duplicate/move, đổi phạm vi, chuyển chủ và chia sẻ.
- Worker phải giữ ràng buộc KB/document của công việc đã được chấp nhận. Không mở rộng đợt sửa thành thay đổi toàn bộ cơ chế job nếu không cần; ghi rõ thời điểm quyền được kiểm tra và xử lý job đang chạy khi thu hồi.

### 5.3. Giao diện quản lý

Một mục **Quyền truy cập** trên KB hiển thị:

- Chủ sở hữu, phạm vi đọc, người/tenant được cấp quyền.
- Quyền hiệu lực: xem, đóng góp, quản lý, tải bản gốc.
- Nguồn quyền: SuperAdmin, Admin tenant, toàn tenant, ACL hoặc công khai.
- Trạng thái lời mời, hết hạn, thu hồi; không chỉ hiển thị ID kỹ thuật khi có thể lấy tên/email.
- Cảnh báo rõ khi thu hồi một ACL nhưng đối tượng vẫn có quyền từ nguồn khác.

Tách **Chuyển chủ sở hữu** khỏi **Công bố**. Tách **Đưa vào tìm kiếm mặc định** khỏi **Quyền truy cập**. Không dùng yêu cầu chọn/gia nhập tenant để chặn toàn bộ giao diện của SuperAdmin.

### 5.4. Migration và tương thích

- Lập danh sách role/cờ legacy và cách xử lý, gồm owner/contributor/viewer và `CanAccessAllTenants`.
- Không tự nâng cờ legacy thành SuperAdmin.
- Giữ dữ liệu KB hiện tại; không đồng nhất owner và storage tenant một cách máy móc.
- Mapping `published/public` sang phạm vi đọc và ownership phải tường minh.
- Khi thêm restricted mode, không âm thầm đổi toàn bộ KB hiện có sang private hoặc công khai hơn.
- Rà soát ACL/lời mời có owner stale; migration cần idempotent, có báo cáo và đường khôi phục phù hợp.
- Kiểm tra PostgreSQL và SQLite nếu cả hai vẫn được hỗ trợ; không chỉ sửa một bộ migration.
- Hành vi tải gốc hiện tại cần migration tương thích và thông báo rõ nếu thay đổi.

## 6. Thứ tự công việc đề xuất

1. Review lại tài liệu và source, chốt các chi tiết còn mang tính đề xuất: Guest đọc công khai, quyền mặc định Member và điều kiện membership của ACL cá nhân.
2. Viết regression tests cho F1, F2, F3 và F5; sửa các lỗi đã xác minh.
3. Làm SuperAdmin độc lập tenant và thống nhất quyền KB; giữ ranh giới API key.
4. Thêm phạm vi hạn chế và quyền KB dùng chung; chuẩn bị migration.
5. Đồng bộ list/search/chat/file và các đường mutation/batch/worker liên quan.
6. Đồng bộ UI quản trị tenant và quyền truy cập KB.
7. Chạy ma trận kiểm thử, rà soát phạm vi thay đổi và ghi rõ hạn chế kiểm chứng.

Đây là thứ tự đề xuất, không phải kế hoạch kỹ thuật đã được kiểm chứng chi tiết. Agent triển khai cần điều chỉnh theo dependency thực tế.

## 7. Tiêu chí nghiệm thu

### Danh tính và tenant

- [ ] SuperAdmin không có membership dùng được API nền tảng, hồ sơ và 2FA.
- [ ] SuperAdmin quản lý KB của mọi tenant mà không cần tạo membership hoặc bật cờ legacy.
- [ ] Member/Admin tenant không được dùng API quản trị nền tảng.
- [ ] Admin A không thể quản trị B bằng đổi URL, header, body hoặc tenant claim.
- [ ] Suspended/deleted membership không được nâng quyền khi RBAC tắt hoặc JWT cũ còn hạn.
- [ ] Thiếu role không tự trở thành Member có quyền tenant.
- [ ] Tài khoản mất tenant cuối cùng vẫn đăng nhập được dưới trạng thái Guest.

### Quyền KB

- [ ] Member không có ACL không thấy/đọc/tìm kiếm KB hạn chế, kể cả cùng tenant.
- [ ] Không lộ metadata, snippets, citations, preview hoặc file qua đường khác khi bị chặn KB.
- [ ] Quyền xem không tự cho upload, sửa, chia sẻ hoặc tải bản gốc.
- [ ] Đóng góp chỉ sửa/xóa tài liệu của mình; không reparent tài liệu hoặc sửa tài liệu người khác qua batch/chunk.
- [ ] Admin tenant quản lý KB hạn chế của tenant mình; SuperAdmin quản lý mọi KB.
- [ ] Guest không tạo/sở hữu KB hoặc nhận ACL riêng; quyền công khai đúng quyết định cuối cùng.
- [ ] Chia sẻ một KB không mở KB khác hoặc cấp membership tenant sở hữu.
- [ ] Pending/revoked/expired/stale-owner invitation không cấp quyền.
- [ ] Thu hồi membership cần thiết của ACL cá nhân làm quyền đó mất hiệu lực.
- [ ] Thu hồi một nguồn quyền không xóa nhầm nguồn khác; UI giải thích được quyền còn lại.
- [ ] Chuyển chủ vô hiệu hóa quyền cũ; chủ mới không gặp lời mời còn hiệu lực nhưng không thu hồi được.
- [ ] Công bố không đổi chủ; chuyển chủ không bắt buộc đi qua trạng thái công khai.
- [ ] Đăng ký tìm kiếm không cấp quyền; quyền đã thu hồi không còn dùng được qua subscription.
- [ ] API key không hưởng quyền human/public ngoài policy, capability và KB allowlist của key.
- [ ] Backend và UI cho cùng kết quả trên ma trận vai trò × membership × KB scope × ACL × hành động.

## 8. Bằng chứng kiểm thử trong phiên rà soát

Các lệnh đã chạy thành công trong lượt rà soát KB:

```bash
go test ./internal/application/access ./internal/middleware ./internal/router
go test ./internal/application/service ./internal/application/repository ./internal/handler \
  -run 'Test.*(KBInvite|KBInvitation|KBAccessGrant|TenantGrant|KnowledgeBase.*(Public|Visibility|Scope|Catalog)|Published|Subscription)' -count=1
node --experimental-strip-types --test \
  frontend-next/lib/kb-capabilities.test.ts frontend-next/lib/kb-public.test.ts
```

Frontend: 24 test pass. Các suite/nhóm backend trên pass. Các test này vẫn có thể xác nhận hành vi cũ không khớp thiết kế mới; pass không có nghĩa là đã đáp ứng yêu cầu.

Test tái hiện được tạo ngoài repo qua Go overlay, không commit:

- `/tmp/werag-auth-review-yw8q9mpi/`: F1, F2, F3.
- `/tmp/werag-kb-review-itksrlat/`: F5, repository SQLite thật và service/access, seed trạng thái sau chuyển chủ.

Các đường dẫn `/tmp` chỉ có giá trị tham khảo trong workspace phiên này và có thể mất. Agent triển khai phải đưa regression tests có assertion hành vi đúng vào repo, không phụ thuộc các file tạm hoặc giữ test coi lỗi là kết quả mong muốn.

Trong lượt rà soát trước, chạy toàn bộ `internal/types` gặp lỗi ngoài phân quyền: `TestRenderPromptPlaceholdersCurrentTimeIsDateOnly` so ngày timezone máy với `PromptNow()` mặc định Việt Nam. Các test quyền được chọn riêng pass. Không tuyên bố toàn bộ test repo đều pass.

Chưa kiểm thử end-to-end trên deployment thật hoặc tất cả storage/vector backend.

## 9. Hướng dẫn cho agent tiếp nhận

1. Đọc `AGENTS.md` và hướng dẫn thư mục liên quan. Working tree có nhiều thay đổi không thuộc task này; không ghi đè hoặc revert chúng.
2. Dùng GitNexus query/context cho điều hướng symbol; xác minh độ mới của chỉ mục và đối chiếu source hiện tại.
3. Trước khi sửa function/class/method, chạy impact upstream và báo blast radius; cảnh báo HIGH/CRITICAL theo quy định repo.
4. Review lại phát hiện trước khi sửa; phân biệt lỗi đã tái hiện, khoảng trống thiết kế và đề xuất triển khai.
5. Viết test theo hành vi mới, không chỉ sửa test để khớp implementation.
6. Trước commit phải chạy `detect_changes()` và kiểm tra chỉ thay đổi phạm vi dự kiến.
7. Bàn giao kèm migration, kết quả kiểm thử, thay đổi hành vi và các vấn đề còn mở. Không coi tài liệu này là bằng chứng triển khai đã hoàn tất.
