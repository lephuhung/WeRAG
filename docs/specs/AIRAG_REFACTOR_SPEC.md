# WeRAG — AIRAG-Oriented Refactor Specification

**Status:** Proposed  
**Target repository:** `lephuhung/WeRAG`  
**Target branch:** `spec/airag-refactor`  
**Baseline:** WeRAG `main` (WeKnora-derived, v0.8.0 lineage)  
**Reference architecture:** `lephuhung/AIRAG` branch `feat/langgraph-v2`

## 1. Mục tiêu

Refactor WeRAG thành nền tảng RAG/Agent tối ưu cho tài liệu pháp luật và hành chính Việt Nam, tận dụng phần platform đã trưởng thành của WeKnora và chuyển các năng lực domain quan trọng từ AIRAG sang theo các extension point ổn định.

Mục tiêu không phải là port toàn bộ AIRAG sang Go, cũng không phải nhúng LangGraph vào WeRAG. Mục tiêu là:

1. Giữ nguyên các năng lực platform của WeKnora/WeRAG:
   - Web UI;
   - tenant/workspace;
   - RBAC và ownership;
   - knowledge base;
   - session/chat;
   - Quick Answer;
   - ReAct Agent;
   - Tool Registry;
   - MCP;
   - async task queue;
   - storage;
   - vector store;
   - hybrid retrieval;
   - citation/reference drawer;
   - temporary attachment;
   - model/provider management;
   - observability/audit.

2. Port các năng lực domain từ AIRAG:
   - Vietnamese legal document parsing;
   - OCR cho tài liệu scan;
   - legal structure/metadata;
   - legal-aware chunking;
   - exact document resolution;
   - abbreviation resolution;
   - conversation/document context;
   - multi-intent routing policy;
   - people lookup/business tools;
   - legal knowledge graph;
   - legal comparison/compliance workflows khi cần.

3. Giữ patch surface nhỏ để có thể merge/rebase upstream WeKnora về sau.

## 2. Nguyên tắc kiến trúc

### 2.1. WeRAG là platform; AIRAG cung cấp domain intelligence

Boundary mục tiêu:

```text
                    WeRAG Platform
                         │
        ┌────────────────┼────────────────┐
        │                │                │
   Ingestion/RAG      Agent Runtime     Security/UI
        │                │                │
        │                │                │
        └──────── Vietnamese Legal Layer ┘
                         │
             parser / OCR / resolver /
             metadata / tools / KG
```

Không thay:
- auth;
- tenant;
- RBAC;
- session;
- ReAct engine;
- ToolRegistry;
- vector-store drivers;
- task queue;
- storage;
- citation protocol.

Chỉ mở rộng chúng qua extension point hoặc service riêng.

### 2.2. Không port AIRAG Supervisor/LangGraph nguyên khối

AIRAG v2 có các invariant hữu ích:
- semantic intent phải nhìn toàn bộ query;
- regex/entity không được quyết định top-level route;
- single atomic intent có fast path;
- multi-intent hoặc single complex intent đi complex path;
- context resolution phải xảy ra trước semantic classification;
- exact identifiers phải deterministic;
- document-grounded answer phải giữ source identity/citation rõ ràng.

Các invariant này được adapt vào WeRAG, nhưng execution runtime vẫn là:
- Quick Answer cho fast path;
- WeKnora Smart Reasoning/ReAct cho complex path;
- built-in/native tools hoặc MCP cho domain capabilities.

Không tạo một scheduler/checkpointer LangGraph thứ hai trong giai đoạn đầu.

### 2.3. Deterministic identity trước semantic retrieval

Các câu như:

```text
Nghị định 13
Thông tư 15 năm 2020
Điều 5 khoản 2 của nghị định này
```

là bài toán entity/document resolution, không phải vector search.

Luồng đúng:

```text
reference
   ↓
resolve exact document
   ↓
knowledge_id/document_id
   ↓
retrieval scoped inside document
```

### 2.4. KG bổ sung, không thay thế vector/BM25

Một query có thể dùng:
- exact resolver;
- BM25;
- vector;
- parent-child expansion;
- KG;
- read_document.

KG không phải default replacement cho chunk retrieval.

---

## 3. Kiến trúc mục tiêu

```text
User / API / Embed
        │
        ▼
Session + Tenant/RBAC
        │
        ▼
Conversation Context Resolver
        │
        ├── resolved_documents
        ├── quoted_sources
        ├── temporary_attachments
        └── workspace/KB scope
        │
        ▼
Semantic Intent Analysis (optional auto mode)
        │
        ▼
Deterministic Entity Extraction
        │
        ▼
Execution Policy
    ┌───┴─────────────────────────────┐
    │                                 │
single atomic                    multi/complex
    │                                 │
Quick Answer                    ReAct Agent
    │                                 │
    └───────────────┬─────────────────┘
                    │
          Domain + RAG Tool Surface
                    │
       ┌────────────┼────────────┐
       │            │            │
resolve_doc   search/read KB   people/KG
       │            │            │
       └────────────┼────────────┘
                    ▼
             Grounded Answer
                    │
              Legal Citation
```

---

## 4. Module boundary mới

Domain code phải được cô lập.

Đề xuất:

```text
internal/
  vietnamese_legal/
    model/
    metadata/
    normalizer/
    resolver/
    abbreviation/
    extractor/
    retrieval/
```

Parser Python:

```text
docreader/parser/
  vietnamese_legal_parser.py
```

Chunker Go:

```text
internal/infrastructure/chunker/
  vietnamese_legal.go
```

OCR adapter:

```text
internal/infrastructure/docparser/
  hunyuanocr_converter.go
```

Agent tools:

```text
internal/agent/tools/
  resolve_legal_document.go
  resolve_abbreviation.go
  people_lookup.go
  legal_relation_lookup.go
```

Không đặt logic `Điều/Khoản/Điểm`, normalization số hiệu hoặc document resolution rải rác trong generic parser/retriever/agent files.

---

## 5. Vietnamese Legal Document Intelligence

### 5.1. Pipeline chung

Cả KB ingestion và temporary chat attachment phải dùng cùng document-intelligence layer:

```text
File
 │
 ├── digital PDF/DOCX
 │      ↓
 │   DocReader/Docling-like parser
 │
 └── scanned PDF/image
        ↓
     HunyuanOCR
        │
        ▼
VietnameseLegalNormalizer
        │
        ▼
Legal Structure Extraction
        │
        ▼
Legal AST / normalized blocks
        │
        ▼
Vietnamese Legal Chunker
```

Không duy trì hai parser domain riêng cho KB và attachment.

### 5.2. Legal structure

Cấu trúc tối thiểu:

```text
Document
 ├── Phần
 ├── Chương
 │    ├── Mục
 │    │    └── Điều
 │    │         ├── Khoản
 │    │         │    └── Điểm
 │    │         └── ...
 │    └── ...
 └── ...
```

Phải hỗ trợ tối thiểu:
- `PHẦN`;
- `CHƯƠNG`;
- `Mục`;
- `Điều`;
- `Khoản`;
- `Điểm`;
- tiêu đề/chú thích liên quan;
- page range;
- source range.

### 5.3. Raw và normalized content

Không overwrite OCR/raw text vĩnh viễn.

Mỗi chunk cần giữ được:
- raw/source content;
- normalized content;
- structural metadata;
- parser/OCR provenance.

WeRAG đã có `Chunk.SourceContent`, `Chunk.Content`, `Chunk.Metadata`, `ContextHeader`; tận dụng các trường này trước khi thêm schema mới.

Ví dụ:

```json
{
  "source_content": "Ðiều 15. Hô sơ đề xuất cấp độ",
  "content": "Điều 15. Hồ sơ đề xuất cấp độ",
  "context_header": "Nghị định ... > Chương II > Điều 15 > Khoản 2",
  "metadata": {
    "article": "15",
    "clause": "2",
    "page_start": 12,
    "page_end": 13,
    "parser": "vietnamese_legal",
    "ocr_engine": "hunyuanocr"
  }
}
```

Normalization phải bảo thủ, đặc biệt với:
- số hiệu văn bản;
- ngày/tháng/năm;
- tỷ lệ/phần trăm;
- mã tiêu chuẩn;
- URL/IP;
- Điều/Khoản/Điểm.

Không dùng LLM tự do để “sửa đẹp” các literal có tính pháp lý.

---

## 6. OCR/HunyuanOCR

### 6.1. Nguyên tắc

Không OCR lại digital PDF có text layer tốt.

```text
digital page → text extraction
scanned page → OCR
```

### 6.2. Full-document OCR cho legal document

Đối với văn bản pháp luật scan dài, ưu tiên:

```text
PDF
 ↓
HunyuanOCR full-document endpoint
 ↓
ordered Markdown/structured output
 ↓
legal normalizer
 ↓
legal structure
```

Không xem mỗi page OCR là một legal document chunk độc lập vì Điều/Khoản có thể chạy qua page boundary.

### 6.3. Adapter

Thêm reader/converter theo pattern PaddleOCR-VL/MinerU hiện có:

```text
internal/infrastructure/docparser/hunyuanocr_converter.go
```

Config tối thiểu:
- base URL;
- timeout;
- max pages;
- render DPI;
- concurrency;
- retry;
- health check.

Nếu deployment hiện tại dùng HunyuanOCR ở `:8001`, adapter chỉ gọi service; không nhúng model OCR vào app container.

### 6.4. Fallback

Nếu HunyuanOCR unavailable:
1. nếu file có text layer đủ tốt → tiếp tục native parse;
2. nếu scan → fallback VLM OCR nếu được cấu hình;
3. nếu vẫn thất bại → fail parse có lý do rõ ràng, không tạo knowledge “completed” rỗng.

---

## 7. Vietnamese Legal Chunker

### 7.1. Strategy mới

Thêm:

```go
StrategyVietnameseLegal = "vietnamese_legal"
```

vào chunker hiện tại.

Không nhồi luật tiếng Việt vào generic `heuristic`.

### 7.2. Parent-child

Mặc định legal:

```text
Parent = Điều
Child  = Khoản / Điểm / semantic subsection
```

Retrieval chạy trên child nhưng có thể expand parent/neighbor khi assemble context.

### 7.3. ContextHeader

`ContextHeader` phải mang breadcrumb:

```text
Nghị định 13/2023/NĐ-CP > Chương II > Điều 15 > Khoản 2 > Điểm a
```

Embedding dùng:

```text
ContextHeader + Content
```

trong khi literal citation vẫn dùng `Content`.

### 7.4. Fallback

Nếu parser không xác định được legal structure:
- fallback `heading`;
- sau đó `heuristic`;
- cuối cùng generic recursive/legacy.

Không fail toàn bộ ingestion chỉ vì legal recognizer không match.

---

## 8. Legal metadata và canonical document identity

### 8.1. Document metadata

Tối thiểu:

```json
{
  "document_id": "canonical-domain-id",
  "document_type": "nghi_dinh",
  "document_number": "13",
  "year": "2023",
  "canonical_number": "13/2023/NĐ-CP",
  "issuing_agency": "...",
  "issued_date": "...",
  "effective_date": "...",
  "status": "effective|amended|superseded|unknown"
}
```

Tách:
- `document_id`: domain identity;
- `knowledge_id`: WeRAG internal knowledge UUID.

### 8.2. Dedicated resolver index

Không query JSON metadata toàn corpus cho mọi lần resolve.

Thêm bảng:

```text
legal_document_index
- id
- tenant_id
- knowledge_base_id
- knowledge_id
- document_id
- document_type
- document_number
- year
- suffix
- canonical_number
- issuing_agency
- title_normalized
- created_at
- updated_at
```

Index:
- `(tenant_id, document_type, document_number)`;
- `(tenant_id, document_type, document_number, year)`;
- `(tenant_id, canonical_number)`;
- `(tenant_id, document_id)`.

Resolver luôn enforce tenant/KB access trước khi trả `knowledge_id`.

---

## 9. `resolve_legal_document` tool

### 9.1. Input

Tối giản:

```json
{
  "reference": "Nghị định 13"
}
```

Optional:
- allowed KB handles;
- conversation-resolved document context.

Không yêu cầu model tự parse chính xác từng field.

### 9.2. Output

Resolved:

```json
{
  "status": "resolved",
  "document_id": "DOC-...",
  "knowledge_id": "...",
  "canonical_number": "13/2023/NĐ-CP",
  "title": "...",
  "confidence": 1.0
}
```

Ambiguous:

```json
{
  "status": "ambiguous",
  "candidates": [
    {
      "document_id": "...",
      "knowledge_id": "...",
      "canonical_number": "...",
      "title": "..."
    }
  ]
}
```

Not found:

```json
{
  "status": "not_found",
  "normalized_reference": "..."
}
```

### 9.3. Resolution order

1. exact canonical number;
2. type + number + year;
3. type + number;
4. normalized title/alias;
5. conversation-resolved reference;
6. return ambiguity/not found.

Không fallback sang vector retrieval để tự “chọn đại” một văn bản.

### 9.4. POC integration

Giai đoạn đầu cho phép giữ resolver AIRAG dưới dạng MCP service để giảm patch core.

Sau khi contract ổn định mới port thành native `types.Tool`.

---

## 10. Abbreviation resolution

Port logic từ AIRAG thành helper/tool tách biệt.

Mục tiêu:
- map abbreviation → canonical expansion;
- không làm top-level routing;
- có tenant/domain dictionary;
- hỗ trợ context-sensitive aliases;
- giữ provenance.

Ví dụ:

```text
ATTT → an toàn thông tin
HTTT → hệ thống thông tin
```

Resolver chỉ enrich query/context; không được tự quyết route.

---

## 11. Query Context Contract

Thêm typed runtime contract cho mỗi turn.

Đề xuất:

```json
{
  "query": "So sánh khoản này với Nghị định 13",

  "scope": {
    "knowledge_base_ids": [],
    "document_ids": []
  },

  "resolved_documents": [
    {
      "reference": "Nghị định 13",
      "document_id": "...",
      "knowledge_id": "..."
    }
  ],

  "quoted_sources": [
    {
      "knowledge_id": "...",
      "chunk_id": "...",
      "revision": 3,
      "article": "15",
      "clause": "2",
      "selection": {
        "start": 120,
        "end": 300
      }
    }
  ],

  "attachments": [
    {
      "attachment_id": "...",
      "scope": "session"
    }
  ],

  "intent_analysis": null,
  "entities": []
}
```

### 11.1. Priority khi resolve context

```text
explicit quote
  >
explicit @file/@KB scope
  >
resolved current-turn document
  >
conversation-resolved document
  >
general retrieval
```

### 11.2. Conversation references

Các câu:
- `nghị định này`;
- `văn bản trên`;
- `điều vừa nói`;
- `quy định này`;

phải resolve trước semantic intent classification nếu conversation state đủ rõ.

Không biến conversation reference thành semantic intent.

---

## 12. Semantic intent và multi-intent policy

Adapt từ AIRAG `docs/multi-intent-routing-spec.md`.

### 12.1. Rule

```text
LLM decides WHAT the user wants.
Deterministic extraction identifies exact entities.
Execution policy decides fast vs complex.
ReAct determines HOW to execute complex work.
```

### 12.2. Không route bằng regex

Cấm:

```text
phone regex → people route
document number regex → document route
```

Regex/NER chỉ:
- extract;
- normalize;
- validate;
- bind tool arguments.

### 12.3. IntentAnalysis

```json
{
  "primary_intent": "evaluate_compliance",
  "intents": [
    {"name": "people_lookup", "confidence": 0.99},
    {"name": "document_search", "confidence": 0.95},
    {"name": "evaluate_compliance", "confidence": 0.98}
  ],
  "is_multi_intent": true,
  "requires_complex_execution": true
}
```

### 12.4. Intent registry

Code-owned registry:

```text
people_lookup        atomic
document_lookup      atomic
document_search      atomic
direct_answer        atomic
compare_documents    complex
evaluate_compliance  complex
summarize_multi_doc  complex
cross_domain_research complex
```

### 12.5. Routing invariant

```text
FAST_PATH =
  exactly one intent
  AND intent.execution == atomic
```

Mọi trường hợp còn lại → complex/ReAct.

Unknown/uncertain → complex, không rơi về fast path.

### 12.6. Adapt vào WeRAG

Không bắt buộc auto-router ở phase đầu.

Triển khai theo feature flag:

```text
WEKNORA_VN_AUTO_ROUTING=false
```

Khi off:
- giữ UI mode Quick Answer / Smart Reasoning như upstream.

Khi on:
- preflight semantic classifier chọn Quick Answer hoặc Smart Reasoning;
- deterministic policy giữ authority;
- model classifier không trực tiếp execute tool.

---

## 13. Agent tool surface

Tool domain tối thiểu:

```text
resolve_legal_document
resolve_abbreviation
```

Phase sau:

```text
people_lookup
legal_relation_lookup
legal_compare
```

Giữ built-in:
- `search_knowledge`;
- `read_document`;
- `list_documents`;
- `query_knowledge_graph`.

### 13.1. Example

Query:

```text
Nghị định 13 quy định gì về hồ sơ cấp độ?
```

Expected:

```text
resolve_legal_document("Nghị định 13")
  ↓
knowledge_id
  ↓
search_knowledge(query="quy định về hồ sơ cấp độ", scope=document)
  ↓
read_document(...) nếu passage chưa đủ
  ↓
answer + citation
```

### 13.2. Multi-intent example

```text
Tìm thông tin số điện thoại 0989755968 và đối chiếu quy định
về hồ sơ cấp độ xem vi phạm gì không
```

Expected intent:
- people_lookup;
- document_search;
- evaluate_compliance.

Expected execution:
- complex/ReAct;
- people lookup và legal retrieval đều được thực hiện;
- không collapse thành people-only.

---

## 14. Retrieval

Giữ retrieval infrastructure của WeRAG:
- vector;
- BM25/keyword;
- hybrid/RRF;
- rerank;
- parent-child;
- multi-KB fanout.

Bổ sung policy domain:

1. Exact identifiers/document numbers → keyword/exact resolver trước.
2. Legal query đã resolve document → hard scope theo `knowledge_id`.
3. Semantic legal query không chỉ định document → hybrid retrieval.
4. Citation/quotation cần exact source passage → `read_document`.
5. KG chỉ thêm relational context khi cần.

---

## 15. Temporary chat attachments

Giữ lifecycle upstream:
- session-scoped;
- async parse;
- status polling;
- preview;
- TTL cleanup;
- tối đa 5 attachment/turn;
- image/VLM support.

### 15.1. Thay quality layer

Hiện temporary document dùng generic auto chunking và lexical term scoring.

Đối với legal documents:
- dùng cùng Vietnamese Legal Parser;
- dùng legal chunker;
- giữ legal metadata;
- scanned file dùng HunyuanOCR.

### 15.2. Small vs large

```text
small attachment
  → full content in prompt

large attachment
  → temporary chunk retrieval
```

Phase đầu có thể giữ lexical selector.

Phase sau thêm temporary hybrid retrieval:
- keyword/BM25;
- optional in-memory/local vector;
- session/attachment scope;
- TTL cleanup.

Không đưa attachment tạm vào permanent KB/vector collection mặc định.

### 15.3. External regulation case

User upload A + B rồi hỏi:

```text
Kiểm tra hai hồ sơ này có đúng với Nghị định 13 không?
```

Agent phải:
1. đọc/retrieve A;
2. đọc/retrieve B;
3. resolve Nghị định 13;
4. retrieve regulation;
5. synthesize comparison.

---

## 16. Quote in chat

WeRAG đã có answer citation → reference drawer.

Bổ sung chiều ngược:

```text
Document/reference drawer
      ↓
Quote in chat
      ↓
structured quoted_source
```

### 16.1. Message contract

Không chỉ paste text.

Lưu:

```json
{
  "knowledge_id": "...",
  "chunk_id": "...",
  "revision": 3,
  "selection": {
    "start": 120,
    "end": 300
  },
  "snapshot": "..."
}
```

Server re-check:
- tenant;
- KB permission;
- chunk binding;
- revision/source;
- selection range.

### 16.2. UX

Trong Reference Drawer thêm:
- `View source`;
- `Quote in chat`;
- `Ask about this`.

Legal citation display nên ưu tiên:

```text
Nghị định ... · Điều 15 · Khoản 2 · Điểm a
```

thay vì chỉ `filename/chunk`.

---

## 17. Citation và grounded legal answers

Giữ citation protocol hiện tại của WeRAG.

Bổ sung legal projection từ chunk metadata:
- canonical document title/number;
- article;
- clause;
- point;
- page range.

Không để model tự tạo document number/article locator không có trong evidence.

Với claim có literal rủi ro cao:
- số hiệu văn bản;
- ngày;
- tỷ lệ;
- thời hạn;
- Điều/Khoản/Điểm;

nên có validation hậu kỳ ở phase nâng cao, lấy cảm hứng từ AIRAG grounded synthesis.

Không cần port nguyên AIRAG synthesis subgraph ngay phase đầu.

---

## 18. Tenant/RBAC/visibility

Giữ nguyên WeRAG:
- Tenant/Workspace;
- Viewer;
- Contributor;
- Admin;
- Owner;
- creator ownership;
- organization sharing;
- API-key capability;
- agent sharing;
- audit.

Không thêm bypass.

Optional domain metadata:

```text
classification:
- public
- internal
- restricted
```

Classification là policy bổ sung, không thay RBAC.

Public Agent chỉ được bind KB/document classification cho phép.

---

## 19. Legal KG

Phase sau ingestion/resolver.

Ontology ban đầu:

```text
Document
Article
Organization
Person
Province
```

Relations:
- issued_by;
- amends;
- supersedes;
- references;
- implements;
- responsible_for;
- applies_to;
- contains_article.

Article luôn thuộc Document.

KG extraction dùng canonical entity + aliases trước khi insert.

Agent dùng `query_knowledge_graph`/`legal_relation_lookup` khi câu hỏi cần relation; không dùng KG cho mọi retrieval.

---

## 20. Data migrations dự kiến

Tối thiểu:

1. `legal_document_index`.
2. Optional message `quoted_sources` JSONB nếu không thể reuse metadata hiện có.
3. Optional document classification field nếu `CustomMetadata` không đủ cho policy enforcement.
4. Optional temporary retrieval index metadata.

Ưu tiên dùng JSON metadata sẵn có trước khi thêm column mới.

Mọi migration phải:
- có PostgreSQL + SQLite variant nếu repo đang hỗ trợ cả hai;
- backward compatible;
- không làm upstream fields đổi semantics.

---

## 21. Source-level change map

### Parser

```text
docreader/parser/base_parser.py             KEEP
docreader/parser/registry.py                EXTEND
docreader/parser/vietnamese_legal_parser.py NEW
```

### OCR/docparser

```text
internal/infrastructure/docparser/
  hunyuanocr_converter.go                   NEW
```

### Chunking

```text
internal/infrastructure/chunker/strategy.go EXTEND
internal/infrastructure/chunker/
  vietnamese_legal.go                       NEW
```

### Domain

```text
internal/vietnamese_legal/...               NEW
```

### Agent tools

```text
internal/agent/tools/definitions.go         EXTEND
internal/agent/tools/registry.go            KEEP contract
internal/agent/tools/
  resolve_legal_document.go                 NEW
  resolve_abbreviation.go                   NEW
```

### Temporary attachments

```text
internal/application/service/temporary_document.go EXTEND
internal/types/temporary_document.go                EXTEND only if needed
```

### Message/chat context

```text
internal/types/message.go                    EXTEND quoted source contract
internal/application/service/session_*       EXTEND context assembly
```

### Citation/UI

```text
frontend/src/utils/referenceSources.ts       EXTEND legal fields
frontend/src/views/chat/components/...       EXTEND quote actions
```

### Persistence

```text
migrations/...                               ADD legal resolver index
```

---

## 22. Feature flags

Domain features phải deploy độc lập:

```text
WEKNORA_VN_LEGAL_PARSER_ENABLED
WEKNORA_VN_LEGAL_CHUNKER_ENABLED
WEKNORA_VN_HUNYUAN_OCR_ENABLED
WEKNORA_VN_LEGAL_RESOLVER_ENABLED
WEKNORA_VN_AUTO_ROUTING
WEKNORA_VN_TEMP_HYBRID_RETRIEVAL
WEKNORA_VN_QUOTED_SOURCES
WEKNORA_VN_LEGAL_KG_ENABLED
```

Mặc định rollout mới:
- parser/chunker/resolver: off cho KB cũ, opt-in per KB/process config;
- UI quote: off cho đến khi backend contract ổn định;
- auto routing: off;
- KG: off.

---

## 23. Phased implementation

### Phase 0 — Baseline và upstream discipline

- Ghi rõ upstream `Tencent/WeKnora`.
- Tạo benchmark corpus 50–100 legal documents.
- Chạy baseline Quick Answer và Smart Reasoning.
- Ghi metrics trước refactor.
- Không sửa runtime trong phase này.

Acceptance:
- baseline reproducible;
- test queries/version/model config được lưu.

### Phase 1 — Legal ingestion

- HunyuanOCR adapter;
- legal parser/normalizer;
- `vietnamese_legal` chunk strategy;
- metadata + ContextHeader;
- parent-child legal chunks.

Acceptance:
- Điều/Khoản/Điểm không bị cắt sai ở các fixture chính;
- scanned multi-page article được reconstruct;
- raw content còn giữ;
- generic docs vẫn parse như upstream.

### Phase 2 — Legal document resolver

- `legal_document_index`;
- resolver service;
- `resolve_legal_document` tool;
- conversation document context;
- ambiguity handling.

Acceptance:
- `Nghị định 13/2023/NĐ-CP` exact;
- `Nghị định 13 năm 2023` exact;
- `Nghị định 13` trả candidate/ambiguity đúng;
- không dùng vector để chọn identity.

### Phase 3 — Query policy và domain tools

- semantic multi-intent classifier;
- entity extractor;
- intent registry;
- optional auto routing;
- abbreviation resolver;
- people lookup MCP/native tool.

Acceptance:
- phone-only vẫn fast;
- phone + legal không collapse;
- compare 2 docs đi complex;
- uncertain không đi fast.

### Phase 4 — Attachment + quote + citation

- legal parser cho temporary attachment;
- optional temporary hybrid retrieval;
- quoted source contract;
- `Quote in chat`/`Ask about this`;
- legal citation labels.

Acceptance:
- upload 2 files + compare với KB regulation;
- quote survives next turn;
- permission revoked → quote/read fails closed;
- attachment không leak qua session/tenant.

### Phase 5 — Legal KG

- canonical entities;
- legal relations;
- graph extraction;
- graph query policy.

Acceptance:
- graph improves relation queries nhưng không làm vector path regress.

### Phase 6 — Evaluation/hardening

- grounded literal validation;
- retrieval benchmark;
- citation correctness;
- security/tenant regression;
- upstream rebase drill.

---

## 24. Evaluation set

Tối thiểu các nhóm:

### Exact document resolution

```text
Tìm Nghị định 13/2023/NĐ-CP
Nghị định 13 năm 2023
Nghị định 13
Thông tư 15 năm 2020
```

### Structural retrieval

```text
Điều 15 quy định gì?
Khoản 2 Điều 15 gồm những gì?
Điểm a khoản 2 Điều 15 có nội dung gì?
```

### Conversation context

```text
Turn 1: Cho tôi nội dung Nghị định ...
Turn 2: Điều 5 của nghị định này thì sao?
```

### Multi-document

```text
So sánh Chương II của văn bản A với Chương III của văn bản B.
Tóm tắt điểm khác nhau giữa hai văn bản.
```

### Multi-intent

```text
Tìm thông tin số điện thoại 0989755968 và đối chiếu quy định
về hồ sơ cấp độ xem vi phạm gì không.
```

### Attachments

```text
[upload A.pdf, B.pdf]
So sánh hai file này.

[upload hồ sơ.pdf]
Kiểm tra hồ sơ này theo Nghị định 13.
```

### Quote

```text
[quote Điều 15 khoản 2]
Quy định này áp dụng trong trường hợp nào?
```

---

## 25. Metrics

Theo dõi:
- document resolution exact accuracy;
- ambiguity precision;
- chunk boundary accuracy;
- structural locator accuracy;
- retrieval Recall@K;
- MRR/MAP nếu dataset hỗ trợ;
- citation precision;
- citation completeness;
- answer groundedness;
- multi-intent recall;
- wrong-fast-path rate;
- tenant authorization regression;
- parse latency;
- OCR latency;
- Agent tool count/turn;
- token usage.

Một refactor không được coi là tốt nếu answer subjective “hay hơn” nhưng:
- citation sai;
- document identity sai;
- cross-tenant scope sai;
- latency tăng không kiểm soát.

---

## 26. Regression invariants

1. Tenant/RBAC luôn được kiểm tra trước khi resolver/tool trả document.
2. `knowledge_id` không được coi là canonical legal identity.
3. Regex identifier không quyết định semantic route.
4. Multi-intent không được collapse về một tool chỉ vì có phone/document number.
5. Quote không bypass permission.
6. Temporary attachment không trở thành permanent knowledge ngoài ý muốn.
7. Legal parser failure phải fallback generic khi phù hợp.
8. OCR không chạy vô ích trên native text page tốt.
9. KG không thay vector retrieval mặc định.
10. Upstream Quick Answer/Smart Reasoning vẫn hoạt động khi toàn bộ VN flags off.

---

## 27. Non-goals

Giai đoạn refactor này không nhằm:
- rewrite WeRAG thành Python;
- port toàn bộ LangGraph v2;
- thay ReAct engine của WeKnora;
- thay toàn bộ vector store;
- bắt buộc Neo4j cho mọi deployment;
- bắt buộc OCR cho mọi PDF;
- tạo một “domain agent” riêng cho từng nghiệp vụ;
- thay tenant/RBAC bằng ACL tự viết;
- đưa mọi temporary attachment vào permanent vector DB;
- làm auto routing bắt buộc ngay từ phase đầu.

---

## 28. Upstream sync policy

Repository fork phải giữ:

```text
upstream = Tencent/WeKnora
origin   = lephuhung/WeRAG
```

Quy tắc:
- domain code mới nằm trong package/file riêng;
- generic upstream files chỉ chỉnh ở registration/integration point;
- không rename hàng loạt upstream packages;
- không sửa semantics của existing public API nếu không cần;
- migration additive;
- feature flags default-safe;
- mỗi phase có commit/PR độc lập;
- trước merge phase mới, test rebase/merge với upstream main gần nhất.

Mục tiêu là phần khác biệt với upstream nhìn giống:

```text
+ Vietnamese legal extension
+ small registration hooks
```

không phải:

```text
forked core runtime rewrite
```

---

## 29. Definition of Done

Refactor foundation được coi là đạt khi:

1. Legal digital PDF và scanned PDF vào cùng canonical structure.
2. `Nghị định 13` được resolve bằng metadata/index, không bằng embedding.
3. `Điều/Khoản/Điểm` được preserve trong chunk metadata/citation.
4. Agent có thể resolve document rồi search/read đúng scope.
5. Multi-intent example của AIRAG không bị route thành people-only.
6. Attachment chat dùng legal parser và có thể đối chiếu với KB document.
7. Quote một source vào turn sau giữ exact source identity.
8. Tenant/RBAC/share vẫn fail closed.
9. Tắt toàn bộ VN feature flags → hành vi gần upstream WeKnora.
10. Có benchmark/regression suite chứng minh chất lượng và không làm hỏng upstream flow.

---

## 30. Quyết định kiến trúc chốt

WeRAG sau refactor phải theo mô hình:

```text
WeKnora platform/runtime
        +
Vietnamese Legal Document Intelligence
        +
AIRAG-derived query/context invariants
        +
domain tools
```

Không theo mô hình:

```text
WeKnora UI
        +
AIRAG backend nguyên khối
```

và cũng không theo:

```text
WeKnora ReAct
        +
LangGraph supervisor thứ hai
```

Boundary này là nguyên tắc chính để giữ hệ thống đơn giản, có thể bảo trì và vẫn theo kịp upstream.
