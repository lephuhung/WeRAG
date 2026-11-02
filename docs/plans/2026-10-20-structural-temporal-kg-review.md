# Review kiến trúc: "Structural and Temporal Legal KG" đối chiếu với WeRAG

Ngày: 20/10/2026 · Nhánh đối chiếu: `feat/document-assistant` (e0d54bae) + `feat/legal-amendment-kg` (plan v3, 2148fc2a) · Trạng thái: **chỉ phân tích — không sửa mã, không migration**.

## 0. Kết luận ngắn

1. Đề xuất **trùng ~70 % với plan v3 đang triển khai** (`2026-10-19-legal-amendment-kg-plan.md`): trích lai regex → LLM → code kiểm chứng, khoảng hiệu lực, trạng thái "chưa xác định", văn bản đích chưa có → yêu cầu bổ sung, Neo4j dựng lại được. Không nên mở một dự án song song; nên **gộp đề xuất vào plan v3 thành v4**. Bước 2 (lớp thực thể) đã merge; bước 3 (bộ giải) **chưa có dòng mã nào** → đây là thời điểm rẻ nhất để đổi mô hình dữ liệu.
2. Năm điểm đề xuất **thêm** so với v3 và đáng nhận: (a) danh tính điều khoản ổn định ở mức nền tảng; (b) sự kiện thay đổi là bản ghi riêng (LegalChange) có nguồn gốc + trạng thái xác minh; (c) phiên bản lời văn (ProvisionVersion) có cache; (d) tách **hiệu lực** khỏi **tình trạng lời văn**; (e) dữ liệu cho giao diện đánh dấu nội dung bị sửa.
3. Ba điểm nên **điều chỉnh**:
   - Cây Điều/Khoản/Điểm **đã có sẵn** trong `chunks.metadata.legal`. Mình đếm được 1,16 triệu `provision_ids` trên 10,8 nghìn văn bản. Không cần parse lại hay dùng LLM. Neo4j chỉ cần chiếu **những điều khoản có liên quan đến thay đổi**, không cần cả cây.
   - Trạng thái trên node chỉ là cache có `as_of`. Nguồn chuẩn là tập sự kiện cộng với bộ giải.
   - **Đường trả lời nên đọc Postgres, không đọc Neo4j.** Các truy vấn cần lúc trả lời ("thay đổi nào chạm điều khoản X", "B tác động gì") là tra chỉ mục 1 bước, không phải duyệt đồ thị. Điểm này **đi ngược quyết định 1 của plan v3** ("KG trong Neo4j, không bảng chuẩn hoá") → cần người dùng chốt (§9, D1).
4. Bổ sung mới chưa có trong cả hai bản: **văn bản hợp nhất (VBHN) làm "đáp án"**. Danh mục dev có 2.459 VBHN, hiện 0 văn bản trong KB. Dùng VBHN để chấm điểm trích xuất, làm nguồn đồng thuận thứ ba và kiểm tra phiên bản dựng ra (§3.5).

## 1. Hiện trạng — cái gì tái sử dụng được

| Thành phần | Ở đâu | Đang làm gì | Dùng lại cho đề xuất | Hạn chế |
|---|---|---|---|---|
| Bộ đọc vbpl HTML | `internal/vbpl/convert.go`, `chunk.go`, `amend.go`, `phrase_amend.go`, `citations.go` | Chunk theo Điều; `provision_ids` (id vbpl theo `dieu`/`khoan:K`/`khoan:K/diem:D`), `subdivision_refs`, `heading_path`, `quoted`, `amends` (34.654 mục / 14.950 chunk), `citations` (khoảng ký tự + id vbpl) | **Nguồn cây cấu trúc** (tất định) + **nguồn trích thứ nhất** + nhãn vàng từ liên kết | `Amend` không có op (chỉ "trích lời văn mới" hoặc "cụm từ"); `Document` là chuỗi như văn bản viết; không lưu khoảng ký tự của từng khoản/điểm trong metadata (bộ chuyển đổi có block map rune offset nhưng không ghi ra); ~25 % chunk vbpl không có `provision_ids` (trang cũ, phụ lục) |
| Bộ phân tích cấu trúc văn bản thường | `internal/vietnamese_legal/structure.go` (`DeriveHeadingPaths`, `DeriveSubdivisionMetadata`) | Phần/Chương/Mục/Điều + Khoản/Điểm cho PDF/OCR (schema v1) | Cây cho 22,2 nghìn chunk không phải vbpl | Không có Phụ lục; không phân biệt lời văn trích dẫn (quoted) → "Điều 5" trong ngoặc kép của B có thể bị hiểu là Điều 5 của B |
| LegalDocContext + bộ trích LLM theo chunk | `vietnamese_legal/entity.go:537`, `service/extract.go:227-456`, `chat_pipeline/legal_graph.go` | 1 lần gọi LLM / chunk → Document/Article/Org/Person/Task; SUA_DOI/THAY_THE/BAI_BO/CAN_CU/PART_OF…; `PostProcessLegalGraph` ép kiểu, đảo cạnh sai chiều, bỏ tự trỏ | Ý tưởng kiểm tra kiểu đầu-cuối + đảo chiều; `ScopeAmendedClause` | Cạnh **không thuộc tính** (không op chi tiết, không ngày, không bằng chứng); khoá node theo **tên** trong **từng KB** (`ENTITY<kb>` + `{name, kb}`) → cùng một văn bản ở 2 KB là 2 node; nhiều nhiễu đã ghi nhận 12/10 |
| Giải bí danh văn bản | `service/legal_graph_alias.go`, `ResolveLegalDocumentName` (tra theo ngày), `FindCitations`, `CatalogNumberKey` | Tên/số hiệu → id danh mục | **Lớp Entity Resolution** của đề xuất — dùng nguyên | Đã xong ở bước 2 plan v3 |
| Neo4j | `repository/retriever/neo4j/repository.go` | `apoc.merge.node`, scope theo KB, xoá theo `knowledge_ids` | Kết nối, APOC, sao lưu (timer) | **Không có index/constraint**; tìm node bằng `toLower(name) CONTAINS`, 1 bước, vô hướng |
| Danh mục pháp lý | `legal_documents` (47.316), `legal_document_relations` (131 nghìn; amends 5.916, repeals 7.104, replaces 6.658, suspends 82, consolidates 4.135), `legal_document_links`, `legal_document_requests` | Hiệu lực cấp văn bản, ngày ban hành/hiệu lực/hết hiệu lực (effective_on 45,2 nghìn, expired_on 17,2 nghìn), duyệt SuperAdmin | **Tầng Document** của đề xuất + nguồn ngày + đối chiếu lệch | Chỉ cấp văn bản; `status` có thể lỗi thời |
| Lời văn hiện hành / lịch sử | `legal_phrase_amendments.go` (`LegalCurrentWording`), `legal_amendment_history.go`, `search_knowledge_legal.go` (`applyCurrentWording`, `expandLegalRelations`), `agent/legal_history.go` | Chỉ **thay cụm từ** đổi lời văn mô hình thấy; sửa cả khoản chỉ hiện ở khối "Lịch sử sửa đổi" sau câu trả lời; ghép A bằng **chuỗi số hiệu `!=`**; ngày = `eff_from` hoặc lùi về ngày ban hành | Các **điểm móc** (model_content, khối lịch sử, quick-answer bus, finalize) — bộ giải mới cắm vào đúng các chỗ này | Logic giải nằm rải 3 file, không có khái niệm "phiên bản tại ngày D"; lùi về ngày ban hành là sai pháp lý |

## 2. Quan hệ với plan v3

| Đề xuất | Plan v3 | Đánh giá |
|---|---|---|
| Hybrid extraction regex → LLM → resolution | 4.1–4.5, đã đo: Qwen F1 0,81; Qwen∩bộ đọc P 0,99 (phủ 41 %) | Trùng; đã có số liệu |
| Entity Resolution Engine | 4.2 + bước 2 đã merge (`FindCitations`, tra tên theo ngày, khoá danh mục 000175) | Đã có |
| Quan hệ cấp Điều/Khoản/Điểm | `LEGAL_Provision` + cạnh `AMENDS` | Trùng |
| LegalChange (node sự kiện) | cạnh `AMENDS` có thuộc tính + `edge_key` | **Khác** — xem 3.4 |
| ProvisionVersion | bộ giải dựng lúc trả lời, không lưu | **Khác** — xem 3.5 |
| Trạng thái hiệu lực theo node, có thời gian | 4.12 một enum trạng thái theo câu trả lời | **Khác** — nên tách trục (3.2) |
| Resolver theo thời gian, "chưa xác định" khi thiếu căn cứ | 4.7, 4.11, 4.12 | Trùng; v3 chi tiết hơn (cổng câu trả lời) |
| Postgres = nguồn chuẩn, Neo4j = duyệt | v3: `amend_ops` jsonb trên chunk B + bảng duyệt; Neo4j bản chiếu | Gần nhau, nhưng v3 không có bảng sự kiện → xem D1 |
| Đích chưa import → unresolved, tự hoàn thiện sau | 4.8 tạo yêu cầu bổ sung | v3 thiếu bước **tự giải lại** khi văn bản đến |
| Giao diện đánh dấu | không có | Mới |

## 3. Đánh giá từng phần

### 3.1 Cây Document–Article–Clause–Point

**Khả thi, không cần parser mới.** Cây đã được tạo tất định lúc ingest:
- vbpl: `provision_ids` là map `dieu`, `khoan:1`, `khoan:2/diem:a` → id vbpl. Có trên 204,9 nghìn / 273,1 nghìn chunk, tổng **1,16 triệu đơn vị**. `subdivision_refs` cho 1,03 triệu tham chiếu khoản/điểm.
- PDF/OCR: `article_nos`/`khoan_nos`/`subdivision_refs` (schema v1).
- Chương/Mục: `heading_path`. Nên giữ dạng **thuộc tính**, không làm node. Không có thay đổi pháp lý nào nhắm vào "Chương" mà không nêu Điều, trừ "bãi bỏ Chương II". Ca đó bung thành các Điều con lúc giải.

**Định danh** (đề xuất đúng khi nói không dùng số Điều làm khoá):
- `doc_key` = id danh mục (id vbpl) nếu có, ngược lại `kb:<knowledge_id>` (văn bản tổ chức tự tải lên).
- `provision_key` = `doc_key` + `#` + **đường dẫn chuẩn hoá theo nhãn**, ví dụ `#dieu:5/khoan:2/diem:a`, `#phuluc:I/muc:2`. Nhãn chuẩn hoá NFC, `đ` giữ nguyên trong thứ tự điểm, "Điều 5a" → `dieu:5a`.
- Id vbpl (`provision_ids`) chỉ là **thuộc tính** (dùng cho liên kết `?go=`), không làm khoá, vì:
  - văn bản sửa đổi tham chiếu đích **bằng nhãn**, không bằng id vbpl;
  - văn bản OCR không có id;
  - khoá theo đường dẫn **tạo được trước khi có văn bản A** (điều khoản "chờ"). Ngay chỗ này giải được bài toán "văn bản sửa đổi import trước văn bản gốc" (3.8).
- Nhãn thay đổi theo thời gian: "bổ sung Điều 5a", "đánh số lại". Khoá theo nhãn **tại phiên bản gốc**. Điều khoản bổ sung có khoá riêng sinh từ sự kiện `bo_sung`. Đánh số lại là một op (`danh_so_lai`) ánh xạ khoá cũ → nhãn hiển thị mới. Khoá cũ không đổi.
- **Một văn bản ở nhiều KB = một bộ điều khoản** ở mức nền tảng. Mỗi bản sao trong KB ánh xạ về qua `knowledges.metadata.external_id`/`source_doc_id` + khớp `doc_number` (plan 4.13). Đây là chỗ đề xuất khác hẳn graph hiện tại (graph hiện tại theo KB).

**Không nên** giao cho LLM "hỗ trợ cấu trúc bất thường". Cấu trúc không đọc được bằng code thì điều khoản đó **không có khoá tin cậy** → mọi thay đổi chạm vào nó ở mức 2 (chưa xác minh) và vào hàng duyệt. LLM dựng cấu trúc là nguồn lỗi không kiểm chứng được. Quyết định 19/10 đã chốt: cây lấy từ metadata, không từ Gemma.

**Thiếu dữ liệu cần bổ sung:** khoảng ký tự `[start,end)` của từng khoản/điểm trong chunk. Bộ chuyển đổi đã có block map rune offset, chỉ chưa ghi vào metadata. Bổ sung bằng **backfill metadata** giống `cmd/vbpl-phrase-backfill`. Không phải embed lại. Không có khoảng này thì không cắt được lời văn một khoản để dựng phiên bản hoặc đánh dấu giao diện.

### 3.2 Trạng thái hiệu lực theo node

Đồng ý với đề xuất: phải tách hiệu lực khỏi việc bị sửa đổi. Plan v3 (4.12) đang gộp vào **một** enum (`CURRENT_VERIFIED`, `PARTIALLY_EXPIRED`, `CONDITIONALLY_NOT_APPLICABLE`…). Thực tế có thể đồng thời "có hiệu lực + lời văn đã sửa chưa xác minh + không áp dụng cho nhóm X". Đề nghị bộ giải trả **ba trục**:

| Trục | Giá trị |
|---|---|
| `validity` | `IN_FORCE`, `NOT_YET_EFFECTIVE`, `PARTIALLY_EXPIRED` (chỉ Document/Article/Clause — do con bị bãi bỏ/hết hiệu lực), `EXPIRED`, `SUSPENDED` (có `until`), `UNKNOWN` |
| `text` | `ORIGINAL`, `AMENDED_VERIFIED`, `AMENDED_UNVERIFIED`, `CONFLICT`, `TEXT_MISSING`, `UNRESOLVED` |
| `applicability` | `GENERAL`, `CONDITIONAL{điều kiện, match/no_match/unknown}`, `TRANSITIONAL{điều kiện}` |

- Cổng câu trả lời (4.11) xét tổ hợp: chỉ cho giá trị khẳng định khi `validity=IN_FORCE` ∧ `text∈{ORIGINAL, AMENDED_VERIFIED}` ∧ `applicability≠CONDITIONAL{match|unknown}`.
- **Tổng hợp lên cha:**
  - Cha `PARTIALLY_EXPIRED` khi có con hết hiệu lực/bị bãi bỏ và cha không bị tác động trực tiếp.
  - Cha **không bao giờ** tự `EXPIRED` vì mọi con đều hết. Ca đó gắn cờ để duyệt.
  - Hiệu lực con kế thừa từ cha khi con không có sự kiện riêng: Document hết hiệu lực → mọi điều khoản hết.
- Đặt trạng thái theo **thời điểm**: bộ giải nhận `as_of`. Cache trên node (Neo4j hoặc bảng) phải có `as_of` + `change_set_hash`. Cache tự hết hạn khi tới ngày hiệu lực kế tiếp của một sự kiện (`next_transition_at`). Thiếu trường này, trạng thái "hiện hành" sẽ sai vào đúng ngày văn bản sửa đổi có hiệu lực.
- Danh mục (Document) là nguồn **đã duyệt**. Khi sự kiện trích ra mâu thuẫn với danh mục (danh mục "Còn hiệu lực" nhưng có `bai_bo_van_ban` từ văn bản đã có hiệu lực), **không tự sửa danh mục**. Đưa vào danh sách lệch cho SuperAdmin (quy tắc 17/10: tự động chỉ được gắn cờ, không được quyết). Trong lúc chờ duyệt, bộ giải trả `validity=UNKNOWN` cho phần mâu thuẫn.

### 3.3 Trích xuất quan hệ cấp Điều/Khoản/Điểm

Phương pháp của đề xuất đúng và đã được đo (plan v3 §2.2). Những điểm cần thêm hoặc nhấn mạnh:

- **Đơn vị nguồn:** cạnh xuất phát từ **khoản/điểm của B chứa câu dẫn**. Đơn vị này lấy từ `provision_ids`/khoảng ký tự của câu dẫn, không hỏi LLM. Ví dụ "Khoản 1 Điều 1 của B" là tất định.
- **Ngữ cảnh kế thừa:** mẫu "Điều 1. Sửa đổi, bổ sung một số điều của Nghị định X" → "1. Khoản 2 Điều 5 được sửa đổi như sau". Văn bản đích nằm ở tiêu đề Điều, và `heading_path` có sẵn. Plan v3 đưa danh sách **đóng** ứng viên (từ đoạn, tiêu đề cha, tên B, tra tên) cho Qwen chọn. Giữ nguyên cách này; nó chống bịa số hiệu tốt nhất.
- **Lời văn trích dẫn phải bị loại khỏi cấu trúc của B.** vbpl có cờ `quoted`; văn bản OCR thì không, nên văn bản OCR chỉ ở mức 2.
- **Một khoản của B sửa nhiều đích**, và "các văn bản sau: 1…; 2…": mỗi đích một sự kiện (plan 4.3 đã có).
- **Chuỗi sửa văn bản sửa đổi** (B2 sửa khoản 3 Điều 1 của B, mà khoản đó lại là lệnh sửa A): sự kiện của B2 nhắm vào điều khoản của B. Bộ giải lần ngược về A. Giai đoạn đầu luôn để mức 2.
- **Gate cuối luôn là code** (plan 4.5): đích tồn tại; `from` có nguyên văn trong phiên bản ngay trước; nhãn đầu khối lời văn mới khớp đích; động từ khớp op; "văn bản này/Nghị định này" → không phải tác động lên A.

### 3.4 LegalChange

**Nên nhận**, dưới dạng **bản ghi Postgres là gốc** + **node phản ánh trong Neo4j**:
- Lý do bảng thay vì `amend_ops` jsonb trên chunk B (v3 4.4):
  - (1) Reparse B soft-delete chunk cũ, nên jsonb đi theo chunk mất id và mất trạng thái duyệt/giải. Plan v3 phải vá bằng `edge_key` + bảng duyệt riêng.
  - (2) Truy vấn nóng nhất là "**theo đích**" (điều khoản A bị gì). jsonb nằm trên chunk **B** chỉ tra được bằng GIN `@>` + so chuỗi. Đây đúng là lỗi ghép chuỗi `!=` hiện nay.
  - (3) Tự giải lại khi A được import cần `UPDATE … WHERE target_number_key = ?`.
  - (4) Thống kê độ chính xác theo đường × op cho cổng 6.2.
- Kết quả trích thô theo từng nguồn (bộ đọc, Qwen, VBHN) vẫn có thể giữ ở metadata chunk làm **nguồn gốc**. Bảng `legal_changes` là bản đã gộp, đã giải, đã xếp mức.
- Trong Neo4j: `(:LEGAL_Provision)-[:ISSUES]->(:LEGAL_Change)-[:AFFECTS]->(:LEGAL_Provision|:LEGAL_Doc)` (đã "reify" thành node), cộng thêm cạnh tắt `AMENDS` nếu cần duyệt nhanh. Node sự kiện cho phép một sự kiện → nhiều đích, gắn bằng chứng, gắn quyết định duyệt mà cạnh không làm được. Khối lượng nhỏ: hiện có 34,6 nghìn mục từ bộ đọc; ước tính ~50–80 nghìn sự kiện cho 10,8 nghìn văn bản trung ương.

Phác thảo cột (chưa phải migration):
- `id`, `edge_key UNIQUE` (băm: B, đích, op, phạm vi, from/to, khoảng câu dẫn)
- `op`
- `source_doc_key`, `source_provision_key`
- `target_doc_key NULL`, `target_ref_raw`, `target_number_key`, `target_path`, `target_provision_key NULL`
- `new_label`, `after_label`, `phrase_from`, `phrase_to`, `scope_condition`
- `quoted_span{chunk_id,start,end,text_hash}`
- `eff_from`, `eff_to`, `eff_basis{catalogue|own_clause|unknown}`
- `resolution{resolved|unresolved_target|ambiguous|invalid}`, `tier`, `sources[]`, `extractor jsonb`
- `review_status`, `reviewed_by/at`
- `evidence{chunk_id,start,end,sentence_hash}`
- `superseded_by` (khi bằng chứng đổi)

### 3.5 ProvisionVersion

**Nên nhận, theo kiểu lười (lazy) như đề xuất gợi ý**:
- Phiên bản gốc = lời văn trong chunk A (đã lưu, không chép).
- Phiên bản sau thay đổi được **dựng bằng phép ghép tất định**:
  - `sua_doi`: thay toàn bộ đơn vị bằng khoảng trích của B;
  - `thay_cum_tu`: thay **chỉ khi** `from` xuất hiện đúng phạm vi và số lần khớp rõ;
  - `bo_sung`: chèn sau `after_label`;
  - `bai_bo`: đánh dấu.
- Ghép không được → `TEXT_MISSING`/`UNRESOLVED`. **Không bao giờ** để LLM viết lời văn hợp nhất.
- Cache ở `legal_provision_versions(provision_key, valid_from, valid_to, text, text_hash, change_ids[], change_set_hash, verification, computed_at)`. Sự kiện đổi → xoá cache theo `provision_key`.
- Không dựng trước cho điều khoản chưa từng bị chạm.

**Văn bản hợp nhất (VBHN) — đề nghị bổ sung:**
- Danh mục dev có **2.459** văn bản loại "Văn bản hợp nhất" và 4.135 quan hệ `consolidates`. Hiện chưa văn bản nào nằm trong KB.
- VBHN là lời văn đã hợp nhất chính thức tại một ngày. Chú thích cuối trang thường ghi "Khoản này được sửa đổi theo quy định tại khoản 2 Điều 1 của Nghị định số …, có hiệu lực kể từ ngày …". **Cần kiểm chứng định dạng chú thích trên HTML vbpl trước khi tin.**
- Ba công dụng:
  - (1) **nhãn vàng quy mô lớn** cho đánh giá trích xuất, thay cho việc dán nhãn tay 300 đoạn;
  - (2) **nguồn đồng thuận thứ ba** cho mức 1;
  - (3) **kiểm tra phiên bản dựng ra**: bộ giải dựng A tại ngày của VBHN rồi so với lời văn VBHN. Lệch → không lên mức 1.
- Không đưa VBHN vào KB tìm kiếm khi chưa quyết. Nếu đưa, lời văn bị trùng với A và B.

### 3.6 Bộ giải hiệu lực theo thời gian (EffectiveProvisionResolver)

Plan v3 4.7 đã đúng hướng: không lấy "mới nhất"; ngày không rõ → `UNRESOLVED`; B hết hiệu lực không làm sống lại lời văn cũ; cùng ngày cùng phạm vi → `CONFLICT`. Bổ sung:

- **Ngày hiệu lực riêng của từng khoản B**: điều khoản thi hành của B ("khoản 3 Điều 1 có hiệu lực từ …"). Phải trích thành `eff_basis=own_clause`. Ngày hiệu lực của cả văn bản chỉ là mặc định.
- **Bỏ ngay fallback về ngày ban hành** trong `phraseEffect` (`legal_phrase_amendments.go:167`). Hiện code đang dùng ngày ban hành khi thiếu `eff_from`.
- **Thời điểm áp dụng ≠ thời điểm hỏi**: xử phạt theo thời điểm hành vi, thuế theo kỳ tính thuế. Bộ tách ngày tất định lấy ngày trong câu hỏi; không chắc → hôm nay + câu do code ghi rõ.
- Bộ giải là **hàm thuần**: `Resolve(provision_key, as_of, changes[], catalogue) → {validity, text, applicability, version, evidence[]}`. Đặt trong `internal/legalamend` (mới), không phụ thuộc Neo4j, test bằng bảng ca. Ba chỗ đang tự giải (`LegalCurrentWording`, `LegalAmendmentHistory`, `fullyExpiredDocs`/`legalNote`) chuyển sang gọi hàm này.

### 3.7 Truy xuất phục vụ RAG

Không thay Hybrid Search. Mở rộng tại các điểm móc **đã có**:
- **Ngược** (tìm được điều khoản A): `search_knowledge` → `applyCurrentWording` (`search_knowledge_legal.go:41`) → gọi bộ giải cho các `provision_key` của chunk → `model_content` mang lời văn hiện hành + trạng thái + nguồn.
- **Xuôi** (tìm được điều khoản B là lệnh sửa): gắn cho mô hình "đây là lệnh sửa khoản 2 Điều 5 của A; lời văn hiện hành của A là …". Như vậy mô hình không coi lời văn trích của B là quy định độc lập, và cũng không đọc nhầm sang lời văn cũ của A.
- `expandLegalRelations` (cấp văn bản) giữ nguyên, thêm nhánh cấp điều khoản: chỉ kéo **đúng chunk** chứa đích, giới hạn số dòng.
- Kiểm tra quyền trên **chunk B** trước khi chèn lời văn (plan 4.13, quyết định 10.2).
- Cổng câu trả lời (plan 4.11) dùng chung kết quả bộ giải.
- **Truy vấn đường trả lời đi Postgres** (`legal_changes` có index `(target_doc_key, target_path)` và `(source_doc_key)`), mục tiêu p95 < 50 ms cho ≤ 30 điều khoản. Neo4j dùng cho tab đồ thị, khám phá nhiều bước (chuỗi sửa, hướng dẫn) và công cụ `query_knowledge_graph`. Neo4j sập/xoá (đã xảy ra 08/10) thì trả lời vẫn đúng.

### 3.8 Lưu trữ, văn bản chưa import, idempotent

- **Postgres (nguồn chuẩn):**
  - `legal_provisions`: sổ đăng ký điều khoản dựng từ metadata chunk của **bản sao chuẩn** (KB công khai) — ~1,16 triệu dòng cho trung ương. Thêm điều khoản "chờ" cho đích chưa có văn bản.
  - `legal_changes`
  - `legal_provision_versions` (cache)
  - `legal_text_spans` tuỳ chọn (3.9)
- **Neo4j (bản chiếu, `build_id`, dựng lại được):**
  - `LEGAL_Doc` cho mọi văn bản danh mục.
  - `LEGAL_Provision` **chỉ cho điều khoản là nguồn/đích của sự kiện + tổ tiên của chúng**. Ước tính vài chục nghìn thay vì 1,16 triệu. Chiếu cả cây chỉ khi có nhu cầu duyệt cây trong đồ thị.
  - `LEGAL_Change` dạng node.
  - Cạnh `CATALOG{relation}` chép từ danh mục.
  - **Bắt buộc tạo constraint** `UNIQUE key` cho các nhãn này. Repo hiện không tạo index nào.
- **Văn bản đích chưa có:**
  - Sự kiện lưu với `target_doc_key NULL`, `target_number_key`, `resolution=unresolved_target`.
  - Tạo `legal_document_requests` với requester `amendment_graph` (plan 4.8).
  - Khi danh mục import hoặc một liên kết KB được duyệt: job **giải lại** `WHERE target_number_key = ANY(...)`.
  - Đích có trong danh mục nhưng chưa có lời văn → điều khoản "chờ" theo đường dẫn, không kiểm chứng được `from` → mức 2.
- **Idempotent:**
  - `edge_key` UNIQUE + `ON CONFLICT DO UPDATE`; bằng chứng đổi (băm khác) → bản ghi mới, bản cũ `superseded_by`, quyết định duyệt cũ không tự gắn sang.
  - Neo4j dựng theo `build_id` rồi chuyển con trỏ bản hoạt động (plan 4.6).
  - Đổi `vbpl_converter_version` → dựng lại `legal_provisions` + khoảng ký tự cho văn bản đó.

### 3.9 Giao diện đánh dấu nội dung bị thay đổi

Dữ liệu ở trên đủ để làm sau:
- Mở văn bản A → lấy sự kiện theo `target_doc_key` → tô các `provision_key` bị tác động. Tooltip ghi văn bản B, ngày, lời văn trước/sau (từ `legal_provision_versions`).
- Thay cụm từ → `legal_text_spans(provision_key, version_hash, start, end)` trên lời văn **NFC đã chuẩn hoá của đúng phiên bản bộ chuyển đổi**. Không tạo node cho từng cụm từ, đồng ý với đề xuất.
- Liên kết sang vbpl: `?go=<provision_id>`.
- Màn hình: trang xem tài liệu trong `frontend-next` (en + vi). Đây là một bước riêng, sau khi bộ giải ổn định.

## 4. Tương thích với graph đã tạo

- Lớp mới dùng nhãn `LEGAL_*` mức nền tảng, **tách khỏi** `ENTITY<kb>` hiện tại. Graph cũ không bị đụng, không cần re-extract để chạy lớp mới.
- Khối lượng dựng lại thấp. Lớp mới không cần embed lại chunk; chỉ cần backfill metadata (khoảng ký tự khoản/điểm) và nạp bảng từ `chunks.metadata.legal.amends` hiện có.
- Sau khi lớp mới chạy ổn: tắt các loại cạnh SUA_DOI/THAY_THE/BAI_BO trong bộ trích LLM theo chunk **đối với văn bản đã có trong `legal_changes`**, vì chúng trùng và nhiễu. Giữ CAN_CU/CHU_TRI… Cần đánh giá tác động lên `query_knowledge_graph` và `RankGraphRelations` (đang đẩy các cạnh này lên đầu).
- KB cũ không có `provision_ids` (schema v1, OCR) → khoá `kb:<id>#…`, mức 2. Không chặn.

## 5. Quy mô

| | Trung ương hiện tại (dev) | Toàn vbpl (~171 nghìn văn bản, gồm ~116 nghìn địa phương) — ước tính |
|---|---|---|
| Văn bản có lời văn | 10,8 nghìn | ~100–170 nghìn |
| Điều khoản (Điều+Khoản+Điểm) | 1,16 triệu | ~8–15 triệu (văn bản địa phương ngắn hơn) |
| Đoạn ứng viên cho LLM | 16,8 nghìn (6 % chunk) | ~100–200 nghìn |
| Thời gian trích (Qwen 1 luồng, đêm) | 2–5 đêm | nhiều tuần → cần >1 luồng hoặc mô hình nhỏ (plan §8), hoặc chỉ chạy văn bản có quan hệ `amends/repeals/suspends` trong danh mục |

- Postgres chịu được 15 triệu dòng `legal_provisions` với index btree `(doc_key, path)`.
- Neo4j nhẹ vì chỉ chiếu điều khoản liên quan.
- Chi phí lúc trả lời không đổi theo quy mô, vì là tra theo khoá của các chunk đã truy xuất.

## 6. Rủi ro và cách kiểm soát

| Rủi ro | Kiểm soát |
|---|---|
| LLM chọn sai đích | Danh sách đóng; đích phải tồn tại; `from` nguyên văn trong phiên bản ngay trước; nhãn đầu khối khớp; mức 1 cần đồng thuận + cổng Wilson ≥ 0,97 theo đường × op; VBHN đối chiếu |
| Đảo chiều quan hệ | Nguồn luôn là văn bản chứa câu dẫn (theo cấu trúc, không theo LLM); đích phải ban hành trước B (trừ đính chính cùng ngày); "văn bản này" → không phải tác động |
| Nhiều văn bản sửa cùng điều khoản | Xếp theo `eff_from`, rồi ngày ban hành; kiểm `from` trên phiên bản ngay trước chỉ khi mọi sự kiện trước đã giải; cùng ngày cùng phạm vi khác nội dung → `CONFLICT` |
| Sửa một phần (cụm từ) bị hiểu là thay cả khoản | Op tách bạch; ghép chỉ trên khoảng; `from` khớp nhiều lần mà không có chỉ dẫn → `UNRESOLVED` |
| Điều khoản chuyển tiếp | Trục `applicability=TRANSITIONAL` + điều kiện; không bao giờ tự quyết áp dụng; cổng câu trả lời bắt nêu điều kiện |
| Trạng thái danh mục mâu thuẫn với sự kiện | Danh sách lệch cho SuperAdmin; không tự sửa hàng đã duyệt; `validity=UNKNOWN` trong lúc chờ |
| Lời văn trích trong B bị đọc thành cấu trúc của B | Cờ `quoted` (vbpl); OCR → mức 2 |
| Số hiệu trùng (≈70 % văn bản địa phương trùng số hiệu) | Khoá bằng id danh mục, không bằng số hiệu; tra tên theo ngày; mơ hồ → hàng duyệt |
| Bộ chuyển đổi đổi phiên bản làm lệch khoảng ký tự | Khoảng gắn `converter_version` + `text_hash`; lệch → dựng lại, không dùng khoảng cũ |
| Neo4j mất dữ liệu | Không nằm trên đường trả lời; dựng lại từ Postgres |
| Tải vLLM (sự cố 18–19/10) | Khung đêm, 1 luồng, ngắt mạch (plan 4.9) |

## 7. Lộ trình đề xuất (thay mục 5 của plan v3 từ bước 3)

| # | Việc | Ghi chú |
|---|---|---|
| 3a | Sơ đồ khoá + `legal_provisions` (Postgres) + backfill khoảng ký tự khoản/điểm | migration; không embed lại |
| 3b | `legal_changes` + nạp từ 34,6 nghìn mục `amends` của bộ đọc (thêm op suy từ dạng câu) + gộp `legal_amendment_reviews` vào | thay `amend_ops` jsonb |
| 3c | Bộ giải thuần (3 trục, `as_of`, phiên bản lười + cache) + chuyển `LegalCurrentWording`/`LegalAmendmentHistory` sang gọi nó; bỏ fallback ngày ban hành | công tắc `legal.current_wording.resolver` |
| 4 | Cổng câu trả lời (chỉ log → chặn theo 10.3) | như v3 |
| 5 | Yêu cầu bổ sung + requester + **job giải lại** | như v3 4.8 + mới |
| 6 | Thử nghiệm VBHN: kiểm định dạng chú thích trên 50 VBHN → nếu dùng được, tạo bộ vàng lớn | quyết định D4 |
| 7 | Trích Qwen v2 → cổng 6.2 (bộ khoá kín + VBHN) | như v3 bước 8 |
| 8 | Chạy đêm toàn kho + hook sau ingest | như v3 bước 9 |
| 9 | Chiếu Neo4j (`LEGAL_*`, node `LEGAL_Change`, constraint, `build_id`) + tab đồ thị | đổi chỗ: sau bộ giải |
| 10 | Trang duyệt SuperAdmin + danh sách lệch | như v3 bước 7 |
| 11 | Giao diện tô điều khoản bị sửa | mới |
| 12 | Tắt cạnh sửa đổi trong bộ trích LLM theo chunk cho văn bản đã có sự kiện | mới, đo tác động |

## 8. Không làm

- LLM dựng cây cấu trúc hay viết lời văn hợp nhất.
- Lưu trạng thái hiệu lực trên node như nguồn chuẩn.
- Chiếu cả 1,16 triệu (rồi 15 triệu) điều khoản vào Neo4j khi chưa có nhu cầu duyệt cây.
- Tự sửa danh mục đã duyệt theo kết quả trích.

## 9. Cần người dùng quyết định

- **D1 — Nguồn chuẩn:** bảng Postgres `legal_provisions` + `legal_changes` + cache phiên bản, Neo4j chỉ là bản chiếu ngoài đường trả lời (**khuyến nghị**). Phương án kia là giữ quyết định 1 của v3 (Neo4j + `amend_ops` jsonb + bảng duyệt). Đề xuất gốc cũng nghiêng về Postgres.
- **D2 — Neo4j chiếu cả cây hay chỉ điều khoản liên quan:** khuyến nghị chỉ điều khoản liên quan + tổ tiên.
- **D3 — Sự kiện dạng node `LEGAL_Change` (khuyến nghị)** hay cạnh `AMENDS` có thuộc tính.
- **D4 — VBHN:** cho phép crawl 2.459 VBHN làm đáp án/nguồn thứ ba (không đưa vào KB tìm kiếm)?
- **D5 — Trạng thái 3 trục** thay cho enum đơn ở plan v3 4.12?
- **D6 — Phạm vi văn bản địa phương:** có đưa vào lộ trình trích xuất không? Quyết định này ảnh hưởng thời gian chạy (nhiều tuần) và nhu cầu mô hình nhỏ.

## 10. Quyết định và phát hiện (20/10, sau review)

- **D1 chốt: Postgres là nguồn chuẩn và đường trả lời đọc Postgres.** Neo4j làm sau, chỉ khi cần (D2, D3 hoãn theo).
- **D4/D6: làm trước để có dữ liệu test.** Kết quả khảo sát đầu tiên:
  - **Quan hệ cấp điều khoản do biên tập viên vbpl gắn sẵn trong HTML đã crawl.**
    - Mỗi `<p class="prov-article|prov-clause|prov-item">` của văn bản A có thể mang `type="<mã>:<id điều khoản nguồn trong B>"`.
    - Mã quan hệ: 10 sửa đổi bổ sung, 1 bãi bỏ, 13 bổ sung, 12 thay thế, 14 giải thích, 6 đính chính, 11 tạm ngưng, 5 đình chỉ.
    - Phủ **19.195 thẻ trên 1.765/10.991 văn bản** (năm 2010–2026). 96 % id nguồn khớp một điều khoản trong HTML B đã crawl.
    - Bộ chuyển đổi `internal/vbpl` hiện bỏ thuộc tính này.
    - Server action "provision-target" (id `2836b8008e17a31b85409db6fcd8515b307b565d`, body `[docId, provisionId]`, trang `/van-ban/chi-tiet/{docId}`) trả `sourceProvisions[{id, title "Khoản 1, Điều 1", content, effFrom, documentId, type}]` + `targetProvision`. Như vậy có cả **ngày hiệu lực theo từng khoản nguồn**. Ví dụ NĐ 68/2026 Điều 3 ← khoản 1 Điều 1 NĐ 141/2026, hiệu lực 01/01/2026.
    - Đây là nguồn do người biên tập (không phải LLM) → ứng viên nhãn vàng + nguồn đồng thuận mạnh. Cần đo độ đầy đủ so với 34,6 nghìn mục của bộ đọc.
  - **VBHN:** mẫu 74 văn bản (2024–2026), 43 trang HTML rỗng (nội dung ở tệp đính kèm `_content.html`). 23/31 trang có HTML chứa chú thích dạng "Khoản này được sửa đổi, bổ sung theo quy định tại khoản 1 Điều 1 của Nghị định số …, có hiệu lực kể từ ngày …". Dữ liệu ở `/home/orca/data/vbpl-vbhn`.
  - **Địa phương:** bộ thử 146 văn bản Hà Tĩnh/Nghệ An 2026 (`/home/orca/data/vbpl-local-test`): 143 có markup prov-*, 59 có câu dạng sửa đổi/bãi bỏ, **0 thẻ quan hệ điều khoản** → văn bản địa phương chỉ có nguồn bộ đọc + LLM.

## 11. Bộ dữ liệu kiểm tra (20/10) — kết quả đo

Script: `scripts/legal_amend_kg/gold/` (build_gold, fetch_prov_targets, fetch_vbhn, parse_vbhn, agree_sources, build_testset, score_locked). Dữ liệu: `/home/orca/data/legal-amend-gold`, `/home/orca/data/vbpl-vbhn`. Không có văn bản địa phương.

**Nguồn tham chiếu:**
- Thẻ biên tập viên vbpl: 19.057 cạnh.
  - Kiểm tay 14/14 đúng.
  - **Không đầy đủ**: bổ sung điều khoản mới không có chỗ gắn thẻ; nhiều cặp B→A gắn thiếu.
- Chú thích VBHN:
  - 1.422 VBHN tải về, chỉ 132 có chú thích chuẩn (phần lớn chỉ có PDF/Word) → 3.649 cạnh.
- Hai nguồn so với nhau, chỉ tính chỗ cả hai cùng phủ (cùng văn bản gốc, cùng văn bản sửa đổi):
  - trùng đích 524 (291 trùng cả khoản nguồn), trùng nhánh 103, chỉ VBHN có 155 → **~80 % khớp**.
  - 1.238 cạnh VBHN nằm ngoài vùng biên tập viên gắn thẻ.
- **Ngày hiệu lực trong chú thích VBHN trùng `effective_on` danh mục của văn bản sửa đổi ở 98,7 %** (2.549/2.583), khác 34, danh mục thiếu 20.
  - → Ngày hiệu lực riêng của từng khoản là ngoại lệ hiếm; ngày cả văn bản là mặc định đúng.

**Bộ test khoá kín** `data/locked-2026-10-20.jsonl`:
- 287 đoạn B, 988 nhãn tham chiếu.
- Loại 296 văn bản B đã dùng ở bộ trước.

**Kết quả** (đích = id danh mục; tên luật không số được tra theo ngày):

| | Qwen3.6 (1 luồng, rút gọn, không schema) | Bộ đọc vbpl |
|---|---|---|
| Phủ: đúng điều khoản / tính cả đúng nhánh | **0,78 / 0,85** | 0,30 / 0,37 |
| sửa đổi, bổ sung | 0,86 / 0,89 | 0,65 / 0,73 |
| thay thế (gồm thay cụm từ) | 0,79 / 0,87 | 0,29 / 0,36 |
| bãi bỏ | **0,94** / 0,95 | 0,02 / 0,02 |
| bổ sung (đích mới, thẻ gắn ở điều khoản kề) | 0,34 / 0,60 | 0,02 / 0,21 |
| Dự đoán được nguồn tham chiếu xác nhận | 952/1.259 (0,76) | 381/542 (0,70) |

- Kiểm tay 30 dự đoán Qwen "chưa xác nhận": 25 đúng, 2 sai, 3 chưa chắc.
  - 2 sai: câu thuật lại việc bãi bỏ trước đây ("đã bị bãi bỏ bởi…"); đoạn nêu hai văn bản → gán nhầm văn bản.
  - → Độ chính xác ước tính của Qwen **~0,9**, còn dưới cổng 0,97 của mức 1.
- Qwen: 2/287 lỗi parse, trung vị 3,3 s/đoạn.
- Lỗi chuẩn hoá đích phát hiện khi chấm: số hiệu dạng "17-2023-NĐ-CP" (lấy từ tiêu đề KB); 16,5 % đích chỉ có tên luật. Đã có `FindCitations` + tra tên theo ngày ở bước 2.

## 12. Kiểm kê markup HTML vbpl (20/10)

Toàn bộ thống kê: `/home/orca/data/legal-amend-gold/html_inventory.txt` (script `gold/html_inventory.py`).
- Văn bản trung ương: 10.991 trang, 58 rỗng.
- VBHN: 1.410 trang, 1.085 rỗng.

| Dấu hiệu | Ý nghĩa | Phủ | Bộ chuyển đổi hiện nay | Đề xuất |
|---|---|---|---|---|
| `p.prov-part/chapter/section/subsection/article/clause/item` (+ `div/td.prov-*`) | Cấu trúc Phần→Điểm | article 10.761 trang; clause 9.286; item 6.909 | **Dùng** | Giữ |
| `p@id` (uuid) | Id điều khoản ổn định của vbpl (deep link `?go=`) | 1,14 triệu, 10.858 trang | **Dùng** (`provision_ids`) | Giữ; là khoá nối với thẻ quan hệ |
| `p@id` dạng `p12_r3`, `khoan_#`, `dieu_#` | Id kiểu cũ / bản chép từ trang khác | 57 nghìn | Không | Bỏ (không ổn định) |
| **`p@type="<mã>:<id khoản nguồn>"`** | Quan hệ cấp điều khoản do biên tập viên gắn trên văn bản **đích** (10 sửa đổi bổ sung, 1 bãi bỏ, 13 bổ sung, 12 thay thế, 14 giải thích, 6 đính chính, 11 tạm ngưng, 5 đình chỉ) | 19.998 thẻ, 1.832 trang | **Bỏ** | **Tái sử dụng**: nguồn xác nhận quan hệ + nhãn vàng |
| **`p@new-types="<mã>:<id>[,…]"`** | Thêm văn bản sửa đổi cho **cùng** điều khoản (thường là lần sửa sau). 1.543/1.677 id có trong dữ liệu crawl | 1.464 thẻ, 449 trang | Bỏ | **Tái sử dụng** cùng `type` (bộ vàng hiện **chưa** gồm phần này) |
| `p@type="10:PHU-LUC"` | Điều khoản thuộc phụ lục/quy chế bị sửa (không có id nguồn) | 81 | Bỏ | Đánh dấu "phần phụ lục bị sửa", không nối id |
| **`p@parent-id`** | Id cha (điều khoản chứa khoản/điểm, cả khi lời văn không có số) | 52.521, 1.333 trang | Bỏ | Tái sử dụng: cây cha–con chắc chắn hơn suy từ số |
| `p@data-merged="true"` | Tiêu đề chương ghép 2 dòng (số + tên) | 6.559, 913 trang | Không cần | Bỏ qua |
| `div.doc-region-appendix` | Vùng phụ lục/quy chế kèm theo | 334, 270 trang | **Dùng** | Giữ; dùng để tách khoá `phuluc:` khỏi Điều của văn bản chính (tránh trùng "Điều 11") |
| `a@href="/van-ban/chi-tiet/<id>"` | Liên kết trích dẫn tới văn bản khác (id = id danh mục) | ~11 nghìn trên trang gốc; 15,7 nghìn trên VBHN | **Dùng** (`legal.citations`) | Giữ; nối đích bằng id thay cho chuỗi số hiệu |
| `sup.footnote-ref` + `li.footnote-item#fn-N` | VBHN: chú thích "Khoản này được … theo quy định tại … có hiệu lực kể từ …" | 3.650 chú thích, 132 VBHN | Không ingest VBHN | Tái sử dụng làm nguồn đối chiếu/nhãn (đã có `parse_vbhn.py`) |
| `li.footnote-item` trên văn bản gốc | Chú thích giải thích trong biểu mẫu | 398 trang | Bỏ | Giữ thành ghi chú cuối điều khoản (không phải quan hệ) |
| `table.prov-table`, `td.prov-*`, `colspan/rowspan` | Bảng (mức phạt, biểu thuế…) | 10.121 bảng, 912 trang | **Dùng** (bung ô gộp) | Giữ |
| **`span.math-formula`** (+ `data-latex` ở VBHN) | Công thức (phân số dựng bằng span) | 560 trên 42 trang; 89 trên VBHN | **Bị làm phẳng** → mất phân số ("A = B C") | Tái sử dụng: chuyển thành "A = (B) / (C)" hoặc giữ LaTeX; quan trọng cho công thức thuế/tài chính |
| `img.prov-img`, `img@src` (blob vbpl-public, base64, ảnh thuvienphapluat) | Hình/sơ đồ/mẫu | 590 ảnh / 99 trang (+ ảnh khác 846 trang) | Bỏ (đã quyết định) | Giữ quyết định; chỉ ghi chỗ có ảnh |
| `sup`/`sub` thường | m², °C, chỉ số | 30 nghìn / 17 nghìn | Thành chữ thường | Có thể giữ `^`/`_` cho đơn vị; ưu tiên thấp |
| `a.clsBookmark_*`, `a.clsopent*`, `span@data-href="/van-ban/get/noi-dung-tham-chieu…"`, `a@name="dieu_#"`, `ng-star-inserted`, `Mso*`, `docs-internal-guid` | Rác từ bản chép trang khác (thuvienphapluat, luatvietnam, Word, Google Docs) | rải rác | Bỏ | Bỏ |
| `main.docx-viewport`, `article.docx-page`, `div.docx-header/footer` | Trang dựng từ DOCX (mới) | 269 trang gốc, 101 VBHN | — | Header/footer chứa quốc hiệu/số hiệu: dùng làm metadata, không làm chunk |

**Việc nên làm khi sửa bộ chuyển đổi** (cần duyệt; đổi converter = backfill metadata, không phải embed lại nếu không đổi lời văn):
1. Ghi `type`/`new-types` thành `legal.provision_relations` [{code, source_prov_id}] trên chunk của văn bản đích.
2. Ghi `parent-id` và khoảng ký tự từng khoản/điểm.
3. Chuyển `math-formula` thành dạng phân số đọc được (việc này **đổi lời văn** → cần embed lại 42 văn bản).

**Cập nhật 20/10 — thêm `new-types` vào bộ vàng:**
- Bộ vàng tăng 19.057 → 20.660 cạnh (+1.603).
- Nhãn bộ khoá kín (giữ nguyên 287 đoạn) tăng 988 → 1.044. Bản cũ lưu ở `runs/locked-labels-v1.jsonl`.
- Qwen: phủ 0,77 đúng điều khoản / 0,85 tính cả đúng nhánh; tỷ lệ dự đoán được xác nhận tăng 0,76 → 0,80 (52 dự đoán "chưa xác nhận" nay được xác nhận).
- Bộ đọc vbpl: 0,30 / 0,39; tỷ lệ được xác nhận 0,77.

**Ngày hiệu lực theo từng khoản nguồn (API vbpl "provision-target", 20/10):**
- Đã lấy 10.099/18.971 điều khoản đích. Dừng vì vbpl trả HTTP 500 liên tiếp; chạy lại được, có resume.
- 10.856 khoản nguồn **trùng** `effective_on` danh mục của văn bản sửa đổi. Chỉ 31 khác, và các ca khác xem ra là lỗi dữ liệu (ví dụ TT 01/2024 ghi 2016).
- Thiếu `effFrom`: 26. Văn bản nguồn ngoài danh mục: 47.
- 9.345 điều khoản đích có 1 khoản nguồn; 678 có 2; 76 có ≥3 (chuỗi sửa đổi).
- → Ngày hiệu lực cấp văn bản sửa đổi là đủ. Không cần cột ngày riêng theo khoản lấy từ vbpl; ngoại lệ chỉ lấy từ điều khoản thi hành của B khi văn bản ghi rõ.
