# Phân quyền tài khoản, tenant và KB — kết quả review và phương án thực hiện

- Ngày: 2026-10-17.
- Tài liệu được review: `docs/tenant-kb-permissions-review-and-design.md` (bản bàn giao 2026-10-17).
- Nhánh/working tree: `feat/document-assistant` tại `24c581d5`, có nhiều thay đổi chưa commit của việc khác. Mọi số dòng dưới đây là của working tree hôm nay.
- Trạng thái: review xong, phương án đề xuất; chưa sửa code, chưa tạo migration.

## 1. Kết luận review

Tám phát hiện trong tài liệu bàn giao đều đúng với source hiện tại. Có thêm ba điểm tài liệu chưa nêu (R1–R3) và hai chỗ tài liệu cần sửa (D1–D2).

### 1.1. Xác minh từng phát hiện

| # | Kết luận | Bằng chứng trong source |
| --- | --- | --- |
| F1 | **Đúng.** Cờ `CanAccessAllTenants` cấp Admin mà không cần `EnableCrossTenantAccess` hay `IsSystemAdmin` | `internal/middleware/auth.go:1024` (bước 2 của `resolveTenantRole`); `crossTenantSwitch` chỉ là `targetTenantID != user.TenantID` (`auth.go:572`), nên JWT mang tenant lạ đủ kích hoạt, không cần header. `IsTenantAccessible` (`access.go:90`) có kiểm tra cờ config, `resolveTenantRole` thì không. `SwitchTenant` bỏ kiểm tra membership (`service/user.go:1195`). `CreateTenant` (`handler/tenant.go:250`). |
| F2 | **Đúng.** RBAC tắt thì membership suspended/thiếu vẫn thành Admin | `auth.go:1054-1066` (bước 4 fail-open). `RequireRole` khi RBAC tắt chỉ log, không chặn (`rbac.go:59-63`). |
| F3 | **Đúng.** SuperAdmin không membership bị `TENANT_REQUIRED` ở settings/stats/2FA | `isTenantOptionalAPI` (`auth.go:88-160`) chỉ cho `/system/admin/tenants`, `/system/admin/users`, không có `/system/admin/settings`, `/stats`, `/auth/2fa/*`. |
| F4 | **Đúng.** Thiếu role → Member | `types/caller.go:15-20`; `TenantRoleFromContext` cũng mặc định Member (`types/context_helpers.go:181`). |
| F5 | **Đúng.** Lời mời cũ sống sót sau chuyển chủ, chủ mới không thu hồi được | `repository/kb_invitation.go:154-167` không so owner; `service/kb_invitation.go:204` trả `ErrKBInviteNotFound` khi owner lệch. Tenant-wide grant đã so owner (`service/kb_access_grant.go:334-339`), lời mời cá nhân thì chưa. |
| F6 | **Đúng.** Chưa có hạn chế trong cùng tenant | `access/knowledgebase.go:165` cùng owner tenant → `KBPermissionAdmin`; `ListVisibleKnowledgeBases` lọc theo `tenant_id` + visibility, không có ACL (`repository/knowledgebase.go:137`). |
| F7 | **Đúng.** SuperAdmin chia sẻ được nhưng không sửa được KB tenant khác | `CanManageKBSharing` cho SuperAdmin (`ownership.go:78`); `ResolveKBForManage` với KB thuộc tenant chỉ nhận cùng tenant (`knowledgebase.go:316`); UI trả `kind:"invited"`, manage=false bất kể `isSystemAdmin` (`kb-capabilities.ts:128-141`). |
| F8 | **Đúng.** Scope/owner/quyền dùng trộn nhau | `KBVisibility` 3 giá trị (`types/knowledgebase.go:59-82`); `ValidateOwnership` buộc public ⇔ owner=0 (`:823-846`); CHECK `owner_tenant_id <> recipient_tenant_id` cấm mời cùng tenant (migration 000110). |

### 1.2. Phát hiện bổ sung

- **R1 — Cờ legacy vượt mọi role gate.** `RequireRole` cho `CanAccessAllTenants` qua mọi guard Admin (`internal/middleware/rbac.go:52-57`). Đây là nhánh thứ tư của F1, phải sửa cùng lúc, không thì F1 vẫn còn qua đường khác.
- **R2 — Chuyển chủ tenant-sang-tenant hiện bị cấm tường minh.** `SetKnowledgeBaseVisibility` trả `tenant-to-tenant scope transfer is not supported` (`service/knowledgebase.go:641`). Đường duy nhất là tenant → public → tenant, chính là luồng tài liệu yêu cầu không dùng. Vì vậy "chuyển chủ" là tính năng **mới**, không phải sửa lỗi.
- **R3 — Tự nâng Admin trong middleware còn tồn tại.** Bước 3 của `resolveTenantRole` (`auth.go:1030-1050`) tự `AddMember` Admin cho tenant chưa có thành viên. Mục 2.3 tài liệu yêu cầu bỏ; cần luồng bootstrap riêng cho SuperAdmin.

### 1.3. Điểm tài liệu cần sửa

- **D1 — Bỏ yêu cầu kiểm tra SQLite** (mục 5.4). Repo chỉ hỗ trợ Postgres từ 2026-10-11; không viết migration hoặc test SQLite. Test tái hiện F5 của đợt trước dùng repository SQLite chỉ để chạy nhanh; test đưa vào repo nên dùng mock repository hoặc Postgres testcontainer đang có.
- **D2 — Guest đọc KB công khai là hành vi hiện tại, không phải quyền mới.** `ResolveKB` đã cho human không tenant đọc public/published (`access/knowledgebase.go:173-181`, `isTenantlessPublicReadPath`). Đề xuất **giữ nguyên** và chốt luôn trong mục 1 của tài liệu; nếu sản phẩm muốn cấm, đó là thay đổi hành vi cần ghi rõ.

### 1.4. Các điểm mở — đã chốt 2026-10-17

Người dùng chốt:

| Điểm | Quyết định |
| --- | --- |
| Guest đọc KB? | **Chỉ đọc KB công khai**, gồm cả hai loại: KB nền tảng `public` (Luật, Nghị định…) và KB tenant đã `published` (tài liệu của UBND cấp tỉnh công khai cho đơn vị khác dùng). Không ACL, không tạo/sở hữu KB. Xác nhận lại 2026-10-17. |
| Quyền mặc định của Member trên KB "toàn tenant" | **Xem + Đóng góp** (như hiện nay). Admin tenant có thể hạ xuống "chỉ xem" theo từng KB bằng `member_default`. |
| ACL cá nhân phụ thuộc membership | **Đồng ý đề xuất**: lưu `recipient_tenant_id`, kiểm tra membership active ở read path; mất membership → quyền mất; không tự chuyển sang tenant khác. |
| Cờ `can_access_all_tenants` | **Chỉ SuperAdmin được có.** Cấm tài khoản không phải SuperAdmin mang cờ này: migration xoá cờ ở mọi user có `is_system_admin=false`, và code từ chối ghi cờ cho người không phải SuperAdmin. Cờ không còn tự cấp quyền; quyền nền tảng chỉ theo `is_system_admin`. |

## 2. Thiết kế đích (tóm tắt để code theo)

### 2.1. Một hàm quyết định quyền duy nhất

Thêm `access.DecideKB(ctx, caller, kb, action, lookups) (Decision, error)` trong `internal/application/access/`, thay dần `ResolveKB`, `ResolveKBWithInvite`, `ResolveKBForDownload`, `ResolveKBForManage`. `Decision` gồm `Permission` (none/view/contribute/manage), `CanDownloadOriginal`, `Source` (superadmin | tenant_admin | tenant_scope | acl_user | acl_tenant | public | published) và `EffectiveTenantID`. Thứ tự đúng mục 5.1 của tài liệu:

1. API key → giữ nguyên `checkAPIKeyScope` + capability, không bao giờ rơi vào nhánh human.
2. `IsExplicitHumanSuperAdmin` → manage + download, mọi KB.
3. Admin active của `OwnerTenantID` → manage + download.
4. `Visibility`:
   - `public` (owner 0) / `published`: human đăng nhập → view + download (hiện trạng).
   - `tenant`: Member active của owner → quyền mặc định của KB (`member_default`: view hoặc contribute).
   - `restricted` (**mới**): chỉ qua ACL.
5. ACL còn hiệu lực: `kb_invitations` accepted (owner trùng, chưa hết hạn, membership `recipient_tenant_id` còn active) hoặc `kb_access_grants` approved (đã so owner). ACL cấp tối đa `view`, không download.
6. Quyền tài liệu của chính mình (`Knowledge.CreatedBy`) chỉ áp dụng khi đã có `contribute`.
7. Còn lại: `ErrUnauthorized` nếu caller không tenant và không SuperAdmin, ngược lại `ErrForbidden`.

Guard HTTP (`KBAccessRead/Manage/Download…` trong `internal/middleware/kb_access.go`) và `internal/mcpserver/scope.go`, `service/mcp_endpoint.go` gọi chung hàm này.

### 2.2. Dữ liệu

- `knowledge_bases.visibility` thêm giá trị `restricted`; thêm cột `member_default VARCHAR(16) NOT NULL DEFAULT 'contribute'` (chỉ có nghĩa với `tenant`). Không đổi `tenant_id`/`owner_tenant_id`.
- `kb_invitations`: bỏ CHECK `owner_tenant_id <> recipient_tenant_id` để mời cùng tenant; thêm cột `revoked_reason VARCHAR(32)` để ghi `owner_changed`.
- Không tạo bảng ACL mới. Không có explicit deny.
- Hành vi tải bản gốc giữ nguyên: public/published/own tenant/SuperAdmin có; ACL không.

### 2.3. Danh tính

- `resolveTenantRole` trả `(role, ok)` với `role=""` khi không membership active; **không** fail-open, **không** auto-promote. Cờ `enable_rbac` chỉ còn tác dụng ở `RequireRole` để log (giữ cho rollout), không còn ảnh hưởng danh tính.
- `Caller.Role` cho phép rỗng; thêm `Caller.HasTenantRole()`. `Normalize()` không ép thành Member nữa; mọi nơi so `Role.HasPermission(Member)` tự nhiên trả false với rỗng.
- Quyền nền tảng chỉ theo `IsSystemAdmin`. `CanAccessAllTenants` trở thành cờ hiển thị không có hiệu lực cấp quyền; không migrate tự động.
- Phân loại route: thêm bảng `routeScope` (identity | platform | tenant | resource) trong router thay cho danh sách rời trong `isTenantOptionalAPI`; identity + platform không cần tenant.

## 3. Phương án thực hiện theo giai đoạn

Mỗi giai đoạn là một PR riêng, merge được độc lập và không đổi hành vi hợp lệ hiện có trừ chỗ ghi rõ. Trước khi sửa mỗi symbol phải chạy `impact` upstream theo quy định repo; `ResolveKB`, `resolveTenantRole`, `RequireRole` chắc chắn HIGH (guard của toàn bộ route KB), cần báo người dùng trước khi sửa.

### Giai đoạn 0 — Regression tests (0.5 ngày)

Viết test khẳng định hành vi **đúng** (sẽ đỏ cho đến khi sửa):

- `internal/middleware/auth_role_test.go`: (a) `CanAccessAllTenants=true`, RBAC bật, không membership, JWT tenant lạ → `ok=false`; (b) membership suspended + RBAC tắt → `ok=false`; (c) tenant trống, home tenant → không `AddMember`.
- `internal/middleware/rbac_test.go`: `CanAccessAllTenants` không qua `RequireRole(Admin)`.
- `internal/middleware/auth_tenantless_test.go`: SuperAdmin không tenant gọi `/system/admin/settings`, `/system/admin/stats`, `/auth/2fa/status` → không `TENANT_REQUIRED`.
- `internal/application/service/kb_invitation_test.go` + `access/kb_invites_test.go`: sau khi owner đổi, `HasAcceptedInvite` không cấp; chủ mới `Revoke` thành công.
- `internal/application/service/user_test.go`: `SwitchTenant` với cờ legacy nhưng không membership → `ErrMembershipNotFound`.

### Giai đoạn 1 — Sửa F1, F2, F3, F5, R1, R3 (1.5 ngày)

| Việc | File | Thay đổi hành vi |
| --- | --- | --- |
| Bỏ bước 2 và 4, bỏ auto-promote bước 3 của `resolveTenantRole` | `internal/middleware/auth.go` | Người có cờ legacy mất quyền Admin tenant lạ; deployment `enable_rbac=false` không còn fail-open. Ghi vào release note. |
| `RequireRole` không nhận `CanAccessAllTenants` | `internal/middleware/rbac.go` | như trên |
| `IsTenantAccessible` bỏ nhánh cờ, thay bằng `IsSystemAdmin` | `internal/middleware/access.go` | SuperAdmin đổi `X-Tenant-ID` sang tenant bất kỳ (chỉ để chọn phạm vi) |
| `SwitchTenant` chỉ nhận membership active hoặc SuperAdmin | `internal/application/service/user.go` | |
| `CreateTenant` `catalogManager` chỉ theo `IsSystemAdmin`/platform key | `internal/handler/tenant.go` | |
| Bootstrap tenant trống: endpoint SuperAdmin `POST /system/admin/tenants/:id/members` đã có; chỉ cần bỏ self-heal, kiểm tra luồng tạo tenant của SuperAdmin có chỉ định Admin đầu | `internal/handler/tenant.go`, `tenant_member.go` | |
| `HasAcceptedInvite` so `owner_tenant_id` với KB hiện tại; `Revoke` cho Admin của chủ **hiện tại** thu hồi cả bản ghi stale; `persistKBScopeChange` khi owner đổi → `MarkRevokedByKB(kb_id, reason=owner_changed)` cùng transaction | `repository/kb_invitation.go`, `service/kb_invitation.go`, `service/knowledgebase.go` | Lời mời cũ mất hiệu lực sau chuyển chủ (đúng thiết kế) |
| Mở route identity/platform không cần tenant: thêm `routeScope` và dùng trong `isTenantOptionalAPI` | `internal/middleware/auth.go`, `internal/router/routes_auth_tenant.go`, `routes_system*.go` | SuperAdmin không membership dùng được settings/stats/2FA |
| Frontend: `auth.tsx` không coi `TENANT_REQUIRED` là lỗi với SuperAdmin; thêm trạng thái "quản trị nền tảng, chưa chọn đơn vị" | `frontend-next/lib/auth.tsx`, `auth-recovery.ts`, `components/account-menu.tsx` | |

Thêm trong giai đoạn này: migration `000147_restrict_can_access_all_tenants` chạy `UPDATE users SET can_access_all_tenants = false WHERE is_system_admin = false` (idempotent, in số dòng bị đổi qua RAISE NOTICE); `AdminCreateUser`/`AdminUpdateUser` trong `internal/handler/tenant_member.go`, `handler/auth.go` và `service/user.go` từ chối `can_access_all_tenants=true` khi `is_system_admin=false`. Trước khi deploy, đếm user bị ảnh hưởng trên dev DB và báo người dùng.

### Giai đoạn 2 — SuperAdmin độc lập tenant trên KB, Guest tường minh (1 ngày)

- `ResolveKBForManage`, `ResolveKBForDownload`, `RequireKBManage`: thêm nhánh `IsExplicitHumanSuperAdmin` → Admin cho **mọi** KB (F7). `EffectiveTenantID` vẫn là `kb.TenantID`; không đổi caller.
- `Caller.Normalize` không ép Member; `TenantRoleFromContext` trả rỗng khi thiếu; rà `rg 'Normalize\(\)' internal` (khoảng 30 chỗ) để chắc không chỗ nào dựa vào Member mặc định (F4).
- `GET /auth/me` trả `membership_state: superadmin | member | guest` và danh sách membership; `frontend-next/lib/tenant-roles.ts` thêm `"guest"`; `kb-capabilities.ts` dùng `isSystemAdmin` cho mọi `kind` (F7 UI).
- Guest: UI ẩn "Tạo KB", backend `POST /knowledge-bases` yêu cầu membership active hoặc SuperAdmin + tenant đích (đã có guard `TenantAdmin`, chỉ cần test).

### Giai đoạn 3 — Phạm vi hạn chế và chính sách dùng chung (3 ngày)

1. Migration `000148_kb_restricted_scope` (000147 dùng cho cờ legacy ở Giai đoạn 1; 000145–000146 đang trong working tree chưa commit; kiểm lại số khi bắt đầu): `visibility` nhận `restricted`; cột `member_default`; bỏ CHECK cùng tenant trên `kb_invitations`; cột `revoked_reason`. Down migration: xoá cột, trả CHECK (fail nếu đã có bản ghi cùng tenant, ghi rõ).
2. `access.DecideKB` theo mục 2.1 + test bảng (vai trò × membership × scope × ACL × hành động), tối thiểu 60 ca.
3. Chuyển guard HTTP, `mcpserver/scope.go`, `mcp_endpoint.go` sang `DecideKB`; giữ các hàm cũ làm wrapper một release rồi xoá.
4. Repository: `ListVisibleKnowledgeBases` → `ListReadableKnowledgeBases(ctx, caller)` dùng một câu SQL với `LEFT JOIN kb_invitations/kb_access_grants/tenant_members`, trả thêm `access_source`. Áp dụng cho list, catalog, search (`/knowledge/search`, hybrid), chọn KB cho agent/session, citations, preview, file proxy, batch, wiki, subscription (subscription chỉ là lọc thêm trên tập đã đọc được).
5. API `PUT /knowledge-bases/:id/visibility` nhận `restricted`; `POST /knowledge-bases/:id/invitations` cho phép `recipient_tenant_id = owner_tenant_id`.
6. Worker: không đổi cơ chế job; thêm kiểm tra `DecideKB` lúc **nhận** job và ghi vào tài liệu rằng job đang chạy không bị huỷ khi thu hồi (hạn chế đã biết).
7. `GET /knowledge-bases/:id/access` trả `effective_permissions`, `sources[]`, owner, scope, danh sách người/tenant được cấp với tên/email.

### Giai đoạn 4 — Chuyển chủ sở hữu (1 ngày, tính năng mới, R2)

- `POST /knowledge-bases/:id/transfer-owner {target_tenant_id}` chỉ SuperAdmin (`PlatformSuperAdmin()` guard). Trong một transaction: đổi `owner_tenant_id`, giữ `tenant_id`, revoke mọi `kb_invitations` + `kb_access_grants` của KB với `reason=owner_changed`, ghi audit `kb_owner_transferred`. Không đi qua `public`.
- `SetKnowledgeBaseVisibility` giữ cho công bố/rút công bố và nâng lên platform-public; bỏ nhánh `public → tenant` chuyển chủ khỏi đó sau khi endpoint mới có (hoặc giữ nhưng gọi chung hàm revoke).

### Giai đoạn 5 — UI quyền truy cập (2 ngày, `frontend-next/` only)

- Gộp `kb-access-panel.tsx`, `kb-invite-panel.tsx`, `kb-share-modal.tsx` thành một tab **Quyền truy cập** đọc từ `/access`: chủ sở hữu, phạm vi (chọn `tenant | restricted | published`, SuperAdmin thêm `public`), quyền mặc định Member, danh sách người/tenant được cấp + trạng thái lời mời, nguồn quyền còn hiệu lực, cảnh báo "vẫn đọc được do KB đang công khai" khi thu hồi.
- Nút **Chuyển chủ sở hữu** riêng, chỉ SuperAdmin. **Đưa vào tìm kiếm mặc định** tách ra khỏi tab quyền.
- Khi tạo KB: bắt chọn phạm vi (mặc định `tenant`).
- i18n chỉ `en` và `vi`.

### Giai đoạn 6 — Kiểm thử và bàn giao (1 ngày)

- Chạy `go test ./internal/middleware ./internal/application/access ./internal/application/service ./internal/handler ./internal/router` và `node --test` các file `lib/*.test.ts`. Test `TestRenderPromptPlaceholdersCurrentTimeIsDateOnly` trong `internal/types` phụ thuộc timezone máy, ngoài phạm vi; ghi rõ.
- Kiểm thử tay trên dev với 4 tài khoản: SuperAdmin không membership, Admin tenant A, Member tenant A không ACL, Member tenant B được mời; đi qua ma trận mục 7 của tài liệu bàn giao.
- `detect_changes()` trước mỗi commit; viết release note phần "thay đổi hành vi" (mục Giai đoạn 1).

## 4. Ước lượng và thứ tự

| Giai đoạn | Nội dung | Ước lượng | Phụ thuộc |
| --- | --- | --- | --- |
| 0 | Regression tests | 0.5 ngày | — |
| 1 | Sửa F1/F2/F3/F5/R1/R3 | 1.5 ngày | 0 |
| 2 | SuperAdmin độc lập tenant, Guest tường minh | 1 ngày | 1 |
| 3 | Restricted scope + `DecideKB` + list/search | 3 ngày | 2 |
| 4 | Chuyển chủ sở hữu | 1 ngày | 3 |
| 5 | UI quyền truy cập | 2 ngày | 3, 4 |
| 6 | Ma trận kiểm thử, bàn giao | 1 ngày | 5 |

Tổng khoảng 10 ngày công. Giai đoạn 0–2 có thể merge và deploy trước (đóng các lỗ bảo mật đã tái hiện).

## 5. Rủi ro

- **Mất quyền của người dùng cờ legacy** (Giai đoạn 1): theo quyết định, cờ bị xoá ở mọi người không phải SuperAdmin; cần danh sách trước khi deploy để người dùng biết ai bị ảnh hưởng.
- **`Caller.Role` rỗng** (Giai đoạn 2): nhiều chỗ gọi `Normalize()`; sai một chỗ là Guest thành Member hoặc Member bị chặn. Giảm rủi ro bằng test bảng ở `access/` và grep toàn bộ `TenantRoleMember`.
- **Hiệu năng list/search** (Giai đoạn 3): câu SQL join thêm 3 bảng; cần index `(kb_id, recipient_user_id, status)` đã có và `tenant_members (user_id, tenant_id, status)`; đo trên dev với 6 KB VBPL.
- **Working tree bẩn**: không commit file ngoài phạm vi; cân nhắc làm trên worktree riêng từ `feat/document-assistant` để tách khỏi thay đổi đang test của người dùng.

## 7. Trạng thái thực hiện và bàn giao (2026-10-17)

Nhánh `feat/tenant-kb-permissions` (worktree `/home/orca/werag-perms`, gốc `feat/document-assistant` @ `24c581d5`), 21 commit (HEAD sau cùng ghi ở git log), chưa push, chưa merge. Opus 5.5 viết code, Fable 5.1 review từng giai đoạn.

| Giai đoạn | Commit | Trạng thái |
| --- | --- | --- |
| 0 Regression tests | `22f0bbd2` | Xong |
| 1 F1/F2/F3/F5/R1/R3, migration 000147 | `ebe9294e` | Xong, đã review |
| 2 SuperAdmin độc lập tenant, Guest tường minh, UI | `bd132cf6` `7cd34756` `e71a51ed` | Xong, đã review |
| 3a Migration 000148, `DecideKB`, guard, `/access` | `458cce4c` `e4428771` `80717985` | Xong, đã review |
| 3b List/search/chat/file/wiki/batch qua `DecideKB` | `c0c6e757` `1345182c` `3302cc0f` | Xong, đã review |
| 4 Chuyển chủ sở hữu | `a0986934` `dbdd7aae` | Xong, đã review |
| 5 Tab Quyền truy cập (frontend-next) | `53286e8a` `adee9ac8` | Xong code, **chưa test trình duyệt** |
| Sửa sau review độc lập (citation web, chuyển chủ giữ restricted, 400 khi tạo KB sai, tùy chọn public) | `72ecb654` `8b4a25d7` `e22871c7` `6c4c2ae0` | Xong, hai lượt review độc lập kết luận sẵn sàng merge |
| Sửa sau test live (Guest tenant cũ bị 403 ở /auth/me; Guest bị guard Member chặn đọc KB public) | `c251efd4` `89f8c4b1` | Xong, test live lại đạt |
| 6 Kiểm thử API trên dev | — | **Đã merge vào `feat/document-assistant` (merge commits `9ddccbbc`, `43b5519e`, `e83b56ed`) và test live 2026-10-17**; UI chưa test trình duyệt |

Kiểm chứng tự động trên nhánh: `go build ./...` sạch; `go test ./internal/... ./client/...` pass toàn bộ, trừ `TestQueryKnowledgeGraphReadsTheGraphStore` trong `internal/agent/tools` hỏng khi chạy `-count>1` **cả trên nhánh gốc** (lỗi có sẵn, không thuộc dự án này); `go vet` chỉ còn cảnh báo cũ `mcpserver/scope_test.go:213`; frontend-next 261 test pass, `tsc --noEmit` sạch.

### 7.1. Các bước deploy

1. Merge nhánh vào `feat/document-assistant` **sau** khi 000145–000146 đã commit (kiểm lại số migration 000147/000148 không trùng).
2. Trước khi chạy migration, trên DB dev liệt kê tài khoản bị ảnh hưởng:
   ```sql
   select username, is_system_admin, can_access_all_tenants,
          (select count(*) from tenant_members m where m.user_id = u.id and m.status = 'active') as active_memberships
   from users u where can_access_all_tenants or is_system_admin;
   ```
   Ai có cờ nhưng không phải SuperAdmin sẽ mất quyền chéo tenant; gán `is_system_admin` nếu đó là quản trị nền tảng thật.
3. Giai đoạn 1 và 2 phải lên cùng nhau (chỉ Giai đoạn 1 thì SuperAdmin không membership bị 403 trên route tenant).
4. Tenant chưa có thành viên không còn tự nâng người đăng nhập đầu tiên thành Admin; SuperAdmin gán Admin qua `/system/admin/tenants/:id/members`.

### 7.2. Thay đổi hành vi (release note)

- Role tenant chỉ đến từ membership active; `enable_rbac=false` chỉ còn hạ cảnh báo của role gate thành log, không fail-open.
- `can_access_all_tenants` không cấp quyền; migration 000147 xoá cờ ở mọi tài khoản không phải SuperAdmin; API từ chối ghi cờ cho người không phải SuperAdmin.
- SuperAdmin có mọi quyền trên mọi KB, không cần membership; danh sách KB của SuperAdmin là toàn nền tảng, nhưng chat "tất cả KB" và tìm kiếm tài liệu chỉ lấy tenant đang chọn + KB public.
- Member tenant chủ nhận `member_default` (mặc định đóng góp) thay vì Admin ở tầng service.
- Lời mời chỉ có hiệu lực khi do chủ hiện tại cấp và membership người nhận còn active; đổi chủ thu hồi mọi lời mời và grant trong cùng transaction.
- `PUT /visibility` không còn chuyển chủ; dùng `POST /knowledge-bases/:id/transfer-owner` (SuperAdmin). Chuyển chủ không mở rộng phạm vi: `restricted` giữ `restricted` (mọi ACL bị thu hồi, chỉ Admin chủ mới và SuperAdmin đọc cho tới khi chia sẻ lại), `published` và `public` về `tenant`.
- Tạo KB với `member_default` sai hoặc scope `published`/lạ trả 400 thay vì âm thầm đổi.
- Citation web search và tệp đính kèm luôn giữ trong lịch sử chat; chỉ citation gắn KB không đọc được (hoặc tài liệu đã xoá) bị ẩn.
- KB có thể `restricted`; mời cùng tenant được phép; `GET /knowledge-bases/:id/access` trả quyền hiệu lực, nguồn, danh sách người được cấp.
- Agent không ghi vào KB public nền tảng chỉ vì `tenant_id` trùng tenant caller.
- Citations tới KB không còn đọc được (hoặc đã xoá) bị ẩn khi tải lịch sử chat.

### 7.3. Ma trận kiểm thử tay trên dev

Bốn tài khoản: SuperAdmin không membership; Admin tenant A; Member tenant A không ACL; Member tenant B được mời.

- [ ] SuperAdmin không chọn đơn vị: settings, stats, 2FA, hồ sơ dùng được; danh sách KB toàn nền tảng; mở tab Quyền truy cập của KB tenant A và sửa scope.
- [ ] SuperAdmin chuyển chủ KB A → B: lời mời/grant cũ hiển thị "thu hồi do đổi chủ sở hữu", Admin B thấy KB, Admin A không.
- [ ] Admin A đặt KB `restricted`: Member A không ACL không thấy KB trong list, 403/404 ở chi tiết, search, chat, preview, download, wiki, chunk, batch.
- [ ] Admin A mời Member A (cùng tenant) vào KB restricted: Member A đọc, preview, không tải gốc, không upload; thu hồi → mất ngay lượt chat sau.
- [ ] `member_default = view`: Member A hết nút upload và bị 403 khi upload qua API.
- [ ] Member B được mời: đọc được; đình chỉ membership của B → mất quyền; khôi phục → có lại.
- [ ] Guest (xoá membership cuối): đăng nhập được, thấy KB public/published, không có nút tạo KB, `POST /knowledge-bases` bị TENANT_REQUIRED.
- [ ] Người có cờ legacy nhưng không SuperAdmin sau migration: không đổi được sang tenant lạ, `/tenants/all` 403.
- [ ] API key tenant A: vẫn chỉ KB allowlist của mình; không đọc KB published/public của tenant khác.
- [ ] Đưa vào tìm kiếm mặc định không mở quyền; bỏ ACL thì KB biến mất khỏi kết quả dù subscription còn.

### 7.3b. Kết quả test API live trên dev (2026-10-17)

Migration 146 → 148 chạy tự động khi backend khởi động; không tài khoản nào mất cờ legacy (chỉ một SuperAdmin, không ai có cờ mà không phải SuperAdmin). Tài khoản test tạo trong DB dev (giữ lại để test UI): `claude_super` (SuperAdmin, không membership), `claude_admin` (Admin 10003), `claude_member_a` (Member 10003), `claude_member_b` (Member 10004), `claude_guest` (không membership, `tenant_id` 10004). Token mint bằng `scripts/doc_assistant_eval/mint_token.py`.

Đạt:
- SuperAdmin không membership: `/auth/me` = `superadmin`; settings, stats, 2FA 200; list KB toàn nền tảng (`access_source=superadmin`); đọc, chuyển chủ, xoá KB của tenant khác 200.
- KB `tenant` của 10003: Member A 200, Member B 403, Guest 401/403, SuperAdmin 200.
- Admin A đặt `restricted`: Member A 403 ở chi tiết và hybrid-search, KB biến mất khỏi list; Admin A và SuperAdmin vẫn đọc.
- Mời Member A (cùng tenant) và Member B (tenant khác): xem được, `download_original=false`, `access_source=acl_user`; đình chỉ membership B → 403 ngay, khôi phục → 200; thu hồi lời mời A → 403 ngay.
- `member_default=view`: Member A `contribute=false`, upload URL 403.
- Admin A nâng `public` → 403 (chỉ SuperAdmin); Admin A chuyển chủ → 403; SuperAdmin chuyển chủ sang 10004 → 200, `revoked_invitations=1`, `tenant_id` dữ liệu giữ 10003, Admin A và Member A mất quyền.
- Guest: `/auth/me` = `guest`, `tenant_required=true`; catalog public 4 KB; chi tiết và danh sách tài liệu KB public 200; KB tenant 401; list KB, tạo KB, upload → 403 `TENANT_MEMBERSHIP_REQUIRED`.
- `next build` production của frontend-next đã merge: 22 route biên dịch sạch.

### 7.4. Việc còn mở (không chặn merge)

- Guest trên KB `tenant`/`restricted` nhận 401 (bước 7 của `DecideKB`: không tenant = unauthorized) thay vì 403; cần kiểm frontend không coi 401 này là hết phiên.
- Guest chưa tải được ảnh nhúng của KB public: `GET /knowledge-bases/:id/files` (image proxy), `entity-graph`, `legal-graph` không nằm trong `isTenantlessPublicReadPath` → TENANT_REQUIRED; ảnh trong nội dung KB public sẽ vỡ với Guest.

- Endpoint nhận `kb_id` qua query/body (`/knowledge/batch?kb_id=`) chưa tra lời mời, người được mời bị từ chối.
- File chưa đăng ký trong catalog vẫn phục vụ theo tenant của đường dẫn trên `/files`.
- Snippet trong bước tool của agent và trong văn bản trả lời không bị ẩn, chỉ danh sách citation.
- `access_source` của SuperAdmin luôn là `superadmin` nên badge "được chia sẻ" không hiện cho họ.
- `GET /access` chỉ nêu một nguồn chính cộng ACL còn hiệu lực, không liệt kê mọi quy tắc áp dụng.
- `enable_cross_tenant_access` trong config không còn tác dụng; nên xoá khỏi config và tài liệu.
- `ListOwnedKnowledgeBases` không còn caller production.
- Tạo KB với scope `published` ngay từ đầu chưa hỗ trợ (tạo `tenant` rồi công bố).
- Test repository Postgres (readable list, revocation counts) chỉ chạy khi có `WEKNORA_MIGRATION_TEST_POSTGRES_DSN`.
- Member tenant B được mời vào KB `published` của A nhưng chưa subscribe không thấy KB trong danh sách, dù mở trực tiếp được.
- Gợi ý câu hỏi mở đầu của agent ở chế độ "tất cả KB" với SuperAdmin lấy KB của mọi tenant, khác chat và tìm kiếm.
- Danh sách KB vẫn chạy 2 câu đếm cho mỗi KB; với SuperAdmin (toàn nền tảng) tốn theo số KB.
- API key, MCP endpoint và kênh IM vẫn đọc KB `restricted` của chính tenant mình mà không cần ACL (đúng quy tắc đã chốt, cần ghi release note).
- Sau chuyển chủ, dữ liệu KB vẫn ở `tenant_id` cũ: chủ mới dùng model, kho lưu và quota của tenant cũ; tenant cũ không xoá được khi KB còn. Audit chỉ ghi phía tenant mới. Subscription cũ sống lại nếu chủ mới công bố lại.
- Chuyển chủ không khoá giữa đọc và ghi; một thay đổi xen giữa có thể bị mất.
- Trang KB gọi `GET /access` hai lần (kb-detail và tab); cần refactor để dùng chung.
- Thu hồi lời mời của một Admin tenant chủ trên KB `restricted` không hiện cảnh báo dù người đó vẫn đọc được.
- Gửi `visibility` và `member_default` cùng lúc là hai lần ghi không chung transaction (UI hiện không gửi kiểu này).
- Chú thích của `hideUnreadableReferences` trong `message.go` nằm dính trên `isWebSearchReference`.
- Ba commit 3b, bốn commit 4–5 và bốn commit sửa mang Co-Authored-By Opus 5.5 (đúng người viết); các commit trước ghi Fable 5.1.
