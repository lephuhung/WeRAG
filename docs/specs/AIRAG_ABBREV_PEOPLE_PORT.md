# Port AIRAG Abbreviation & People Search — Native Implementation

**Status:** Proposed
**Phụ thuộc:** `AIRAG_REFACTOR_SPEC.md` §4 (file layout), §10 (abbreviation resolution)
**Repo nguồn (reference, sẽ bị stop):** `lephuhung/AIRAG` (`/home/AIRAG`)
**Repo đích:** WeRAG (WeKnora-derived)

> **Định hướng đã đổi:** phương án ban đầu là MCP sidecar trong AIRAG. Vì repo
> AIRAG sẽ bị stop, toàn bộ logic được **port native sang Go trong WeRAG**;
> không phụ thuộc process/DB của AIRAG lúc runtime. MongoDB `10.10.0.120` là
> server ngoài, độc lập với repo AIRAG — vẫn dùng được.

## 1. Mục tiêu & quyết định

| Quyết định | Lựa chọn |
|---|---|
| Kiến trúc | **Native Go** trong WeRAG — không MCP, không service mới |
| Abbreviation dictionary | Bảng `abbreviations` mới trong WeRAG Postgres, **global** (không tenant_id) — giống AIRAG |
| People search | Kết nối trực tiếp MongoDB `10.10.0.120` qua `go.mongodb.org/mongo-driver/v2` (đã có trong go.mod dạng indirect → promote lên direct) |
| Gating people search | Capability-driven registration: env `PEOPLE_SEARCH_ENABLED` + `TenantRoleAdmin` — KHÔNG đưa vào `AvailableToolDefinitions`/`DefaultAllowedTools` (pattern giống `search_memory`/`shell_exec`) |
| Disambiguation | Phase 1: DB-only, không LLM call. Nhiều nghĩa → tool trả danh sách để agent hỏi lại user |

## 2. Nguồn cần port (AIRAG reference)

| Logic | File gốc | Độ lớn |
|---|---|---|
| `Abbreviation` model + CRUD + `expand_ab_in_text` (regex word-boundary, guard số hiệu `172/GM-UBND`, sort desc theo len) | `backend/app/models/abbreviation.py`, `services/abbreviation_service.py` | ~110 dòng |
| Candidate detection `_is_likely_abbreviation` (uppercase 2-5 chars, low-vowel heuristic, stop words) | `backend/app/services/agents/supervisor.py:170-207` | ~40 dòng |
| `SEARCHABLE_COLLECTION_MAP` — 5 lookup types × 7 collections (bhxh/evn/lg/vacxin/cv19/uids/vnvc), fields + display_fields | `backend/app/services/people/mongo_searchable_map.py` | ~270 dòng static data |
| Query + consolidate: `_extract_numbers`, `_query_single_schema_sync` (exact `$in` với int-variant, regex i, phone normalize), `_identity_key` (cccd→bhxh→name+dob\|phone), `_consolidate`, `_build_display_text` (label map, ObjectId strip), busy fallback | `backend/app/services/people/mongo_people_service.py` | ~890 dòng → ~450 Go |
| Mongo client config (serverSelection 3s, socket 15s, `max_time_ms` 5s/query) | `backend/app/services/people/mongo_client.py` | ~95 dòng |

**Không port:** `semantic_preprocessor` (opt-in, LLM disambiguation), `people_doc_search_node` (WeRAG tự dùng `search_knowledge`), `mongo_formatter_node`, agent-graph wiring.

## 3. Thiết kế WeRAG

### 3.1. Storage — bảng `abbreviations` (global)

Migration `migrations/versioned/000108_abbreviations.{up,down}.sql`:

```sql
CREATE TABLE abbreviations (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    short_form  VARCHAR(50)  NOT NULL,
    full_form   VARCHAR(255) NOT NULL,
    description TEXT,
    is_active   BOOLEAN      NOT NULL DEFAULT FALSE,
    suggested_by VARCHAR(36),            -- user id người đề xuất
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX ix_abbreviations_short_form ON abbreviations (lower(short_form));
-- nhiều nghĩa cho 1 short_form là HỢP LỆ (disambiguation) → không unique
```

Model: `internal/types/abbreviation.go`; repository: `internal/application/repository/abbreviation.go`; service: `internal/application/service/abbreviation.go` — theo pattern `mcp_service` hiện có.

### 3.2. REST API quản lý dictionary

`internal/handler/abbreviation.go` + routes (gate theo `rbacGuards`):

| Endpoint | Role | Semantics (theo AIRAG) |
|---|---|---|
| `GET /api/v1/abbreviations?search=&is_active=&page=` | Viewer+ | list + pagination |
| `POST /api/v1/abbreviations` | Viewer+ | tạo suggestion `is_active=false` |
| `PATCH /api/v1/abbreviations/:id` | Admin+ | update; chỉ Admin được đổi `is_active` |
| `DELETE /api/v1/abbreviations/:id` | Admin+ | xóa |

(Frontend quản lý để phase sau — phase 1 quản qua API/SQL là đủ.)

### 3.3. Abbreviation logic — `internal/vietnamese_legal/abbreviation/`

```text
abbreviation/
  expand.go        # ExpandText(ctx, text) -> ExpandResult
  candidates.go    # IsLikelyAbbreviation(word) — port heuristic AIRAG
  store.go         # gọi repository (cache active list + invalidation on write)
```

`ExpandResult`:

```go
type ExpandResult struct {
    Original   string            `json:"original_text"`
    Expanded   string            `json:"expanded_text"`
    Applied    []AppliedAbbr     `json:"applied"`                // {short_form, full_form}
    Ambiguous  map[string][]Abbr `json:"ambiguous,omitempty"`    // nhiều nghĩa → KHÔNG tự expand
    Potential  []string          `json:"potential_abbreviations"`// nghi là viết tắt nhưng chưa có trong DB
}
```

Port nguyên tắc `expand_ab_in_text`:
- Sort active abbreviations theo `len(short_form)` desc (xử lý overlap "AI"/"AIE").
- Pattern: `(?<!/)(?<!-)\b<short>\b(?!/)(?!-)` — **Go `regexp` không hỗ trợ lookbehind/lookahead** → dùng `regexp2` (đã là dep? kiểm tra) hoặc viết matcher thủ công: tìm `\b<short>\b` rồi kiểm ký tự liền trước/sau khác `/` và `-`. Khuyến nghị matcher thủ công — tránh thêm dependency.
- Case-insensitive match, thay bằng `full_form`.
- Multi-meaning (nhiều row active cùng `short_form`): **không expand**, đưa vào `Ambiguous` — khác AIRAG v1 (expand theo row đầu) nhưng khớp tinh thần spec §10 "giữ provenance".

### 3.4. People search — `internal/vietnamese_legal/people/`

```text
people/
  client.go        # mongo-driver/v2 singleton, timeouts như AIRAG
  schema_map.go    # SEARCHABLE_COLLECTION_MAP port thành static Go
  service.go       # SearchCCCD/Name/BHXH/Phone/Advanced
  consolidate.go   # identity key + gộp hồ sơ + display markdown
```

Config mới (env → `internal/config`):

| Env | Default | Ghi chú |
|---|---|---|
| `PEOPLE_SEARCH_ENABLED` | `false` | Master switch — tắt thì tool không register |
| `PEOPLE_MONGO_URI` | — | Full URI override (ưu tiên) |
| `PEOPLE_MONGO_HOST` / `_PORT` / `_USER` / `_PASSWORD` / `_DATABASE` / `_AUTH_SOURCE` | `localhost:27017`, no-auth | Fallback build URI |
| `PEOPLE_MONGO_QUERY_TIMEOUT_MS` | `5000` | Per-query `maxTimeMS` — schema chậm bị skip |

Dùng prefix `PEOPLE_MONGO_*` thay vì `MONGO_*` trần để tránh đụng các service khác sau này.

Semantics giữ nguyên từ AIRAG:
- `cccd`: trích nhóm **9 hoặc 12** chữ số từ input (nhiều giá trị → `$in`), match exact + int variant.
- `bhxh`: nhóm **≥5** chữ số. `phone`: đúng **10** chữ số (strip space/dot/dash trước).
- `name`: regex substring `i`; `advanced`: AND các tiêu chí (name = exact `^...$` i, dob/address = contains i, phone = normalized exact).
- Query song song các schema (errgroup, mỗi query `maxTimeMS` 5s, timeout → skip schema không fail).
- Consolidate theo `_identity_key`: `cccd:<digits>` → `bhxh:<digits>` → `np:<name>|<dob|phone>` → `id:<_id>`.
- Kết quả: `SearchResult{Found bool, Persons []PersonRecord, Display string, Schemas []string, LookupType string, Unavailable bool}`.
- **`Unavailable` phân biệt với not-found**: mất kết nối mongo → `Unavailable=true` + thông báo "hệ thống đang bận", tool trả lỗi mềm để agent truyền nguyên văn — không được để agent trả "không tìm thấy".
- Strip mọi value dạng ObjectId (24 hex) khỏi output (`_is_object_id` port).

### 3.5. Agent tools — `internal/agent/tools/`

```text
resolve_abbreviation.go   # ToolResolveAbbreviation = "resolve_abbreviation"
people_lookup.go          # ToolPeopleLookup        = "people_lookup"
```

**`resolve_abbreviation`** — 1 tool, 3 mode qua param `action`:

```json
{"action": "expand",  "text": "..."}                              // → ExpandResult
{"action": "lookup",  "short_form": "ATTT"}                       // → matches[]
{"action": "suggest", "short_form": "...", "full_form": "...", "description": "..."}
```

- `expand`/`lookup`: safe read — đưa vào `DefaultAllowedTools` (hoặc capability flag `ABBREVIATION_ENABLED`, default on khi bảng tồn tại).
- `suggest`: insert `is_active=false`, `suggested_by` = user từ `types.PrincipalFromContext(ctx)`; trả `{status: "pending_review"}`. Review qua REST §3.2.

**`people_lookup`** — 1 tool gom cả 5 lookup type (ít tool hơn 5 tool rời, khớp spec §4 `people_lookup.go`):

```json
{"lookup_type": "cccd|bhxh|phone|name|advanced",
 "query": "câu hỏi hoặc giá trị thô",          // cho cccd/bhxh/phone/name — server tự trích
 "criteria": {"name": "...", "dob": "...", "address": "...", "phone": "..."}}  // advanced
```

- **Registration gate** (trong `registerTools`, `agent_service.go`): chỉ register khi **cả** `PEOPLE_SEARCH_ENABLED=true` **và** mongo config hợp lệ **và** `TenantRoleFromContext(ctx) == TenantRoleAdmin`. Không nằm trong `AvailableToolDefinitions`/`DefaultAllowedTools` — allowlist do tenant sửa không được quyền bật nó (pattern `search_memory`).
- Lý do role-gate thay vì approval-gate: `internal/agent/approval/Gate` chỉ hỗ trợ MCP tools (`ServiceID`-keyed checker). Tenant admin ≈ superadmin-only của AIRAG.
- Output: `{found, persons[], display, schemas, lookup_type}` — `persons` cho reasoning, `display` (markdown dựng sẵn) để agent trình bày. Kèm `_person_group` để UI gộp card nếu cần.
- Description tool phải ghi rõ: "PII — chỉ trình bày dữ liệu trả về, không suy diễn; khi `unavailable` thì nói đúng 'hệ thống đang bận'".

### 3.6. File layout tổng hợp

```text
migrations/versioned/000108_abbreviations.{up,down}.sql   NEW
internal/types/abbreviation.go                             NEW
internal/application/repository/abbreviation.go            NEW
internal/application/service/abbreviation.go               NEW
internal/handler/abbreviation.go                           NEW
internal/vietnamese_legal/abbreviation/{expand,candidates,store}.go   NEW
internal/vietnamese_legal/people/{client,schema_map,service,consolidate}.go  NEW
internal/agent/tools/resolve_abbreviation.go               NEW
internal/agent/tools/people_lookup.go                      NEW
internal/agent/tools/definitions.go                        EDIT (tool name consts)
internal/application/service/agent_service.go              EDIT (register + gates)
internal/config/config.go (+ .env.example)                 EDIT (PEOPLE_* vars)
internal/router/routes_infra.go (hoặc routes mới)          EDIT (abbreviation CRUD)
go.mod                                                     EDIT (mongo-driver/v2 → direct)
```

## 4. Caveats

1. **PII**: `persons[]` chứa CCCD/BHXH/SĐT thật. Role-gate Admin + cân nhắc log tool args ở mức hash/redact (kiểm `common.PipelineInfo` hiện log full args — có thể cần tool-side redact).
2. **Abbreviation expansion chỉ chạy khi agent gọi** — không deterministic như AIRAG (expand trước routing). Phase 2: hook expand vào QA/session path trước khi agent chạy (xem `session_agent_qa_scope`, quick-answer path) — spec riêng.
3. **Multi-schema regex trên collection lớn** (`lg` ~13M docs): giữ `maxTimeMS` 5s + skip-on-timeout như AIRAG; cân nhắc `PEOPLE_MONGO_PER_SCHEMA_LIMIT` (default 10).
4. **`suggest_abbreviation` tạo rác**: chỉ review bằng tay; nếu spam trở thành vấn đề → thêm gate Admin cho action suggest.
5. **MongoDB là single external dep**: sidecar không còn, nên khi mongo chết → tool trả `unavailable`, agent phải fallback sang `search_knowledge` thay vì khẳng định không có dữ liệu.

## 5. Test plan

- [ ] Migration up/down tạo/xóa bảng đúng; CRUD API: viewer suggest được nhưng không activate được, admin activate được.
- [ ] `ExpandText`: `ATTT` → applied; `172/GM-UBND` không bị expand; multi-meaning → `Ambiguous` chứ không expand bừa.
- [ ] `people_lookup` không xuất hiện trong `GetModelFunctionDefinitions` khi viewer gọi / khi `PEOPLE_SEARCH_ENABLED=false`.
- [ ] `people_lookup(cccd)` trích đúng 9/12 số từ câu tự do; `phone` từ chuỗi có space/dot; kết quả gộp 1 người từ nhiều schema.
- [ ] Mongo down → `Unavailable=true` trong ≤ ~8s, không hang turn.
- [ ] `suggest` → row `is_active=false` + `suggested_by` đúng user; sau khi admin activate qua API thì `expand` thấy ngay (cache invalidation).
- [ ] Regression `go build ./... && go test ./internal/agent/tools/... ./internal/vietnamese_legal/...`

## 6. Open questions

- Có cần tool `people_doc_search` companion (tài liệu nhắc tới người vừa tra, theo `people_doc_search_node` của AIRAG) không, hay để agent tự gọi `search_knowledge`? (Đề xuất: để agent tự gọi — ít code hơn.)
- `resolve_abbreviation` nên vào `DefaultAllowedTools` luôn hay sau flag `ABBREVIATION_ENABLED`?
- Collection map của AIRAG có vài chỗ TODO chưa chắc field (evn CCCD, cv19 CCCD, uids name) — giữ nguyên y như AIRAG, confirm field với người quản mongo sau.
