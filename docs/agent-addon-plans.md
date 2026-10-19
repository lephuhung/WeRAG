# Agent chuyên ngành bán kèm gói dịch vụ

- Ngày: 2026-10-19.
- Nhánh: `feat/agent-addons` (worktree `/home/WeRAG-wt/agent-addons`), tách từ `feat/document-assistant` tại `38e230f7`.
- Trạng thái: người dùng đã chốt toàn bộ mục 7 (19/10/2026); giai đoạn A đang làm.
- Liên quan: `docs/domain-pack-standard.md` (bộ kỹ năng), `docs/billing-payment-webhook.md`, mục "Tổ chức áp dụng" trong `docs/tenant-kb-permissions-implementation-plan.md` §8.

## 1. Vì sao đổi

Cách hiện tại (SuperAdmin tick "Tổ chức áp dụng" trên từng agent) đúng về kỹ thuật nhưng người dùng không biết mình có agent nào, vì sao có, và làm sao để có thêm. Quyết định của người dùng (19/10/2026):

1. Có **hai loại "gói"** khác nhau, phải gọi khác tên:
   - **Gói dịch vụ** (Go, Plus, …): bảng `usage_plans`, hạn mức câu hỏi/token. Tổ chức mua gói này.
   - **Bộ kỹ năng chuyên môn** (trước gọi "gói chuyên môn", domain pack): nội dung + công cụ gắn vào agent. Đổi tên hiển thị để không lẫn với gói dịch vụ; định danh trong mã (`domain_packs`, `domainpacks`) giữ nguyên.
2. Luồng của SuperAdmin: **tạo agent chuyên ngành → chọn bộ kỹ năng cho agent → đưa agent vào danh mục bán kèm gói dịch vụ**.
3. Luồng của Tổ chức: **mua gói dịch vụ; trong lúc mua chọn thêm agent chuyên ngành**. Mỗi gói đã gồm **2 agent cơ bản**; agent chuyên ngành chọn thêm thì **trả thêm phí**. Thành viên của Tổ chức dùng được các agent Tổ chức đã mua.

## 2. Thuật ngữ trên giao diện

| Khái niệm | Tên hiển thị (vi) | en | Ghi chú |
| --- | --- | --- | --- |
| `usage_plans` | Gói dịch vụ | Service plan | Go, Plus, … |
| domain pack | Bộ kỹ năng chuyên môn (ngắn: Bộ kỹ năng) | Skill set | đổi từ "Gói chuyên môn" |
| agent được bán thêm | Agent chuyên ngành | Specialist assistant | |
| agent có sẵn trong mọi gói | Agent cơ bản | Included assistant | 2 agent: "Soạn thảo văn bản", "Suy luận thông minh" (tên trong `builtin_agents.yaml`, câu hỏi 7.1) |

## 3. Cách phân phối một agent

Mỗi agent tuỳ chỉnh có đúng một **cách phân phối** (trường mới, SuperAdmin chọn trong trình sửa agent):

| Cách phân phối | Ai thấy | Dùng cho |
| --- | --- | --- |
| **Cơ bản** | mọi Tổ chức (lượt hỏi vẫn qua kiểm gói dịch vụ `usage.enforce`, mặc định bật) | 2 agent cơ bản |
| **Chuyên ngành (bán kèm)** | thành viên của Tổ chức có gói dịch vụ còn hạn **và** đã mua agent này | agent có bộ kỹ năng |
| **Nội bộ** | chỉ các Tổ chức trong "Tổ chức áp dụng" | agent thử, agent riêng một đơn vị không bán |

- Agent có bộ kỹ năng mặc định là **Chuyên ngành**, chưa thuộc gói nào ⇒ chưa ai thấy cho tới khi được bán — không còn nguy cơ "tạo xong là mọi Tổ chức thấy".
- "Tổ chức áp dụng" (đã xây, migration 000165) giữ lại cho cách **Nội bộ**, chuyển vào mục "Nâng cao" của trình sửa agent.
- SuperAdmin luôn thấy và dùng được mọi agent (như hiện tại).
- Built-in (trợ lý văn bản, hỏi đáp…) không đổi cách phân phối; hai agent cơ bản là built-in (câu hỏi 7.1).

## 4. Dữ liệu

- `custom_agents.distribution` (`basic` | `addon` | `internal`), mặc định `basic` cho agent cũ để không đổi hành vi; agent mới có `domain_packs` → `addon`.
- Danh mục bán kèm: bảng `usage_agent_addons` (`agent_id`, `price_vnd` mỗi tháng, `plan_ids` jsonb — rỗng = bán kèm mọi gói, mô tả cho người mua, `is_active`, `sort_order`). Giá là dữ liệu do SuperAdmin sửa lúc chạy, không seed (cùng quy tắc với `usage_plans`).
- Quyền dùng: bảng `usage_subscription_addons` (`subscription_id`, `agent_id`, `starts_at`, `ends_at`, `price_vnd` đã chốt, `order_id`). Gắn vào **thuê bao** (tenant hoặc cá nhân), hết hạn cùng thuê bao trừ khi gia hạn.
- Đơn hàng: `usage_orders` thêm danh sách agent mua kèm (giá chốt từng agent) để báo giá, mã khuyến mãi và xác nhận thanh toán (thủ công hoặc webhook) dùng chung luồng hiện có.

## 5. Luồng

**SuperAdmin**
1. Trình sửa agent, tab Cơ bản: chọn **Bộ kỹ năng chuyên môn** ngay đầu trang (đưa lên từ tab Công cụ; vẫn kiểm `required_tools`).
2. Chọn **Cách phân phối**; nếu Chuyên ngành: nhập giá/tháng, chọn gói dịch vụ được bán kèm (mặc định tất cả), mô tả ngắn cho người mua.
3. Trang Gói dịch vụ (admin billing/usage): mỗi gói hiện "Gồm 2 agent cơ bản" và "Agent chuyên ngành bán kèm" (đọc từ danh mục).
4. Trang Bộ kỹ năng (đổi tên từ trang Gói chuyên môn): tab "Agent đang dùng" hiện agent, cách phân phối, số Tổ chức đã mua.

**Tổ chức (Tenant Admin) — trang "Mua gói"**
1. Chọn gói dịch vụ (Go/Plus…), số tháng, số ghế (gói ghế).
2. Thấy "Đã gồm: <2 agent cơ bản>" và danh sách **Agent chuyên ngành** (tên, mô tả, giá/tháng) để tick thêm.
3. Báo giá = giá gói + Σ giá agent × số tháng (× số ghế với gói ghế) (− khuyến mãi). Đặt hàng → chuyển khoản → SuperAdmin/webhook xác nhận → thuê bao + quyền dùng agent được kích hoạt.
4. Mua thêm agent giữa kỳ: báo giá theo 7.3 (tháng còn lại tính đủ; tháng lẻ ≤ 15 ngày tính 1/2, > 15 ngày tính đủ), × số ghế nếu gói ghế; hết hạn cùng thuê bao.
5. Trang "Gói sử dụng" của Tổ chức liệt kê agent đang có và ngày hết hạn.

**Thành viên**
- Danh sách agent trong ô chat = agent cơ bản + agent chuyên ngành Tổ chức đã mua (+ agent nội bộ được áp dụng). Agent chưa mua không hiện; gọi thẳng bằng id → 404 mã 2104 (cơ chế đã có).
- Khi quyền dùng hết hạn: lượt chat kế tiếp nhận thông báo 2104 như hiện tại; ô chọn agent tự về agent mặc định (đã có).

## 6. Kiểm tra quyền (một chỗ)

`customAgentService` đã là cổng duy nhất cho mọi lượt tra agent (tenant scope). Mở rộng điều kiện hiện tại thành:

```
thấy(agent, caller) =
  SuperAdmin người thật
  ∨ distribution = basic
  ∨ distribution = internal ∧ (applies_to rỗng ∨ tenant ∈ applies_to)
  ∨ distribution = addon ∧ ∃ usage_subscription_addons còn hạn của thuê bao đang hiệu lực
                           (tenant của caller, hoặc thuê bao cá nhân của caller) chứa agent
```

- `usage.enforce` mặc định **bật**; công tắc chỉ để tắt khi kiểm thử. Dù tắt, agent `addon` vẫn chỉ hiện khi đã mua — quyền dùng tính theo dữ liệu thuê bao, không theo công tắc kiểm hạn mức (7.4).
- IM, embed, MCP, API key: cùng điều kiện qua tenant của kênh (đã có khung kiểm từ "Tổ chức áp dụng").

## 7. Quyết định (19/10/2026)

1. **Hai agent cơ bản**: *Soạn thảo văn bản* (`builtin-document-assistant`) và *Suy luận thông minh* (`builtin-smart-reasoning`) — giữ tên hiện có (người dùng chốt 19/10/2026). Các built-in khác giữ tắt.
2. **Giá agent chuyên ngành nhân số ghế** với gói ghế (Go): giá/tháng × số ghế; gói pool: giá/tháng cho cả Tổ chức.
3. **Mua thêm giữa kỳ — chỉ 2 mốc**: mỗi tháng còn lại tính đủ giá; phần tháng lẻ còn lại ≤ 15 ngày tính **1/2**, > 15 ngày tính **đủ**. Hết hạn cùng thuê bao.
4. **Agent chuyên ngành chỉ hiện cho Tổ chức/người đã mua**, kể cả khi `usage.enforce` tắt (một chỗ trong mã để đổi: `addonsVisibleWithoutPurchase`). **`usage.enforce` mặc định bật** (registry + giá trị dự phòng của `usageGuard.Enforced`); tắt chỉ để kiểm thử.
5. **Gói cá nhân cũng mua kèm được** agent chuyên ngành (quyền dùng gắn vào thuê bao cá nhân).
6. Tên hiển thị **"Bộ kỹ năng chuyên môn"** (ngắn: "Bộ kỹ năng") cho domain pack.

## 8. Giai đoạn

| GĐ | Việc |
| --- | --- |
| A | Đổi tên hiển thị "Bộ kỹ năng"; đưa chọn bộ kỹ năng lên đầu tab Cơ bản; trường `distribution` + điều kiện thấy agent; "Tổ chức áp dụng" vào mục Nâng cao |
| B | Danh mục bán kèm (`usage_agent_addons`) + trang admin; hiển thị trên thẻ gói |
| C | Mua kèm trong "Mua gói": báo giá, đơn hàng, khuyến mãi, xác nhận/webhook → `usage_subscription_addons`; mua thêm giữa kỳ |
| D | Trang "Gói sử dụng" của Tổ chức + Word add-in (chỉ hiển thị nếu cần); chuyển agent "Tư vấn doanh nghiệp nhỏ – thuế" sang Chuyên ngành và cho 10003 mua thử |

Mỗi giai đoạn: Opus làm, Fable rà (khi còn hạn mức), migration theo số kế tiếp sau số cao nhất đã commit.

### 8.1 Giai đoạn A — đã làm (19/10/2026, nhánh `feat/agent-addons`, chưa merge, chưa chạy migration trên dev)

**Đã giao**

- `usage.enforce` mặc định **bật** (registry + `usageGuard.Enforced`); guard không có settings service chỉ có trong unit test nên vẫn coi là tắt. Trang quản trị đọc "chưa lưu" là bật.
- Đổi tên hiển thị "Gói chuyên môn" → "Bộ kỹ năng chuyên môn"/"Bộ kỹ năng" (en "Skill sets"/"Skill set"): menu và trang quản trị, trình sửa agent, nhãn yêu cầu văn bản "Bộ kỹ năng: …", mô tả công cụ `compare_tax_methods`, lỗi kiểm tra bộ kỹ năng, tóm tắt swagger, `last_query` của đối chiếu. Định danh, route, trường JSON và dấu footer "Lưu ý về cẩm nang chuyên môn" giữ nguyên.
- Trình sửa agent: chọn bộ kỹ năng ở đầu tab Cơ bản (vẫn chỉ cho suy luận; chế độ hỏi nhanh hiện ghi chú; vẫn kiểm `required_tools`); radio "Cách phân phối" có giải thích; "Tổ chức áp dụng" nằm trong mục "Nâng cao", chỉ hiện với Nội bộ, các cách khác lưu `[]`. Danh sách agent có nhãn "Chuyên ngành"/"Nội bộ"; chi tiết bộ kỹ năng có cột "Cách phân phối".
- Migration **000166** `agent_distribution`: `custom_agents.distribution` (`basic`/`addon`/`internal`, CHECK, mặc định `basic`); hàng tuỳ chỉnh đang có danh sách khác rỗng → `internal` (trên dev: 2 agent eval [10000] và "Tư vấn doanh nghiệp nhỏ – thuế" [10003] — hiển thị không đổi); bảng `usage_subscription_addons` (khoá chính `(subscription_id, agent_id)`, `starts_at`, `ends_at` NULL = theo hạn thuê bao, `price_vnd`, `order_id` → `usage_orders` ON DELETE SET NULL, `granted_by`, `created_at`, `updated_at`; xoá thuê bao xoá theo; chỉ mục `(agent_id, subscription_id)` và `order_id`).
- Điều kiện thấy (§6) nằm trong cổng cũ: `whereAgentVisibleTo` (repository) là **một** điều kiện SQL cho cả danh sách và lấy một agent — EXISTS `usage_subscription_addons` → `usage_subscriptions` theo khoá chính, cả hai còn hiệu lực tại thời điểm gọi, thuê bao `tenant` của Tổ chức gọi **hoặc** thuê bao `user` của người gọi. `customAgentService` truyền `types.AgentViewer` (Tổ chức + người thật; API key/IM/embed/MCP không có người → theo Tổ chức). SuperAdmin người thật vẫn đọc không lọc. Quyết định 7.4 nằm ở **một** hàm: `addonsVisibleWithoutPurchase()` (`service/custom_agent.go`, trả `false`).
- Tạo agent không gửi `distribution`: có danh sách → `internal`, có bộ kỹ năng → `addon`, còn lại → `basic`. Danh sách chỉ nhận với `internal` (khác → 400). Built-in luôn `basic` (gửi khác → 400). Nhân bản giữ cách phân phối.
- API SuperAdmin (nhóm `/system/admin`, chặn API key không phải nền tảng): `GET`/`POST /api/v1/system/admin/usage/subscriptions/:id/addons` (body `agent_id`, `starts_at?`, `ends_at?`, `price_vnd`; cấp lại = thay hạn và giá), `DELETE …/addons/:agent_id`. Chỉ cấp được agent `addon`.
- Kiểm thử: repository (mỗi cách phân phối × thành viên Tổ chức đã mua / Tổ chức khác / người có gói cá nhân / API key / SuperAdmin; hết hạn add-on, hết hạn và huỷ thuê bao, thu hồi; cùng ma trận chạy trên PostgreSQL 16 thật qua `WEKNORA_REPO_TEST_POSTGRES_DSN`), service (mặc định khi tạo, đổi cách phân phối, built-in, nhân bản, cấp/thu hồi), router (chỉ SuperAdmin), handler + lượt chat (add-on hết hạn → 2104), migration lên–xuống–lên trên PostgreSQL 16; frontend `tsc` + node test.

**Khác với thiết kế / cần biết**

1. "Cơ bản" = mọi Tổ chức (không thêm điều kiện "có gói còn hạn" vào việc thấy agent); lượt hỏi vẫn qua kiểm gói `usage.enforce`, nay mặc định bật.
2. Hàng agent thuộc chính Tổ chức luôn thấy, bất kể cách phân phối (như "Tổ chức áp dụng" trước đây).
3. Gói ghế: add-on của thuê bao Tổ chức hiện cho mọi thành viên; ghế vẫn do kiểm gói của lượt hỏi chặn.
4. Thuê bao Tổ chức và thuê bao cá nhân được xét **cả hai** (usage guard chỉ lấy thuê bao Tổ chức trước rồi mới tới cá nhân).
5. Đổi một agent khỏi "Nội bộ" mà không gửi danh sách thì danh sách cũ bị xoá.
6. Đường xoá: `DELETE …/addons/:agent_id` (theo mẫu ghế), thêm `GET` để xem.
7. Gắn agent vào kênh IM/embed/MCP lúc cấu hình (SuperAdmin đọc không lọc) chỉ kiểm danh sách "Nội bộ"; agent Chuyên ngành mà Tổ chức của kênh chưa mua vẫn gắn được, kênh trả lời "trợ lý không áp dụng" ở tin nhắn đầu. Có thể chặn lúc gắn ở giai đoạn sau.
8. Đổi gói (`CreateSubscription` huỷ thuê bao cũ) không chuyển add-on sang thuê bao mới — giai đoạn C phải chép.
9. Thông báo 2104 giữ nguyên câu "không tồn tại hoặc không áp dụng cho Tổ chức của bạn" (frontend và eval dựa vào); docs/error-codes.md ghi thêm trường hợp chưa mua/hết hạn.

### 8.2 Giai đoạn B — đã làm (19/10/2026, cùng nhánh, commit riêng sau giai đoạn A; chưa merge, chưa chạy migration trên dev)

**Đã giao**

- Migration **000167** `usage_agent_addons`: khoá chính `agent_id` (một dòng mỗi agent), `price_vnd` ≥ 0 mỗi tháng (× số ghế với gói ghế), `plan_ids` jsonb (`[]` = bán kèm mọi gói), `description` cho người mua, `is_active`, `sort_order`, `created_at`/`updated_at`; chỉ mục phần các dòng đang bán. Dữ liệu lúc chạy, không seed.
- API SuperAdmin (cùng nhóm `/system/admin` với gói): `GET /api/v1/system/admin/usage/agent-addons` (mặc định gồm dòng tắt; `?include_inactive=false` để bỏ), `POST`, `PUT …/:agent_id` (agent lấy từ đường dẫn), `DELETE …/:agent_id`. Kiểm: giá ≥ 0, gói phải tồn tại, agent phải là Chuyên ngành (400), chưa có dòng (409). Danh sách trả kèm tên agent, `agent_available` (còn là agent Chuyên ngành), `holders` (số thuê bao còn hiệu lực đang giữ add-on) và `included_agents`.
- **Agent bị xoá hoặc đổi khỏi Chuyên ngành → dòng danh mục bị tắt** (không chặn việc đổi/xoá agent): `customAgentService` gọi `DeactivateUnsellableAgentAddon` sau mỗi lần sửa/xoá agent; đổi lại thành Chuyên ngành **không** tự bật lại — SuperAdmin bật "Đang bán". Ngoài ra, báo giá cho người mua luôn bỏ qua dòng mà agent không còn là Chuyên ngành, nên không bán nhầm dù bước tắt có lỗi.
- `GET /billing/plans` (thành viên đã đăng nhập): mỗi gói giữ nguyên các trường cũ ở cấp ngoài cùng, thêm `included_agents` (2 agent cơ bản, tên theo `builtin_agents.yaml` theo ngôn ngữ người gọi) và `addons` (tên, mô tả, `price_vnd`, `per_seat` = gói theo ghế) — chỉ các dòng đang bán, agent còn là Chuyên ngành, bán kèm gói đó. Chưa có mua (giai đoạn C).
- Trình sửa agent, Chuyên ngành: mục "Bán kèm gói dịch vụ" (giá/tháng VNĐ có xem trước "49.000 VNĐ", chọn gói — không chọn = mọi gói, mô tả cho người mua, "Đang bán"), lưu qua API danh mục sau khi lưu agent; chỉ tạo dòng mới khi SuperAdmin đã nhập vào mục này. Trang Gói sử dụng: mỗi gói hiện "Gồm 2 agent cơ bản: …" và "Agent chuyên ngành bán kèm: tên (giá/tháng × số ghế)". Trang Bộ kỹ năng, tab "Agent đang dùng": cách phân phối, giá/trạng thái trong danh mục, số thuê bao đang có.
- Kiểm thử: repository (CRUD, tên/khả dụng/số thuê bao, tắt khi đổi/xoá agent; chạy cả trên PostgreSQL 16), service (kiểm hợp lệ, 409/404/400, báo giá theo gói, `per_seat`, gói lọc, agent đổi phân phối không còn được chào, JSON giữ trường gói ở ngoài), đồng bộ danh mục khi sửa/xoá agent qua `customAgentService`, router (danh mục chỉ SuperAdmin; `/billing/plans` cho thành viên với payload mới), trang chi tiết bộ kỹ năng; frontend `tsc` + node test; migration lên–xuống–lên trên PostgreSQL 16.

**Khác với thiết kế / cần biết**

1. Tên 2 agent cơ bản lấy từ `builtin_agents.yaml` ("Soạn thảo văn bản", "Suy luận thông minh"); người dùng chốt giữ tên này, §2/§7.1 đã sửa theo. Danh sách 2 agent nằm ở `types.UsageIncludedAgentIDs`.
2. Agent cơ bản tuỳ chỉnh (cách phân phối Cơ bản, không phải built-in) không được liệt kê trên thẻ gói.
3. Xoá dòng danh mục không thu hồi quyền dùng đã bán (`usage_subscription_addons` giữ nguyên tới khi hết hạn).
4. Tên agent trong danh mục lấy từ dòng agent của hồ sơ cấu hình (Tổ chức ghi đè tên riêng không ảnh hưởng).

### 8.3 Công tắc bật/tắt agent và giai đoạn C — đã làm (19/10/2026, cùng nhánh, commit riêng; chưa merge, chưa chạy migration trên dev)

**Công tắc bật/tắt agent (yêu cầu mới, làm trước giai đoạn C)**

- Trước đây chỉ có `enabled` trong `config/builtin_agents.yaml` (18/10/2026), SuperAdmin không thấy/không đổi được. Nay: cột `custom_agents.enabled` (migration **000168**, NULL = mặc định: giá trị YAML với built-in, bật với agent tuỳ chỉnh), `CustomAgent.EffectiveEnabled()`, trường API `enabled` (giá trị hiệu lực).
- Với built-in, công tắc nằm trên dòng của agent trong hồ sơ cấu hình (10009) — tạo dòng ở lần bật/tắt đầu, như mọi tuỳ chỉnh built-in; dòng riêng cũ của một Tổ chức nhận công tắc của hồ sơ.
- API: `PUT /api/v1/agents/:id/enabled` `{"enabled": bool}` (SystemAdmin + ConfigProfile như các route sửa agent; service kiểm lại cờ SuperAdmin). `GET /agents?include_disabled=true` chỉ có tác dụng với SuperAdmin (trang quản trị agent); mọi ô chọn agent, kể cả của SuperAdmin, bỏ agent đang tắt. Chọn agent cho MCP endpoint đọc giá trị hiệu lực.
- Built-in đang tắt: không có trong danh sách chọn nhưng vẫn mở được theo id (như cờ YAML). Agent tuỳ chỉnh đang tắt: điều kiện SQL thấy agent loại bỏ (`COALESCE(enabled, true)`), nên ẩn với mọi người trừ SuperAdmin, lượt chat kế tiếp nhận 2104.
- Giao diện: công tắc trên thẻ agent (trang quản trị agent; agent đang tắt hiện mờ kèm ghi chú) và ô "Đang bật" ở đầu tab Cơ bản của trình sửa. Tắt một trong 2 agent cơ bản phải xác nhận ("mọi gói dịch vụ đều gồm agent này").

**Giai đoạn C — mua agent chuyên ngành**

- Migration **000169**: `usage_orders.kind` (`plan`|`addon`), `target_subscription_id`, `addons` jsonb (dòng add-on, giá chốt lúc đặt).
- Báo giá/đơn (Tổ chức và cá nhân, 7.5): `kind=plan` + `addon_agent_ids` = giá gói + Σ giá add-on × số tháng (× số ghế với gói ghế, 7.2); chỉ nhận add-on đang bán kèm gói đó; mã khuyến mãi giảm trên toàn bộ tạm tính (gói + add-on).
- Mua thêm giữa kỳ `kind=addon` (7.3, `types.PriceAddonRemainder`): từ bây giờ (hoặc từ ngày hết của quyền đang có) tới hết thuê bao: mỗi tháng còn lại tính đủ; phần lẻ ≤ 15 ngày tính 1/2 (làm tròn lên đồng), > 15 ngày tính đủ; ngày bắt đầu dở tính là một ngày; × số ghế. Từ chối khi đã có quyền tới hết thuê bao, khi không có thuê bao hiện hành, hoặc thuê bao không có ngày hết (SuperAdmin cấp tay).
- Xác nhận (nút SuperAdmin hoặc webhook thanh toán, cùng transaction):
  - Thuê bao mới: add-on của đơn hết hạn cùng thuê bao.
  - **Đổi gói**: add-on còn hiệu lực của thuê bao cũ được chuyển sang thuê bao mới **tới ngày đã trả tiền** (ngày hết của quyền, hoặc của thuê bao cũ) — kể cả khi gói mới không bán add-on đó (quyết định: giữ tới hết hạn đã trả, không hoàn tiền, không cắt ngang). Add-on mua lại trong đơn đổi gói theo hạn thuê bao mới.
  - **Gia hạn cùng gói**: add-on có trong đơn gia hạn theo hạn mới (giữ ngày bắt đầu); add-on không có trong đơn giữ ngày hết đã trả (quyền không có ngày hết được ghim vào hạn cũ, không được gia hạn miễn phí).
  - Đơn `addon`: quyền trên thuê bao đích tới đúng ngày đã báo giá (`paid_until`), kể cả khi thuê bao được gia hạn trước lúc trả tiền; nếu thuê bao đích không còn là thuê bao hiện hành → 409 (webhook ghi `confirm_failed` như hiện có).
- `GET /api/v1/billing/subscription?scope=tenant|user` (người mua; Tổ chức cần Tenant Admin): thuê bao hiện hành, add-on đang có, add-on còn mua được. Trang "Gói sử dụng" của Tổ chức (`GET /tenants/:id/usage`) liệt kê add-on đang có và ngày hết.
- Giao diện "Mua gói": thẻ gói hiện "Gồm: Soạn thảo văn bản, Suy luận thông minh" và số agent mua thêm được; chọn gói → tick agent chuyên ngành (giá/tháng, /ghế); báo giá liệt kê từng dòng add-on và cách tính; mục "Agent chuyên ngành của gói đang dùng" để mua thêm giữa kỳ. Đơn hàng (người mua và SuperAdmin) hiện dòng add-on.
- Kiểm thử: bảng tính 2 mốc (0/7/15/16 ngày, ngày dở, × ghế, làm tròn nửa giá), báo giá gói + add-on (ghế, pool, cá nhân, không bán, khuyến mãi), xác nhận (quyền + `order_id`), gia hạn (theo hạn mới / giữ hạn cũ / ghim quyền không ngày hết), đổi gói (chuyển tới hạn đã trả), giữa kỳ (giá, đã có, chưa có thuê bao, thuê bao đổi trước khi trả), webhook, `ScopeAddons`, trang Tổ chức; router `/billing/subscription`; đơn có dòng add-on trên PostgreSQL 16 thật; migration 165→169 lên/xuống trên PostgreSQL 16; frontend `tsc` + node test.

**Khác với thiết kế / cần biết**

1. Gia hạn gói ghế có thêm ghế: phần ghế thêm của **add-on** cho thời gian còn lại của kỳ hiện tại không tính (chỉ phần gói có tính theo ngày như trước); add-on tính × số ghế mới × số tháng gia hạn.
2. Đơn `addon` dùng `plan_id` của thuê bao đích (cột bắt buộc), `months` = 0.
3. Word add-in: không đổi (panel gói của add-in đọc `/me/usage`, không có danh sách add-on; không đơn giản để thêm trong repo này).
4. Huỷ/hết hạn đơn chưa trả không đụng tới quyền dùng.

## 9. Rà soát

- 2026-10-19: Fable hết hạn mức (HTTP 429) khi rà nhánh này; theo quyết định của người dùng, một agent Opus độc lập rà trước (bản rà: scratchpad `review-agent-addons-opus.md`, kết quả ghi bên dưới khi xong). **Còn nợ: Fable rà lại toàn bộ `38e230f7..HEAD` (giai đoạn A, B, C, nút bật/tắt) khi có hạn mức**, kể cả khi đã merge.
