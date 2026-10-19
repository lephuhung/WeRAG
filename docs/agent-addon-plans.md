# Agent chuyên ngành bán kèm gói dịch vụ

- Ngày: 2026-10-19.
- Nhánh: `feat/agent-addons` (worktree `/home/WeRAG-wt/agent-addons`), tách từ `feat/document-assistant` tại `38e230f7`.
- Trạng thái: người dùng đã chốt mục 7 (19/10/2026, trừ 7.4 đang dùng đề xuất); bắt đầu giai đoạn A.
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
| agent có sẵn trong mọi gói | Agent cơ bản | Included assistant | 2 agent, xem câu hỏi 7.1 |

## 3. Cách phân phối một agent

Mỗi agent tuỳ chỉnh có đúng một **cách phân phối** (trường mới, SuperAdmin chọn trong trình sửa agent):

| Cách phân phối | Ai thấy | Dùng cho |
| --- | --- | --- |
| **Cơ bản** | mọi Tổ chức có gói dịch vụ còn hạn (và mọi người khi chưa bật kiểm hạn mức) | 2 agent cơ bản |
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

- Khi `usage.enforce` tắt (dev, trước khi bật thu phí): agent `addon` vẫn chỉ hiện khi đã mua — quyền dùng tính theo dữ liệu thuê bao, không theo công tắc kiểm hạn mức (7.4).
- IM, embed, MCP, API key: cùng điều kiện qua tenant của kênh (đã có khung kiểm từ "Tổ chức áp dụng").

## 7. Quyết định (19/10/2026)

1. **Hai agent cơ bản**: *Trợ lý văn bản* (`builtin-document-assistant`) và *Hỏi đáp pháp luật* (`builtin-smart-reasoning`). Các built-in khác giữ tắt.
2. **Giá agent chuyên ngành nhân số ghế** với gói ghế (Go): giá/tháng × số ghế; gói pool: giá/tháng cho cả Tổ chức.
3. **Mua thêm giữa kỳ — chỉ 2 mốc**: mỗi tháng còn lại tính đủ giá; phần tháng lẻ còn lại ≤ 15 ngày tính **1/2**, > 15 ngày tính **đủ**. Hết hạn cùng thuê bao.
4. **Khi chưa bật kiểm hạn mức** (`usage.enforce` tắt): agent chuyên ngành vẫn **chỉ hiện cho Tổ chức/người đã mua** (đề xuất, chờ người dùng xác nhận; một chỗ để đổi).
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
