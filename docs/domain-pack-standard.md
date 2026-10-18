# Gói chuyên môn (domain pack) — chuẩn tích hợp skill cho agent chuyên ngành

- Ngày: 2026-10-18.
- Nhánh: `feat/domain-packs` (worktree `/home/WeRAG-wt/domain-packs`), tách từ `feat/document-assistant` tại `2b76e58a`.
- Trạng thái: chuẩn đã chốt hướng; giai đoạn 1 xong; giai đoạn 2 đã làm trên nhánh (18/10/2026), chờ Fable rà, đối chiếu và eval trên dev. Mỗi giai đoạn do Opus làm, Fable rà trước khi sang giai đoạn sau.
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
- Review qua git diff + test kiểm định dạng (mục 3.4) thay cho installer agent (LLM) tự đọc và cài skill trong sandbox — SuperAdmin duyệt trên diff, không phụ thuộc một bước cài do model điều khiển.
- Không cần sandbox, không cần object storage để đọc tài nguyên.

Đường tải lên lúc chạy (dựa trên `tenant_skill_catalog` đã lưu archive) để sau, khi có nhu cầu gói không cần mã.

`domain-packs/embed.go` nhúng **toàn bộ** cây (`//go:embed *`), không liệt kê từng mẫu: go:embed báo lỗi khi một mẫu không khớp tệp nào (lúc chưa có gói), và nhúng hết thì registry kiểm được mọi tệp thực sự ship — tệp lạ làm dừng khởi động thay vì lọt qua. `ATTRIBUTION.md` và `evals/` vì thế cũng nằm trong binary nhưng không bao giờ được đưa cho model. Tệp go:embed bỏ qua (tên bắt đầu bằng `.`/`_`) do test đọc thư mục nguồn bắt.

### 3.2 Cấu trúc

```
domain-packs/<name>/
  SKILL.md          # frontmatter manifest + quy trình (lớp a)
  catalogue.yaml    # "Văn bản trích dẫn": số hiệu + điều trích (lớp b) — không có cột trạng thái
  formulas.yaml     # "Công thức": công thức cố định của tool Go (lớp d) — tuỳ chọn, xem 3.5
  references/*.md   # bình luận, cẩm nang (lớp c)
  assets/*.md       # mẫu, checklist (lớp c)
  evals/cases.jsonl # bộ kiểm (lớp e)
  ATTRIBUTION.md    # nguồn gốc, giấy phép, commit gốc, những gì đã sửa
```

Không có `scripts/`, README, PROMO, LICENSE gốc, CHANGELOG của bên thứ ba (gộp vào ATTRIBUTION.md).

Schema `catalogue.yaml` (đọc với `KnownFields(true)`; khoá kiểu `status`/`trang_thai`/`hieu_luc`/`in_force`/`effective` bị từ chối ở mọi cấp):

```yaml
documents:
  - number: 168/2025/NĐ-CP         # bắt buộc, không trùng
    title: Nghị định về đăng ký doanh nghiệp   # tuỳ chọn
    used_for: thủ tục đăng ký      # bắt buộc
    articles: ["Điều 21"]          # tuỳ chọn: các điều gói trích
```

`catalogue.yaml` và `formulas.yaml` là **hai nơi duy nhất** SuperAdmin cập nhật văn bản và công thức của gói; trang quản trị "Gói chuyên môn" (§8) hiển thị chúng cùng kết quả tra danh mục. Gói trích số hiệu ở tệp nào (`cited_in`) **không viết tay**: registry tính lúc nạp bằng cách quét `SKILL.md`, `references/**`, `assets/**` tìm số hiệu đầy đủ (số/năm/ký hiệu), so theo khoá danh mục (`CatalogNumberKey`).

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
      date: 2026-08-28              # bỏ khi version là "chưa rõ lần dự thảo"
      aliases: [Luật Phát triển DNNVV, Điều 20 khoản 8]   # tuỳ chọn; chỉ cụm từ chỉ đúng dự thảo này (không dùng "doanh nghiệp một người": cũng chỉ công ty TNHH MTV theo luật hiện hành)
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
- `draft_documents`: `title` + `version` bắt buộc; `date` bắt buộc trừ khi `version` là "chưa rõ lần dự thảo" (khi đó không được có — không bịa ngày);
- `catalogue.yaml`: `articles` (nếu có) không rỗng từng phần tử;
- `formulas.yaml` (nếu có, xem 3.5) đọc strict; mọi `required_tools` là tool công thức (`domaintools.IsDomainTool`) phải được ít nhất một công thức mô tả;
- **lint** (công cụ hỗ trợ SuperAdmin duyệt, không thay review tay; `TestShippedPacksAreValid` đòi 0 phát hiện): mẫu nghi prompt injection ("bỏ qua hướng dẫn", "ignore previous", "system prompt"), tên tool không thuộc `required_tools` ∪ tool luôn-bật, số hiệu được trích trong văn bản gói mà không có trong `catalogue.yaml`, mục danh mục không được trích ở đâu, biểu thức công thức ghi tỷ lệ dạng hằng số ("× 20 %").

### 3.5 Công thức (`formulas.yaml`)

```yaml
formulas:
  - id: thue_theo_ty_le_doanh_thu        # snake_case, không trùng; = id hàm Go
    name: Thuế TNDN tính theo tỷ lệ % trên doanh thu
    expression: Thuế theo % doanh thu = Doanh thu × Tỷ lệ % doanh thu
    variables:
      - {name: doanh_thu, description: Doanh thu năm, unit: đồng, source: input}
      - {name: ty_le_doanh_thu, description: Tỷ lệ thuế TNDN trên doanh thu, unit: "%", source: kb}
    basis:
      - {number: 67/2025/QH15, article: ""}   # số hiệu phải có trong catalogue.yaml; điều chỉ ghi khi nguồn nêu
    tool: compare_tax_methods                # phải nằm trong required_tools
    examples:                                # ≥ 1; hàm Go phải tái tạo (tiền ±1 đ, tỷ lệ ±1e-9)
      - inputs: {doanh_thu: 8000000000, ty_le_doanh_thu: 1.5}
        expected: {thue_theo_doanh_thu: 120000000}
```

- Không có hằng số luật định trong công thức: thuế suất, tỷ lệ, ngưỡng là biến `source: kb` (tra KB lúc trả lời) — lint bắt "20 %" trong `expression`.
- Mã: `internal/agent/domaintools` là registry id → hàm thuần `func(map[string]float64) (map[string]float64, error)` (không giá trị mặc định: thiếu biến = lỗi); mỗi gói một package con (`opcvn`). Registry gói từ chối công thức không có hàm Go cho đúng tool. Test: mọi công thức của mọi gói ship có hàm và qua mọi ví dụ; mọi hàm được mô tả trong một `formulas.yaml`.
- Tool đọc khối "Công thức áp dụng" (tên, biểu thức, căn cứ số hiệu + điều) do `Registry.FormulaBlock(tool)` sinh từ `formulas.yaml` và nối vào kết quả — văn bản công thức chỉ viết một lần, không lặp trong Go.
- `formulas.yaml` không phục vụ qua `read_file`; model nhận những gì cần qua kết quả tool.
- Đổi công thức = sửa `formulas.yaml` + mã Go, review, build lại (không sửa lúc chạy).

Hạn chế đã biết: khối "Công thức áp dụng" gộp công thức của mọi gói mô tả cùng một tool, không lọc theo gói của agent. Vô hại khi mỗi tool chỉ thuộc một gói; khi gói thứ hai dùng lại tool thì phải truyền danh sách gói của agent vào `FormulaBlock`.

## 4. Quy tắc 4+1 lớp

| Lớp | Được phép | Cấm | Cơ chế |
| --- | --- | --- | --- |
| a) `SKILL.md` | quy trình, câu hỏi khai thác dữ kiện, cấu trúc trả lời, phạm vi/ngoài phạm vi, bản đồ tài nguyên, tên tool WeRAG | "luôn/bắt buộc" về hành vi; bảng trạng thái; thuế suất/ngưỡng; script; hướng dẫn cài; web | review tay + test 3.4 |
| b) `catalogue.yaml` | số hiệu + "dùng cho" | tự khai IN FORCE/REPLACED | lệnh đối chiếu (giai đoạn 2) tra `legal_documents.number_key`; thiếu → `legal_document_requests` (nguồn `pack:<name>`); lệch → báo SuperAdmin |
| c) `references/`, `assets/` | bình luận, so sánh, checklist, mẫu; đầu mỗi tệp ghi "Bình luận tham khảo — không phải căn cứ" | trích nguyên văn điều luật không có số hiệu + ngày; số liệu thay đổi theo văn bản | chỉ đọc qua `read_file(skill://…)`, không phải bằng chứng. **Không** nạp vào KB cho tới khi có cờ KB `evidence_role=reference` (giai đoạn 3) |
| d) mã | tool Go cho phép tính có công thức cố định; footer; cảnh báo hết hạn rà soát; nhãn dự thảo (khối "Lưu ý về dự thảo", đã làm ở GĐ 2 — §6) | script trong sandbox cho loại phép tính này | tool đăng ký trong `AvailableToolDefinitions`, bật theo agent — xem 4.1 |
| e) `evals/` | ≥ 20 câu, rubric nhị phân | kỳ vọng tự do | chạy 2 cấu hình (có/không gói) — mục 7 |

### 4.1 Tính toán: backend hay sandbox

Gói và skill đều do SuperAdmin duyệt trước khi thêm, nên nơi chạy **không** chọn theo độ tin cậy của người viết mà theo tính chất phép tính:

| | Tool Go trong backend | Script trong sandbox |
| --- | --- | --- |
| Dùng cho | phép tính có công thức cố định, đầu vào có schema (so sánh thuế, tính hạn, lệ phí, quy đổi) | xử lý dữ liệu / tạo tệp không đoán trước được (Excel, Word, biểu đồ từ dữ liệu người dùng, đối chiếu bảng kê), cần thư viện (pandas, openpyxl, OCR) |
| Vì sao | model chỉ điền tham số theo schema, không tự ghép dòng lệnh (không thể quên tham số rồi rơi vào mặc định); kết quả ổn định, có unit test; khối "Giả định" do mã tạo; không phụ thuộc sandbox, không thêm độ trễ khởi động | lệnh do model ghép **lúc chạy** từ câu hỏi và tệp người dùng tải lên — thứ SuperAdmin không duyệt trước được (tệp tải lên có thể chứa câu lệnh chèn). Sandbox giới hạn hậu quả khi model bị dẫn sai |
| Ràng buộc | hàm thuần: không I/O, không mạng, không DB, không đọc tệp; đầu vào có giới hạn (số dương, khoảng hợp lệ), không có giá trị mặc định cho tham số pháp lý (thuế suất, tỷ lệ); output có khối "Giả định"; unit test | kết quả chỉ là sản phẩm phụ — căn cứ, footer, nhãn dự thảo vẫn do mã ngoài sandbox |
| Vị trí | `internal/agent/domaintools/<pack>/` (mỗi gói một package, không import service/repository); khai trong `required_tools`, đăng ký trong `AvailableToolDefinitions`/`registerTools`, bật qua `allowed_tools` của agent | skill Tổ chức (snapshot image của sandbox config), cài và duyệt bởi SuperAdmin |

Cái giá của tool Go là **gắn kết**: mỗi gói có tool thì thêm mã vào binary lõi và đổi công thức phải build lại backend. Giữ cái giá đó nhỏ bằng ranh giới package ở trên; nếu sau này nhiều gói chỉ cần công thức đơn giản, cân nhắc tool công thức khai báo (công thức trong manifest, bộ đánh giá biểu thức an toàn) thay vì viết Go cho từng gói.

**Gói cần script sandbox chưa được hỗ trợ ở giai đoạn 1–2.** Hiện gói nhúng cấm `scripts/`, tệp thực thi, và `scripts/`/`shell_exec`/`$WEKNORA_SKILL_DIR` trong SKILL.md; manifest decode strict nên trường `requires_sandbox` chưa tồn tại (khai hôm nay = lỗi khởi động). Khi có gói đầu tiên cần (ví dụ Excel add-in): thêm `requires_sandbox` vào `Metadata`, script nằm ở một skill Tổ chức riêng (snapshot image), SKILL.md của gói chỉ nêu tên skill đó, và nới các lệnh cấm tương ứng cho đúng trường hợp này.

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
- **Hiển thị cho model**: thân `SKILL.md` của mỗi gói dùng được được **chèn nguyên vào system prompt** trong khối `<domain_pack name=… version=…>…</domain_pack>` (mục `domain_packs`, sau danh sách skill; `agent.SetDomainPackPrompts`), tổng thân tối đa `agent.MaxDomainPackPromptBytes` = 12 KB theo thứ tự gói; gói vượt ngân sách vẫn có khối `<domain_pack>` nhưng không có thân, thay bằng câu bảo model đọc `read_file(path=skill://<tên>/SKILL.md)` trước khi trả lời câu thuộc phạm vi gói (log cảnh báo). `SKILL.md` không được chứa thẻ `<domain_pack`, `</domain_pack>`, `<steer_message` (registry từ chối); không chèn trong lượt cài skill. Lý do: eval 18/10/2026 — chỉ liệt kê tên + mô tả thì Qwen đọc `SKILL.md` ở 1/30 lượt. Danh sách skill (tên + mô tả) vẫn giữ để model đọc tài nguyên theo nhu cầu bằng `read_file(skill://<name>/references/…)`. `read_file` chỉ phục vụ `SKILL.md`, `references/**`, `assets/**`; `catalogue.yaml`, `evals/`, `ATTRIBUTION.md` chỉ dành cho test và lệnh đối chiếu.
- **Footer**: `appendDomainFooter` gọi sau `appendSourceEditNote` ở **cả hai** đường trả lời: `internal/agent/engine.go` (model dừng tự nhiên) và `internal/agent/finalize.go` (tổng hợp, gồm cả hết vòng lặp). Chunk phải emit trên cùng `answerID` **trước** sự kiện `Done` — handler stream cộng dồn theo id nên footer vừa hiện vừa được lưu; emit sau `Done` thì không vào DB. Footer là khối cuối cùng (sau "Lịch sử sửa đổi"), đúng một lần; bỏ qua khi câu trả lời rỗng và trên các câu cố định do mã ("không tìm thấy căn cứ", fallback, forced final, synthesis fallback, vòng lặp lặp lại). Khối mở bằng dấu mốc cố định `\n\n---\n**Lưu ý về cẩm nang chuyên môn**\n` (`domainpacks.FooterMarker`); khi câu trả lời được phát lại làm lịch sử cho lượt sau, mọi thứ từ dấu mốc trở đi bị cắt (như "Lịch sử sửa đổi"), nếu không Qwen chép footer và câu trả lời có hai footer. Phòng thủ thêm: câu footer nào đã có trong câu trả lời thì không chèn lại.
- **Khối "Kết quả tính"**: tool để Markdown trong `ToolResult.Data["answer_block"]` (`types.ToolDataAnswerBlock`), dựng từ kết quả có cấu trúc (không phân tích lại văn bản). `appendToolAnswerBlocks` chèn, ở cả hai đường trả lời, khối của lần gọi thành công **cuối** của mỗi tool, đúng một lần, sau ghi chú nguồn và **trước** footer, dưới dấu mốc `types.ToolAnswerBlockMarker`; có cả ở synthesis fallback, không có ở câu "không tìm thấy căn cứ". Một phép tính = một khối kết quả: các lần gọi cho ra cùng khối (dù tham số viết khác, "1" hay 1.0) tính một lần; khối khác nhau → mỗi khối "Lần tính n/m". Tiêu đề: "Kết quả tính (do hệ thống tính — nếu phần trên khác, lấy khối này làm chuẩn)". Ở synthesis fallback, nếu đã chèn khối kết quả thì chèn cả footer. `compare_tax_methods` đưa số liệu, dòng "Đọc kết quả" (nói rõ chiều so sánh với biên hòa vốn), khuyến nghị, "Giả định" và "Công thức áp dụng"; output cho model dặn chỉ nêu kết luận và lưu ý GTGT, không tự suy luận lại biên hòa vốn (lý do: eval 18/10/2026 — model bỏ khối giả định và đọc ngược biên hòa vốn). `domainpacks.StripFooter` cắt từ dấu mốc nào đến trước khi phát lại lịch sử.
- **Nhãn dự thảo bằng mã (khối "Lưu ý về dự thảo")**: `draft_documents[].aliases` (cụm từ chỉ đúng dự thảo đó; tiêu đề luôn tính; validator: không rỗng, ≥ 4 ký tự, không chung chung như "luật mới"/"dự thảo", không trùng giữa các dự thảo). `Source.DraftNotice` chia **lời của model** thành đoạn (dòng trống, mục danh sách, dòng bảng, tiêu đề — tiêu đề thuộc đoạn ngay sau nó; mục danh sách mang theo đoạn dẫn kết thúc bằng ":" hoặc tiêu đề mở danh sách; `testdata/paragraphs.json` giữ thuật toán trùng với eval), so khớp tên dự thảo (NFC, không phân biệt hoa thường, ranh giới từ); đoạn nhắc dự thảo mà không có "dự thảo"/"dự án luật" → một dòng cho dự thảo đó: "<tiêu đề> là dự thảo (<lần>, ngày dd/mm/yyyy), chưa có hiệu lực; không áp dụng như quy định hiện hành." (chưa rõ lần: "… là dự án luật (chưa rõ lần dự thảo), chưa được thông qua …"). Không sửa lời của model. Chèn ở cả hai đường trả lời, không có ở synthesis fallback và câu "không tìm thấy căn cứ". Lý do: eval 18/10/2026 (`--runs 3`) — nhãn dự thảo trong lời của model chỉ 54 %.
- **Thứ tự các khối do mã chèn**: lời của model → "Lịch sử sửa đổi"/lưu ý wiki/ghi chú nguồn → "Lưu ý về dự thảo" (`domainpacks.DraftNoticeMarker`) → "Kết quả tính" (`types.ToolAnswerBlockMarker`) → footer (`domainpacks.FooterMarker`). Khi phát lại lịch sử, `StripFooter` cắt từ dấu mốc sớm nhất trong ba dấu mốc này; eval (`pack_eval.body_of`) cắt giống vậy và self-check đối chiếu ba hằng số.
- **Gói chỉ hiệu lực khi đủ tool**: lúc chạy, gói có `required_tools` không nằm hết trong tool thực sự đăng ký cho lượt đó (allowlist hoặc mặc định, sau khi `registerTools` tước tool không dùng được, ví dụ `search_knowledge` khi không có KB) bị bỏ qua (log), cả trong skill lẫn footer. Tên gói được giải một lần trong `buildAgentConfig` (cắt khoảng trắng, bỏ trùng, chỉ tên có trong build, chỉ agent smart-reasoning).
- **Hết hạn rà soát**: `now > review_by` (theo `types.PromptNow()`) → footer thêm câu "Nội dung cẩm nang có mốc dd/mm/yyyy, đã quá hạn rà soát" và API liệt kê gói trả `overdue`. **Không** tự tắt gói theo đồng hồ (trái nguyên tắc 3); muốn ngắt thì SuperAdmin bỏ gói khỏi agent.
- **Lưu agent**: từ chối tên gói không tồn tại; từ chối khi tool hiệu lực thiếu `required_tools` — tool hiệu lực = `allowed_tools`, rỗng thì `tools.DefaultAllowedTools()`; `search_knowledge` còn cần `kb_selection_mode != "none"` (lúc chạy tool này bị tước nếu không có KB). Tool luôn-bật (`check_legal_status`, `find_related_legal_documents`, `resolve_abbreviation`, `search_document_section`) không được khai trong `required_tools`. Chỉ SuperAdmin sửa agent (đã đúng theo route).
- **API**: `GET /api/v1/agents/domain-packs` trả `name, description, version, as_of, review_by, overdue, required_tools` cho trình sửa agent.
- **Pin `@gói`**: `applyPerRequestSkillScope` chấp nhận tên gói như built-in.

## 7. Ngưỡng eval (hiệu chỉnh sau lần chạy đầu)

| Tiêu chí (nhị phân) | Ngưỡng |
| --- | --- |
| Trạng thái văn bản nêu đúng theo danh mục | ≥ 95 % câu có số hiệu |
| Dự thảo được gắn nhãn "dự thảo" khi nhắc tới — tính cái người dùng đọc: lời model **hoặc** khối "Lưu ý về dự thảo" do mã chèn (tỷ lệ chỉ trong lời model báo cáo riêng, không chặn) | 100 % |
| Footer có mặt | 100 % (do mã, có test đơn vị) |
| Không bịa số hiệu (số hiệu trong phần Căn cứ ∈ danh mục ∪ chunk đã lấy) | 100 % — đo offline bằng script eval (regex số hiệu, đối chiếu tool results trong SSE) cho tới khi có bộ kiểm runtime ở GĐ 3 |
| Không xấu hơn cấu hình không-gói trên bộ `trap` + `nli` | 0 câu xấu đi |
| Độ trễ | trung vị của 3 lần chạy ≤ +40 % so với không-gói (phương sai một lần đã đo ±30 %) |

## 8. Phân quyền

- Thêm/sửa gói = commit vào repo (review code). Gán gói cho agent, chọn KB, bật/tắt = **SuperAdmin**.
- Tenant Admin quản lý thành viên và KB riêng; không gán gói, không sửa agent.
- Phiên bản mới của gói đi cùng bản build; CHANGELOG trong ATTRIBUTION.md ghi số hiệu thêm/bớt.
- Mỗi Tổ chức một agent riêng (không chia sẻ được). Màn quản trị liệt kê agent theo gói để rà khi nâng phiên bản.
- **Trang SuperAdmin "Gói chuyên môn"** (frontend-next, khu quản trị nền tảng; chỉ đọc + nút đối chiếu): danh sách gói (phiên bản, mốc, hạn rà soát, số văn bản/công thức/agent); chi tiết ba tab "Văn bản trích dẫn" (số hiệu, tên, điều trích, trạng thái danh mục, có trong KB, ghi chú hiệu lực, tệp trích, công thức dùng), "Công thức" (biểu thức, biến, căn cứ ok/cảnh báo, ví dụ), "Agent đang dùng" (agent, Tổ chức). Dòng có cảnh báo được tô — đó là danh sách "cần cập nhật gì". Nút "Đối chiếu danh mục" gọi `POST …/reconcile` và dẫn tới hàng đợi yêu cầu văn bản. API: `GET /api/v1/system/domain-packs`, `GET /api/v1/system/domain-packs/:name` (tra danh mục chỉ đọc, không ghi yêu cầu; agent mọi Tổ chức có `config.domain_packs` chứa tên gói), cùng guard SuperAdmin với `/system/legal-requests`. Không sửa gói lúc chạy.

## 9. Lộ trình

| GĐ | Việc | Ghi chú |
| --- | --- | --- |
| 1 | Định dạng gói + nguồn nhúng (`domain-packs/embed.go`, `internal/agent/domainpacks/`) + test 3.4 (gói mẫu trong testdata, không ship); manager coi gói là chỉ-đọc; `DomainPacks` trên agent + nạp lúc chạy + kiểm khi lưu; footer (2 đường) + câu quá hạn; `GET /agents/domain-packs`; chọn gói trong trình sửa agent (frontend-next, en + vi); sinh lại swagger. Thứ tự: nguồn → manager → nạp lúc chạy → footer → cấu hình/API → UI | không migration |
| 2 | Chuẩn hoá `opc-vn-business-law`; tool `compare_tax_methods`; lệnh đối chiếu danh mục → `legal_document_requests`; bộ eval `pack` trong `scripts/legal_eval`; màn quản trị liệt kê agent theo gói (`config->'domain_packs'`) | **Đã làm (18/10/2026)**, gồm trang "Gói chuyên môn" (§8), `formulas.yaml` + `articles` + `cited_in` (§3.2, §3.5). Đối chiếu trên DB dev đã chạy (18/10/2026, gói lên 1.1.0); eval với agent thật chưa chạy. Chi tiết ở §9.1 |
| 3 | Cờ KB `evidence_role=reference`; lọc theo lĩnh vực (`legal_document_links` → `legal_documents.field` + bảng mã lĩnh vực); bộ kiểm phần "Căn cứ" bằng mã | lớn nhất; nhãn dự thảo bằng mã đã chuyển sang GĐ 2 (§6) |

### 9.1 Giai đoạn 2 — đã làm

- **Tool `compare_tax_methods`**: hàm thuần `internal/agent/domaintools/opcvn` (port `so_sanh_thue.py`, test tái tạo self-test) + wrapper `internal/agent/tools/compare_tax_methods.go`; trong `AvailableToolDefinitions`, không trong `DefaultAllowedTools`; `registerTools`; policy model-handle rỗng; trình sửa agent có nhóm "Công cụ tính", bước chat có nhãn (en + vi). Bốn tham số bắt buộc, chấp nhận số trong chuỗi ("8000000000", "1.2e9"), từ chối "8 tỷ"/"1.200.000". Khác script có chủ ý: `ty_le_doanh_thu` chỉ là phần trăm, 0 < x < 100 (script nhận cả 1.5 lẫn 0.015 nên 0.5 thành 50 %), kiểm NaN/vô cực, trần 10^15 đ, thuế suất hiện tới 2 chữ số thập phân.
- **Gói `domain-packs/opc-vn-business-law`** (1.0.0, SKILL.md 5 516 byte): 13 số hiệu trong `catalogue.yaml`, 3 dự thảo trong manifest — hai dự án luật không có lần/ngày trong bản gốc nên `version: chưa rõ lần dự thảo` và không có `date` (validator: `date` bắt buộc trừ đúng trường hợp này, và khi đó không được có). Tài nguyên không khẳng định hiệu lực ("bản gốc ghi …; xác nhận bằng check_legal_status"), số liệu luật định → "tra KB". Bỏ thêm bảng ngưỡng siêu nhỏ (NĐ 80/2021) và trần Luật Hỗ trợ DNNVV 2017 ở `02` (số liệu đổi theo văn bản). Sửa câu biên hòa vốn bị ngược chiều trong `05` gốc. 30 câu eval. `TestShippedPacksAreValid` 0 lint; `TestOpcVNBusinessLawPackShips` khoá hợp đồng của gói.
- **Đối chiếu**: `POST /api/v1/system/domain-packs/:name/reconcile` (SuperAdmin như `/system/legal-requests`), `service.DomainPackReconciler`. Khoá `CatalogNumberKey`, chỉ khớp đúng khoá (bỏ khớp tiền tố). Kết quả `found`, `missing`, `ambiguous`, `errors` (lỗi theo từng số hiệu, các số khác vẫn trả). `status_note`: hết hiệu lực toàn bộ/một phần, ngưng/đình chỉ, chưa có hiệu lực (trạng thái hoặc `effective_on` > hôm nay), quá `expired_on`, bị thay thế/làm hết hiệu lực/ngưng bởi văn bản đã có hiệu lực, được (hoặc sẽ được) sửa đổi bởi văn bản khác. Văn bản có trong danh mục: `held_in_kb` theo cùng cách cổng chat (liên kết đã duyệt hoặc bản đã nạp vào KB công khai theo `external_id`); không KB nào giữ → yêu cầu có `legal_document_id` (nút Ingest cần) và `source = "catalogue"`; số hiệu trùng: bỏ dòng hết hiệu lực khi có dòng còn hiệu lực, như cổng. Không có trong danh mục → yêu cầu `source = ""`. `source` giữ nghĩa của cổng; nguồn gói nằm ở `detail.origin = "pack:<tên>"` (cùng `pack_version`, `used_for`), nên tên gói không bị giới hạn varchar(32). Yêu cầu ghi `last_tenant_id`/`last_user_id` của người gọi, `last_query` = "Danh mục gói chuyên môn …". Đã có yêu cầu cùng (`number_key`, `legal_document_id`) ở mọi trạng thái thì chỉ báo lại, không bump, không mở lại yêu cầu bị từ chối/đã thêm. Số hiệu mà mọi dòng danh mục đều "Hết hiệu lực toàn bộ" không ghi yêu cầu (như cổng — cổng hiện thẻ "đã hết hiệu lực"); `status_note` là tín hiệu để sửa gói. Khác cổng: đối chiếu **không** dùng `KnowledgeNamed` (tìm theo tên tệp/tiêu đề trong KB của agent) vì không có agent/KB cụ thể — văn bản tải lên KB bằng tay mà không có `external_id` sẽ hiện "chưa có trong KB". Phần tra cứu (lookup, `held_in_kb`, `status_note`) tách riêng, dùng chung cho trang chi tiết (chỉ đọc). Không migration (thêm `LegalRequestRepository.ListByNumberKey`, `DomainPackAgentRepository`). Không có GET kết quả lần trước.
- **Eval**: `run_legal_eval.py --set pack --pack <tên> --agent-id … --baseline-agent-id … [--runs 3]`, `--dry-run` (kiểm tệp case, offline), `--self-check` (kiểm các hàm chấm, offline). Kiểm nhị phân: `must_label_draft` (đoạn nhắc dự thảo phải có "dự thảo"/"dự án luật"), `must_call_tool`/`must_not_call_tool`, `must_not_cite_numbers_outside_catalogue` (danh mục ∪ kết quả tool của lượt), `expect_refusal`, `expect_referral`, `must_ask_clarification`, `must_match`/`must_not_match`, `must_state_status` (so với trạng thái trong kết quả `check_legal_status`/`find_related_legal_documents`; không có thì chỉ cần nhắc số hiệu, ghi `unverified`), `must_have_footer` (chỉ agent có gói). Ngưỡng theo §7; "không xấu hơn" tính trên nhóm `draft_trap`/`status`/`refusal` của bộ case gói. Số hiệu chỉ có trong `read_file` (bình luận của gói) không tính là có căn cứ. Ngưỡng không đo được (không câu nào, mọi lượt lỗi, câu không so được, có lượt lỗi) → `OVERALL INCOMPLETE`, mã thoát 2 — không bao giờ tính là đạt.

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
| — | — | **Trạng thái 18/10/2026**: đã áp dụng toàn bộ bảng trên (commit gốc `1cb90548`); lệch nhỏ ghi ở §9.1 và `ATTRIBUTION.md`. Bước 7–9 của §5 (đối chiếu trên DB, eval 2 cấu hình, tạo agent) chưa chạy |
| `evals/evals.json` | e | 6 câu → `evals/cases.jsonl`, mở rộng ≥ 20 (bẫy dự thảo, thuế suất, trốn nợ cũ, ngoài phạm vi) |
| `DISCLAIMER.md`, `README`, `PROMO`, `MAINTAINING`, `CHANGELOG`, `LICENSE` | — | 1 câu → `answer_footer`; attribution → ATTRIBUTION.md; còn lại bỏ |
