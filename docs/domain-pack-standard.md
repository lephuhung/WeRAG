# Gói chuyên môn (domain pack) — chuẩn tích hợp skill cho agent chuyên ngành

- Ngày: 2026-10-18.
- Nhánh: `feat/domain-packs` (worktree `/home/WeRAG-wt/domain-packs`), tách từ `feat/document-assistant` tại `2b76e58a`.
- Trạng thái: chuẩn đã chốt hướng; giai đoạn 1 đang làm. Mỗi giai đoạn do Opus làm, Fable rà trước khi sang giai đoạn sau.
- Gói thí điểm: `opc-vn-business-law` (Diginno, CC BY 4.0, bản fork https://github.com/lephuhung/opc-vn-business-law).

## 1. Mục tiêu

Một **nhóm công việc** (ví dụ: tư vấn doanh nghiệp nhỏ – thuế – kế toán) được phục vụ bằng:

```
Tổ chức  →  agent tuỳ chỉnh của Tổ chức  →  gói chuyên môn (skill đã chuẩn hoá + tool + footer)
                                         →  KB VBPL (căn cứ) + danh mục legal_documents (hiệu lực)
```

Nguyên tắc nền:

1. **Căn cứ pháp lý chỉ đến từ KB VBPL và danh mục `legal_documents`.** Gói không bao giờ là nguồn sự thật về hiệu lực, số liệu thuế suất, ngưỡng.
2. **Hành vi bắt buộc phải nằm trong mã.** Qwen bỏ qua quy tắc chỉ có trong prompt (đã đo nhiều lần), nên mọi "luôn / bắt buộc / không bao giờ", mọi phép tính và mọi câu miễn trừ trách nhiệm là mã, không phải chữ trong SKILL.md.
3. **SuperAdmin duyệt mọi thứ.** Không tự áp dụng phiên bản mới, không tự đổi trạng thái văn bản (cùng quy tắc với KB công khai).

## 2. Hiện trạng mã (đã kiểm tra 2026-10-18)

| Khả năng | Hiện trạng | Dẫn chứng |
| --- | --- | --- |
| Skill built-in (go:embed) | Mọi agent smart-reasoning đều thấy; chỉ có `SKILL.md`, không có `references/`; không lọc được bằng `allowed_skills` | `examples/skills/embed.go`, `internal/agent/skills/builtin_source.go:130-141`, `agent_service.go:708-713` |
| Skill cài theo Tổ chức | Gắn với sandbox config; cài = installer agent (LLM) dựng snapshot image; chỉ SuperAdmin; không có sandbox thì không nạp | `internal/types/tenant_skill.go`, `routes_infra.go:87-99`, `agent_service.go:700-718` |
| `allowed_skills` | Rỗng = cho phép tất cả skill Tổ chức | `skills/manager.go:233-250` |
| Đọc tài nguyên skill | `read_file(skill://…)`; **không** được cổng grounding tính là bằng chứng | `grounding_gate.go:43-49` |
| Cổng grounding | Hit `search_knowledge` từ **bất kỳ** KB nào của agent đều là bằng chứng | `grounding_gate.go:38-49,73-90` |
| Lọc KB theo lĩnh vực | Không có; KB VBPL chia theo loại văn bản | `tools/search_knowledge.go:71-98` |
| Khối chèn bằng mã | Có ở cấp engine (`appendLegalHistory`, `appendWikiCaveat`, `appendSourceEditNote`), chưa có theo agent | `internal/agent/finalize.go:138-140` |
| Agent theo Tổ chức | Khoá `(id, tenant_id)`; không chia sẻ liên Tổ chức; tạo/sửa = SuperAdmin | `types/custom_agent.go:65-95`, `routes_agent.go:20-50` |
| Hàng đợi văn bản thiếu | Có, kích hoạt khi câu hỏi nêu số hiệu; chưa có API nhập hàng loạt | `legal_request_gate.go`, migration `000162` |

Hệ quả: chưa có đường "skill chỉ-đọc, có tài nguyên, chỉ một số agent thấy, không cần sandbox". Đó là việc đầu tiên.

## 3. Định dạng gói

### 3.1 Vị trí và cách phát hành

Gói nằm trong repo tại `domain-packs/<name>/` và được nhúng vào binary (go:embed), giống skill built-in nhưng **opt-in theo agent**.

Lý do chọn nhúng thay vì tải lên lúc chạy:
- Gói luôn phải được viết lại (mục 5) và đi kèm tool Go (lớp d) — tool Go đằng nào cũng cần build lại.
- Review qua git diff + test kiểm định dạng (mục 3.4) thay cho installer agent đọc text của bên thứ ba trong sandbox (bề mặt prompt injection).
- Không cần sandbox, không cần object storage để đọc tài nguyên.

Đường tải lên lúc chạy (dựa trên `tenant_skill_catalog` đã lưu archive) để sau, khi có nhu cầu gói không cần mã.

`domain-packs/embed.go` nhúng **toàn bộ** cây (`//go:embed *`), không liệt kê từng mẫu: go:embed báo lỗi khi một mẫu không khớp tệp nào (lúc chưa có gói), và nhúng hết thì registry kiểm được mọi tệp thực sự ship — tệp lạ làm dừng khởi động thay vì lọt qua. `ATTRIBUTION.md` và `evals/` vì thế cũng nằm trong binary nhưng không bao giờ được đưa cho model. Tệp go:embed bỏ qua (tên bắt đầu bằng `.`/`_`) do test đọc thư mục nguồn bắt.

### 3.2 Cấu trúc

```
domain-packs/<name>/
  SKILL.md          # frontmatter manifest + quy trình (lớp a)
  catalogue.yaml    # số hiệu văn bản mà gói dựa vào (lớp b) — không có cột trạng thái
  references/*.md   # bình luận, cẩm nang (lớp c)
  assets/*.md       # mẫu, checklist (lớp c)
  evals/cases.jsonl # bộ kiểm (lớp e)
  ATTRIBUTION.md    # nguồn gốc, giấy phép, commit gốc, những gì đã sửa
```

Không có `scripts/`, README, PROMO, LICENSE gốc, CHANGELOG của bên thứ ba (gộp vào ATTRIBUTION.md).

Schema `catalogue.yaml` (đọc với `KnownFields(true)`; khoá kiểu `status`/`trang_thai`/`hieu_luc`/`in_force`/`effective` bị từ chối ở mọi cấp):

```yaml
documents:
  - number: 01/2021/NĐ-CP          # bắt buộc, không trùng
    title: Nghị định về đăng ký doanh nghiệp   # tuỳ chọn
    used_for: thủ tục đăng ký      # bắt buộc
```

### 3.3 Manifest (frontmatter `SKILL.md`)

```yaml
---
name: opc-vn-business-law          # kebab-case, không tiền tố "legal-" (dành cho built-in)
description: >-                    # ≤ 500 rune: khi nào dùng / không dùng
  ...
metadata:
  version: 1.0.0                   # semver của bản đã chuẩn hoá
  as_of: 2026-10-17                # mốc dữ liệu của nội dung
  review_by: 2027-04-17            # ≤ 6 tháng sau as_of
  domain: doanh-nghiep-nho         # mã nhóm nghiệp vụ nội bộ
  licence: CC-BY-4.0
  attribution: "Nội dung gốc: Diginno, CC BY 4.0, đã chỉnh sửa cho WeRAG"
  origin: https://github.com/lephuhung/opc-vn-business-law@<commit>
  required_tools: [search_knowledge, compare_tax_methods]
  answer_footer: >-                # do mã chèn cuối câu trả lời, không nằm trong body
    Thông tin tham khảo, không phải tư vấn pháp lý/thuế/kế toán. ...
  draft_documents:                 # dự thảo mà gói nhắc tới — phải luôn gắn nhãn
    - title: Dự thảo Luật Phát triển doanh nghiệp nhỏ và vừa
      version: Lần 2
      date: 2026-08-28
---
```

`required_kbs` không nằm trong manifest (id KB khác nhau giữa các môi trường); KB do SuperAdmin chọn khi tạo agent.

### 3.4 Kiểm định dạng (test Go, chạy trong CI)

Mọi gói trong `domain-packs/` phải qua test:
- frontmatter đủ trường bắt buộc; `description` ≤ 500 rune; `review_by` > `as_of` và ≤ `as_of` + 6 tháng;
- `name` trùng tên thư mục, không trùng skill built-in, không tiền tố `legal-`;
- chỉ có các thư mục/tệp trong 3.2; không có tệp thực thi;
- `required_tools` đều là tool có trong `AvailableToolDefinitions`;
- `catalogue.yaml` đúng schema, không có trường trạng thái;
- `SKILL.md` ≤ 6 KB; không chứa URL (trừ trường `origin`), `shell_exec`, `scripts/`, `$WEKNORA_SKILL_DIR`; mỗi tệp tham khảo ≤ 32 KB; `references/**` và `assets/**` cũng không chứa URL (nguồn ghi ở ATTRIBUTION.md);
- manifest đọc bằng struct riêng với `KnownFields(true)` (sai chính tả trường = lỗi); áp luôn `Skill.Validate` hiện có;
- `required_tools` không chứa tool luôn-bật;
- tệp được duyệt cả trên FS nhúng lẫn thư mục nguồn (go:embed bỏ qua tệp `.`/`_`);
- **lint** (không phải cổng an toàn, review tay vẫn bắt buộc): mẫu nghi prompt injection ("bỏ qua hướng dẫn", "ignore previous", "system prompt"), tên tool không thuộc `required_tools` ∪ tool luôn-bật.

## 4. Quy tắc 4+1 lớp

| Lớp | Được phép | Cấm | Cơ chế |
| --- | --- | --- | --- |
| a) `SKILL.md` | quy trình, câu hỏi khai thác dữ kiện, cấu trúc trả lời, phạm vi/ngoài phạm vi, bản đồ tài nguyên, tên tool WeRAG | "luôn/bắt buộc" về hành vi; bảng trạng thái; thuế suất/ngưỡng; script; hướng dẫn cài; web | review tay + test 3.4 |
| b) `catalogue.yaml` | số hiệu + "dùng cho" | tự khai IN FORCE/REPLACED | lệnh đối chiếu (giai đoạn 2) tra `legal_documents.number_key`; thiếu → `legal_document_requests` (nguồn `pack:<name>`); lệch → báo SuperAdmin |
| c) `references/`, `assets/` | bình luận, so sánh, checklist, mẫu; đầu mỗi tệp ghi "Bình luận tham khảo — không phải căn cứ" | trích nguyên văn điều luật không có số hiệu + ngày; số liệu thay đổi theo văn bản | chỉ đọc qua `read_file(skill://…)`, không phải bằng chứng. **Không** nạp vào KB cho tới khi có cờ KB `evidence_role=reference` (giai đoạn 3) |
| d) mã | tool Go cho mọi phép tính; footer; cảnh báo hết hạn rà soát; nhãn dự thảo | script trong sandbox | tool đăng ký trong `AvailableToolDefinitions`, bật theo agent |
| e) `evals/` | ≥ 20 câu, rubric nhị phân | kỳ vọng tự do | chạy 2 cấu hình (có/không gói) — mục 7 |

## 5. Checklist nhập skill của bên thứ ba

1. Clone vào thư mục riêng ngoài repo, **không chạy** script; ghi URL + commit.
2. Bỏ README/PROMO/MAINTAINING/CHANGELOG/LICENSE gốc, `scripts/`, `.github`.
3. Quét text tìm mệnh lệnh ẩn, URL, yêu cầu gọi tool lạ; ghi kết quả vào ATTRIBUTION.md.
4. Tách bảng trạng thái của skill → `catalogue.yaml` (bỏ cột trạng thái) + `draft_documents`.
5. Viết lại SKILL.md: tool WeRAG (`search_knowledge`, `check_legal_status`, `find_related_legal_documents`, tool tính), bỏ tham chiếu script/cài đặt/web; mọi "luôn/bắt buộc" → lớp d.
6. Port script → tool Go: tham số bắt buộc, không mặc định thuế suất/tỷ lệ; output có khối "Giả định"; test Go.
7. Chạy đối chiếu danh mục; không bật khi còn số hiệu lệch chưa được SuperAdmin xử lý.
8. Chạy eval 2 cấu hình, đạt ngưỡng mục 7.
9. SuperAdmin tạo agent cho Tổ chức: `domain_packs=[name]`, chọn KB VBPL, `allowed_tools ⊇ required_tools`.

## 6. Thời gian chạy

- **Bật theo agent**: trường mới `CustomAgentConfig.DomainPacks []string` (chỉ agent smart-reasoning). Chỉ agent khai tên gói mới thấy gói; trợ lý văn bản và agent khác không thấy. Không cần sandbox, không phụ thuộc `SkillsEnabled`/`SelectedSkills`.
- **Thứ tự nguồn skill**: built-in → gói → skill Tổ chức. Gói được xem là skill chỉ-đọc như built-in (`IsReadOnlySkill` = built-in ∪ gói): luôn qua `isSkillAllowed`, không qua `filterAllowedSkills`, `shell_exec` từ chối. Skill Tổ chức trùng tên gói bị ẩn (log). Tên gói trùng built-in bị test 3.4 chặn.
- **Hiển thị cho model**: tên + mô tả trong system prompt, `read_file(skill://<name>/SKILL.md)`, rồi tài nguyên. `read_file` chỉ phục vụ `SKILL.md`, `references/**`, `assets/**`; `catalogue.yaml`, `evals/`, `ATTRIBUTION.md` chỉ dành cho test và lệnh đối chiếu.
- **Footer**: `appendDomainFooter` gọi sau `appendSourceEditNote` ở **cả hai** đường trả lời: `internal/agent/engine.go` (model dừng tự nhiên) và `internal/agent/finalize.go` (tổng hợp, gồm cả hết vòng lặp). Chunk phải emit trên cùng `answerID` **trước** sự kiện `Done` — handler stream cộng dồn theo id nên footer vừa hiện vừa được lưu; emit sau `Done` thì không vào DB. Footer là khối cuối cùng (sau "Lịch sử sửa đổi"), đúng một lần; bỏ qua khi câu trả lời rỗng và trên các câu cố định do mã ("không tìm thấy căn cứ", fallback, forced final, synthesis fallback, vòng lặp lặp lại). Khối mở bằng dấu mốc cố định `\n\n---\n**Lưu ý về cẩm nang chuyên môn**\n` (`domainpacks.FooterMarker`); khi câu trả lời được phát lại làm lịch sử cho lượt sau, mọi thứ từ dấu mốc trở đi bị cắt (như "Lịch sử sửa đổi"), nếu không Qwen chép footer và câu trả lời có hai footer. Phòng thủ thêm: câu footer nào đã có trong câu trả lời thì không chèn lại.
- **Gói chỉ hiệu lực khi đủ tool**: lúc chạy, gói có `required_tools` không nằm hết trong tool thực sự đăng ký cho lượt đó (allowlist hoặc mặc định, sau khi `registerTools` tước tool không dùng được, ví dụ `search_knowledge` khi không có KB) bị bỏ qua (log), cả trong skill lẫn footer. Tên gói được giải một lần trong `buildAgentConfig` (cắt khoảng trắng, bỏ trùng, chỉ tên có trong build, chỉ agent smart-reasoning).
- **Hết hạn rà soát**: `now > review_by` (theo `types.PromptNow()`) → footer thêm câu "Nội dung cẩm nang có mốc dd/mm/yyyy, đã quá hạn rà soát" và API liệt kê gói trả `overdue`. **Không** tự tắt gói theo đồng hồ (trái nguyên tắc 3); muốn ngắt thì SuperAdmin bỏ gói khỏi agent.
- **Lưu agent**: từ chối tên gói không tồn tại; từ chối khi tool hiệu lực thiếu `required_tools` — tool hiệu lực = `allowed_tools`, rỗng thì `tools.DefaultAllowedTools()`; `search_knowledge` còn cần `kb_selection_mode != "none"` (lúc chạy tool này bị tước nếu không có KB). Tool luôn-bật (`check_legal_status`, `find_related_legal_documents`, `resolve_abbreviation`, `search_document_section`) không được khai trong `required_tools`. Chỉ SuperAdmin sửa agent (đã đúng theo route).
- **API**: `GET /api/v1/agents/domain-packs` trả `name, description, version, as_of, review_by, overdue, required_tools` cho trình sửa agent.
- **Pin `@gói`**: `applyPerRequestSkillScope` chấp nhận tên gói như built-in.

## 7. Ngưỡng eval (hiệu chỉnh sau lần chạy đầu)

| Tiêu chí (nhị phân) | Ngưỡng |
| --- | --- |
| Trạng thái văn bản nêu đúng theo danh mục | ≥ 95 % câu có số hiệu |
| Dự thảo được gắn nhãn "dự thảo" khi nhắc tới | 100 % (regex) |
| Footer có mặt | 100 % (do mã, có test đơn vị) |
| Không bịa số hiệu (số hiệu trong phần Căn cứ ∈ danh mục ∪ chunk đã lấy) | 100 % — đo offline bằng script eval (regex số hiệu, đối chiếu tool results trong SSE) cho tới khi có bộ kiểm runtime ở GĐ 3 |
| Không xấu hơn cấu hình không-gói trên bộ `trap` + `nli` | 0 câu xấu đi |
| Độ trễ | trung vị của 3 lần chạy ≤ +40 % so với không-gói (phương sai một lần đã đo ±30 %) |

## 8. Phân quyền

- Thêm/sửa gói = commit vào repo (review code). Gán gói cho agent, chọn KB, bật/tắt = **SuperAdmin**.
- Tenant Admin quản lý thành viên và KB riêng; không gán gói, không sửa agent.
- Phiên bản mới của gói đi cùng bản build; CHANGELOG trong ATTRIBUTION.md ghi số hiệu thêm/bớt.
- Mỗi Tổ chức một agent riêng (không chia sẻ được). Màn quản trị liệt kê agent theo gói để rà khi nâng phiên bản.

## 9. Lộ trình

| GĐ | Việc | Ghi chú |
| --- | --- | --- |
| 1 | Định dạng gói + nguồn nhúng (`domain-packs/embed.go`, `internal/agent/domainpacks/`) + test 3.4 (gói mẫu trong testdata, không ship); manager coi gói là chỉ-đọc; `DomainPacks` trên agent + nạp lúc chạy + kiểm khi lưu; footer (2 đường) + câu quá hạn; `GET /agents/domain-packs`; chọn gói trong trình sửa agent (frontend-next, en + vi); sinh lại swagger. Thứ tự: nguồn → manager → nạp lúc chạy → footer → cấu hình/API → UI | không migration |
| 2 | Chuẩn hoá `opc-vn-business-law`; tool `compare_tax_methods`; lệnh đối chiếu danh mục → `legal_document_requests`; bộ eval `pack` trong `scripts/legal_eval`; màn quản trị liệt kê agent theo gói (`config->'domain_packs'`) | cần truy cập DB dev để đối chiếu |
| 3 | Cờ KB `evidence_role=reference`; lọc theo lĩnh vực (`legal_document_links` → `legal_documents.field` + bảng mã lĩnh vực); bộ kiểm phần "Căn cứ" + nhãn dự thảo bằng mã | lớn nhất |

**Xung đột với `feat/skills-cleanup`** (worktree chưa commit): sửa cùng `initializeSkillsManager`, `configureSkillsFromAgent`, `types/agent.go`, `builtin_source.go`, `read_file.go`. Với nhánh đó, agent có `BuiltinSkillsDisabled && !SkillsEnabled` nhận manager `nil` — khi hai nhánh gặp nhau, điều kiện phải thành `BuiltinSkillsDisabled && !SkillsEnabled && len(DomainPacks) == 0`, nếu không gói biến mất. Để rebase dễ: phần nối gói nằm trong tệp mới (`agent_service_packs.go`, `internal/agent/domainpacks/`), chỉ chạm `initializeSkillsManager` bằng vài dòng; giữ tên `IsBuiltinSkill` làm alias của `IsReadOnlySkill`.

## 10. Ánh xạ `opc-vn-business-law` (gói thí điểm, giai đoạn 2)

| Tệp gốc | Lớp | Xử lý |
| --- | --- | --- |
| `SKILL.md` | a | Giữ quy trình A–E, hỏi dữ kiện, giọng điệu, phạm vi loại trừ. Bỏ khối disclaimer (→ `answer_footer`), mẫu câu trả lời cứng, mục "Cập nhật skill", tham chiếu script/00. Description ≤ 500 rune, tổng ≤ 6 KB |
| `references/00-legal-status.md` | b | Không giữ. 13 số hiệu → `catalogue.yaml`; 3 dự thảo → `draft_documents`; "ba lưu ý" chuyển vào `02`, `04` dạng bình luận |
| `references/01, 03, 06, 07` | c | Giữ, thêm dòng đầu "Bình luận tham khảo", bỏ link |
| `references/02-draft-sme-law-2026.md` | c | Giữ; mọi trích Điều 20 khoản 8 ghi "dự thảo Lần 2 ngày 28/8/2026" |
| `references/04, 05` | c | Giữ phần giải thích; bỏ bảng thuế suất/ngưỡng — con số lấy từ KB |
| `references/08-sources.md` | — | Bỏ; nguồn vào ATTRIBUTION.md |
| `assets/*` | c | Giữ; điều lệ mẫu thêm "mẫu tham khảo, chưa đối chiếu Luật Doanh nghiệp hiện hành" |
| `scripts/so_sanh_thue.py` | d | → tool `compare_tax_methods(doanh_thu, loi_nhuan, ty_le_doanh_thu, thue_suat)`, 4 tham số bắt buộc, khối "Giả định", test tái tạo self-test |
| `evals/evals.json` | e | 6 câu → `evals/cases.jsonl`, mở rộng ≥ 20 (bẫy dự thảo, thuế suất, trốn nợ cũ, ngoài phạm vi) |
| `DISCLAIMER.md`, `README`, `PROMO`, `MAINTAINING`, `CHANGELOG`, `LICENSE` | — | 1 câu → `answer_footer`; attribution → ATTRIBUTION.md; còn lại bỏ |
