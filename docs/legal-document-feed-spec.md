# Chuẩn kết nối và tiêu chí nạp văn bản pháp luật vào WeRAG

Tài liệu bàn giao cho agent/đội khác làm hai việc lặp lại: **tìm văn bản mới
và đẩy vào hệ thống**, và **kiểm tra, cập nhật tình trạng hiệu lực** của văn
bản đã có. Phần 1–5 mô tả những gì hệ thống đang chạy (đối chiếu mã nguồn
ngày 2026-10-17); phần 6 là đề xuất bộ công cụ MCP để agent ngoài gọi mà
không cần SSH hay truy cập DB; phần 7 là hiện trạng và việc tồn đọng.

Mọi đường dẫn mã trong tài liệu tính từ gốc repo `WeRAG`.

---

## 1. Kiến trúc hiện tại

Hai kho dữ liệu tách biệt, cùng khoá bằng **id văn bản trên vbpl.vn**:

| Kho | Nội dung | Nơi lưu | Ai dùng |
|---|---|---|---|
| **Danh mục pháp lý** (legal catalogue) | Metadata + quan hệ giữa các văn bản (thay thế, bãi bỏ, sửa đổi, hướng dẫn, căn cứ…), **không có toàn văn** | Postgres `legal_documents`, `legal_document_relations` (migration 000129/000130), toàn nền tảng, không theo tenant | Tool `check_legal_status`, `find_related_legal_documents`, ghi chú hiệu lực trong kết quả tìm kiếm, wiki dạng catalogue |
| **Kho tài liệu công khai** (public KB) | Toàn văn HTML của văn bản còn hiệu lực, chunk theo Điều | 6 KB công khai (tenant 10000, `owner_tenant_id = 0`), mỗi KB một nhóm loại văn bản | `search_knowledge`, `read_document`, `search_document_section` |

Liên kết giữa hai kho: `legal_document_links(knowledge_id → legal_document_id)`,
tạo bởi quét duyệt pháp lý (`POST /system/legal-reviews/scan`). Tài liệu có
`metadata.external_id` trùng id danh mục nguồn `vbpl` được **tự duyệt**
(`reviewed_by = "vbpl-import"`); tài liệu khác chờ SuperAdmin duyệt tay.

Luồng dữ liệu đang chạy:

```
vbpl.vn ──(1) crawl_vbpl.py──▶ /home/orca/data/vbpl (state.sqlite, html/<id>.html,
   │                              documents.jsonl, relations.jsonl)
   │                                 │
   │                 (2) legal-catalog-import ──▶ Postgres legal_documents / relations
   │                                 │
   │                 (3) mc mirror ──▶ MinIO bucket werag-vbpl/vbpl/
   │
   └──(4) ingest_vbpl.py ──▶ POST /knowledge-bases/{kb}/knowledge/file ──▶ KB công khai
                                                         │
                         (5) POST /system/legal-reviews/scan ──▶ legal_document_links
                         (6) POST /knowledgebase/{kb}/wiki/catalogue/rebuild ──▶ wiki
```

- Bước 1–3 chạy tự động hằng đêm 02:30 giờ VN trong container
  `WeKnora-vbpl-sync` (`deploy/vbpl-sync/sync.sh`, profile compose
  `vbpl-sync`, project `spec-airag-refactor`).
- Bước 4–6 **chạy tay**. Đây là phần cần agent đảm nhận.

Các KB công khai trên dev (id dùng cho `--kb`):

| KB | id | Loại văn bản |
|---|---|---|
| VBPL – Hiến pháp, Bộ luật, Luật, Pháp lệnh | `906d90be-d815-48eb-a876-941aef9cda1a` | Hiến pháp, Bộ luật, Luật, Pháp lệnh |
| VBPL – Nghị quyết | `d5bbdb05-4722-4ef0-a549-596572196524` | Nghị quyết |
| VBPL – Nghị định | `c1d2371a-3ce5-49ae-9896-4640370dbe5c` | Nghị định |
| VBPL – Quyết định, Chỉ thị | `3a711060-74f8-4e7f-b991-cba6d1f6d1a4` | Quyết định, Chỉ thị (+ vài Công văn) |
| VBPL – Thông tư | `f3b46e70-121c-44d6-bd75-6a67d417a791` | Thông tư |
| VBPL – Thông tư liên tịch | `1b9a84bd-3a3e-4813-8e36-9aceae3c6517` | Thông tư liên tịch |

Khi tạo môi trường mới, id khác: lấy bằng `GET /knowledge-bases` (lọc tên
bắt đầu bằng `VBPL – `) hoặc tool MCP `list_knowledge_bases`.

---

## 2. Tiêu chí văn bản đầu vào

### 2.1 Nguồn

| Nguồn | Dùng cho | Ghi chú |
|---|---|---|
| **vbpl.vn** (Cơ sở dữ liệu quốc gia về pháp luật, Bộ Tư pháp) | Danh mục + toàn văn | Nguồn chuẩn, duy nhất đang dùng. API công khai, không cần đăng nhập, xem mục 3.1. |
| Công báo, trang bộ/ngành | Chưa dùng | Chỉ bổ sung khi vbpl.vn thiếu; văn bản phải được tạo dòng danh mục `source = manual` qua duyệt tay (mục 3.4). |
| Bộ dữ liệu Hugging Face `th1nhng0/vietnamese-legal-documents` | Đã thay bằng crawler | Snapshot cũ (2026-07-23), chỉ còn giá trị tham khảo; cùng id với vbpl.vn. |

### 2.2 Phạm vi văn bản được nhận (áp dụng cho cả danh mục và toàn văn)

Bộ lọc đang cài trong `scripts/legal_catalog/crawl_vbpl.py` (hàm `is_kept`),
thống nhất với quyết định 2026-10-09:

1. **Cấp ban hành: trung ương** (`agencyLevel = TRUNG_UONG`). Loại bỏ văn bản
   có `administrativeUnitIds` hoặc cơ quan ban hành khớp
   `UBND|HĐND|tỉnh|thành phố|TP.`.
   Văn bản địa phương **chưa nạp chính thức**; chỉ thử nghiệm với Hà Tĩnh và
   Nghệ An, văn bản ban hành năm 2026, và phải nhập bằng `-merge` (mục 3.3).
2. **Loại văn bản**: nhóm QPPL (Hiến pháp, Bộ luật, Luật, Pháp lệnh, Nghị
   định, Thông tư, Thông tư liên tịch) **hoặc** 29 loại văn bản hành chính
   theo Điều 7 Nghị định 30/2020/NĐ-CP (Nghị quyết, Quyết định, Chỉ thị, Quy
   chế, Quy định, Thông cáo, Thông báo, Hướng dẫn, Chương trình, Kế hoạch,
   Phương án, Đề án, Dự án, Báo cáo, Biên bản, Tờ trình, Hợp đồng, Công văn,
   Công điện, Bản ghi nhớ, Bản thỏa thuận, Giấy ủy quyền, Giấy mời, Giấy giới
   thiệu, Giấy nghỉ phép, Phiếu gửi, Phiếu chuyển, Phiếu báo, Thư công).
   Bỏ `Bản dịch văn bản`.
3. **Ngày ban hành ≥ 01/01/2010** (`--min-year 2010`), không ngoại lệ cho
   luật cũ còn hiệu lực. Văn bản trung ương ngoài bộ lọc vẫn được ghi vào
   danh mục dưới dạng **metadata-only** (`metadata_only: true`, không toàn
   văn, không quan hệ) để các quan hệ "căn cứ" có đích.

### 2.3 Tiêu chí riêng cho toàn văn (đẩy vào KB)

| Tiêu chí | Quy tắc | Vì sao |
|---|---|---|
| Tình trạng hiệu lực | Chỉ đẩy khi `effStatus` ∈ {Còn hiệu lực, Hết hiệu lực một phần, Chưa có hiệu lực, Ngưng hiệu lực, Không còn phù hợp}. **Không bao giờ** đẩy `Hết hiệu lực toàn bộ`; văn bản chuyển sang trạng thái này phải **gỡ khỏi KB** (giữ dòng danh mục). | Quyết định 2026-10-12: danh mục trả lời "đã hết hiệu lực", toàn văn hết hiệu lực không được phục vụ như luật hiện hành. |
| Định dạng | HTML gốc tải từ gateway vbpl.vn (`documentContent.content`), **giữ nguyên**, không chuyển sang Markdown/PDF. Backend tự nhận diện markup `prov-*` (`internal/vbpl`) và chunk mỗi Điều một chunk. | Markup `prov-*` là nguồn duy nhất cho Điều/Khoản/Điểm, trích dẫn và chế độ `amendment`. |
| Kích thước tối thiểu | Bỏ trang < 200 byte (placeholder rỗng của portal, ~58 trang). | |
| Giới hạn trang KB | Hệ thống từ chối tài liệu KB > 500 trang (cổng đầu vào 2026-10-16). Chưa gặp với HTML vbpl; nếu gặp, báo lại thay vì chia nhỏ. | |
| Ảnh nhúng | Giữ nguyên trong HTML tải lên; chunk tự bỏ `<img>`. Bản gốc có ảnh nằm trên MinIO `werag-vbpl/vbpl/html/`. | |
| Phụ lục | Không nạp thành tài liệu con. Ghi danh sách tệp phụ lục vào metadata `vbpl_appendix_files`; câu trả lời liên kết tới tệp trên vbpl.vn. | Quyết định 2026-10-12. |
| Trùng lặp | Backend từ chối (HTTP 409) tệp cùng hash trong cùng KB. Script coi 409 là "đã có". Trang **đổi nội dung** phải xoá tài liệu cũ rồi tải lại (chưa có cập nhật tại chỗ). | |
| KB đích | Theo `docType` → bảng mục 1. Một văn bản chỉ nằm trong **một** KB. | |

### 2.4 Metadata bắt buộc khi tải lên

Form field `metadata` là JSON **chuỗi → chuỗi** (không lồng; danh sách phải
JSON-encode). Hàm `metadata()` trong `scripts/legal_catalog/ingest_vbpl.py`
là tham chiếu:

| Khoá | Bắt buộc | Giá trị | Dùng ở đâu |
|---|---|---|---|
| `external_id` | **có** | id vbpl.vn (= `legal_documents.id`) | Tự duyệt liên kết pháp lý; nhận diện tài liệu để gỡ/thay |
| `source` | **có** | `vbpl` | Bỏ sinh câu hỏi khi parse; quét duyệt chỉ tự duyệt nguồn này |
| `doc_number` | có | số ký hiệu, ví dụ `30/2020/NĐ-CP` | Trích dẫn |
| `doc_type` | có | tên loại | Chọn KB, hiển thị |
| `issue_date`, `eff_from` | có | `YYYY-MM-DD` | Ghi chú hiệu lực |
| `eff_status` | có | tên trạng thái của portal | Kiểm tra hết hiệu lực |
| `vbpl_url` | có | `https://vbpl.vn/van-ban/chi-tiet/{id}` | Liên kết nguồn |
| `vbpl_appendix_files` | nếu có phụ lục | JSON list `[{"title","file_name"}]` | Liên kết phụ lục |

Tên tệp: `"<số hiệu> - <tiêu đề>.html"`, `/` đổi thành `-`, cắt 180 ký tự.

### 2.5 Khoá định danh — những điều dễ sai

- **Số hiệu không duy nhất**: ~70% văn bản trong bộ dữ liệu trùng số hiệu
  với văn bản khác (chủ yếu địa phương), 16% vẫn trùng khi ghép (số hiệu, cơ
  quan). Khoá duy nhất là **id vbpl.vn**; không bao giờ khoá bằng số hiệu.
- id vbpl.vn là số nguyên dạng chuỗi với văn bản cũ, UUID với văn bản mới.
- Trạng thái của portal có thể **lỗi thời** (ví dụ 29/2018/QH14 vẫn "Còn hiệu
  lực" nhiều tháng sau khi 117/2025/QH15 thay thế). Vì vậy hệ thống suy ra
  hết hiệu lực từ quan hệ (mục 5).

---

## 3. Chuẩn kết nối hiện có

### 3.1 Lấy dữ liệu từ vbpl.vn

Chi tiết trong docstring `scripts/legal_catalog/crawl_vbpl.py`; tóm tắt:

| Việc | Cách gọi |
|---|---|
| Liệt kê | `POST https://vbpl.vn/van-ban/trung-uong`, header `Next-Action: <list id>`, `Accept: text/x-component`, body `[{"pageNumber":1,"pageSize":500,"sortBy":"issueDate","sortDirection":"desc","agencyLevel":"TRUNG_UONG"}]`; đọc dòng RSC `1:` → `{total, items[]}` |
| Chi tiết + toàn văn | `GET https://vbpl-bientap-gateway.moj.gov.vn/api/qtdc/public/doc/{id}` → `data.{docNum,title,docType,issueDate,effFrom,effTo,effStatus,documentIssues,documentFields,documentContent.content,documentRelatedList}` |
| Quan hệ | server action "diagram", body `["<id>"]` → `documentNamesByType` (văn bản này tác động văn bản khác) / `documentNamesBySource` (văn bản khác tác động nó), mã quan hệ 1–18 (bảng `RELATION_LABELS`) |
| Tệp phụ lục, bản gốc PDF | `GET https://vbpl-bientap-gateway.moj.gov.vn/api/qtdc/public/doc/minio/buckets/vbpl/{docID}/{fileName}/download` — `fileName` phải path-escape (có tên chứa dấu cách: `VanBanGoc_TT __TT_BTC.pdf`); trả `application/octet-stream`. Bản gốc của văn bản nằm ở `data.documentContentFileName` của chi tiết (`VanBanGoc_….pdf`) |

Ràng buộc bắt buộc:

- Phải gửi `User-Agent` của trình duyệt (WAF trả 403 nếu không).
- Tốc độ ≤ 2 yêu cầu/giây (`--delay 0.5`); 403/429 = quá nhanh, lùi lâu.
- Id server action **đổi sau mỗi lần portal deploy**; crawler tự dò lại từ
  `_next/static/chunks/7325-*.js`. Nếu dò thất bại, truyền tay
  `--list-action/--diagram-action`.
- Một số văn bản trả 500 vĩnh viễn: bỏ qua sau 2 lần thử, **giữ metadata cũ**.
- Tuyệt đối không gọi `ws.vbpl.vn/vbqppl.asmx` (SOAP cũ, dữ liệu lỗi thời,
  lộ phương thức ghi `Update*`).

### 3.2 Định dạng trao đổi (hợp đồng giữa crawler và importer)

`documents.jsonl`, mỗi dòng một văn bản, tên trường theo bộ dữ liệu HF
(struct `vbplDocument` trong `cmd/legal-catalog-import/main.go`):

```json
{"id":"179941","title":"Nghị định 30/2020/NĐ-CP về công tác văn thư",
 "so_ky_hieu":"30/2020/NĐ-CP","loai_van_ban":"Nghị định",
 "co_quan_ban_hanh":"Chính phủ","nguoi_ky":"Nguyễn Xuân Phúc","chuc_danh":"Thủ tướng",
 "linh_vuc":"Hành chính","ngay_ban_hanh":"05/03/2020","ngay_co_hieu_luc":"05/03/2020",
 "ngay_het_hieu_luc":"","tinh_trang_hieu_luc":"Còn hiệu lực",
 "pham_vi":"Trung ương","url":"https://vbpl.vn/van-ban/chi-tiet/179941",
 "has_html":true,"updated_date":"2026-10-01T03:12:00Z","metadata_only":false}
```

Ngày dạng `dd/mm/yyyy`; rỗng = chưa biết. `relations.jsonl`:

```json
{"doc_id":"<id tác động>","other_doc_id":"<id bị tác động>","relationship":"Thay thế"}
```

Nhãn tiếng Việt của portal được ánh xạ sang quan hệ có hướng trong
`internal/legalcatalog/vbpl.go` (replaces, repeals, amends, guides, based_on,
suspends, consolidates, expires, partially_expires…). Nhãn mới chưa biết sẽ
bị bỏ qua và ghi log; thêm vào bảng ánh xạ khi portal thêm mã.

Thư mục làm việc `/home/orca/data/vbpl/` (gương trên MinIO
`werag-vbpl/vbpl/`): `state.sqlite` (trạng thái crawl, resumable),
`html/<id>.html`, hai tệp JSONL, `ingest.sqlite` (những gì đã tải lên KB,
khoá `(kb_id, vbpl_id)` + sha256 HTML).

### 3.3 Nạp danh mục vào Postgres

```bash
# bên trong container vbpl-sync (DB_* lấy từ môi trường server)
legal-catalog-import -documents /work/documents.jsonl -relations /work/relations.jsonl -snapshot 2026-10-17
```

Quy tắc importer:

- Upsert theo id. Dòng `metadata_only` **không ghi đè** dòng đã có đầy đủ.
- Dòng đã được SuperAdmin duyệt (`reviewed_by ≠ ''`) không bị ghi đè; khác
  biệt được cất vào `source_pending` cho người duyệt.
- Mặc định **thay toàn bộ** bảng quan hệ bằng các cạnh có cả hai đầu trong
  tệp. Importer từ chối thay bằng tập rỗng (bảo vệ sau sự cố 2026-10-12).
- `-merge`: chỉ thay cạnh chạm tới văn bản trong tệp; **bắt buộc** cho mọi
  tệp bộ phận (địa phương, bổ sung tay). Lần nhập trung ương đầy đủ hằng đêm
  sẽ xoá cạnh của văn bản địa phương; chạy lại `-merge` sau đó.
- `-prune`: xoá văn bản vbpl vắng mặt trong tệp. Không dùng trong lịch tự động.

### 3.4 REST API của WeRAG (`/api/v1`, bao `{success, data}`)

Xác thực cho agent máy: **API key** header `X-API-Key: sk-…`, tạo bởi
SystemAdmin qua `POST /tenants/{id}/api-keys` với body
`{"name":"vbpl-feeder","capabilities":["ingest","retrieve"],"knowledge_base_ids":[<6 KB>]}`.
Phạm vi KB của key là hàng rào cứng: key chỉ ghi được vào KB trong danh sách.

| Việc | Gọi | Quyền |
|---|---|---|
| Tải văn bản | `POST /knowledge-bases/{kb}/knowledge/file` (multipart: `file`, `fileName`, `metadata` JSON) → `data.id` | API key `ingest` |
| Liệt kê tài liệu trong KB | `GET /knowledge-bases/{kb}/knowledge?page=&page_size=&keyword=&parse_status=&source=` | `retrieve` |
| Đọc 1 tài liệu (có `metadata`) | `GET /knowledge/{id}` | `retrieve` |
| Xoá | `DELETE /knowledge/{id}` | `ingest` |
| Parse lại | `POST /knowledge/{id}/reparse` | `ingest` |
| Tải bản gốc | `GET /knowledge/{id}/download` | `retrieve` |
| Quét duyệt pháp lý | `POST /system/legal-reviews/scan` | **JWT SuperAdmin**, không nhận API key |
| Danh sách duyệt | `GET /system/legal-reviews?status=pending&knowledge_base_id=&page=&page_size=` | JWT SuperAdmin |
| Quyết định duyệt | `PUT /system/legal-reviews/{knowledge_id}` body `{"action":"approve"|"reject","legal_document_id":"…","document":{document_number,title,doc_type,issuer,signer,field,issued_on,effective_on,expired_on,status}}` (`document` không có `legal_document_id` → tạo dòng `manual`) | JWT SuperAdmin |
| Tìm trong danh mục | `GET /system/legal-catalog/search?q=30/2020` | JWT SuperAdmin |
| Đề nghị bổ sung: quyết định | `PUT /system/legal-requests/{id}` body `{"action":"ingest"\|"reject"\|"added","knowledge_base_id","knowledge_id","note","allow_pdf"}`; `ingest` văn bản chỉ có PDF scan mà không `allow_pdf` → 400 và lưu `detail.portal` | JWT SuperAdmin |
| Đề nghị bổ sung: kiểm tra cổng | `POST /system/legal-requests/{id}/portal-check` → item với `detail.portal = {checked_at, status, html, pdf_file, exists}` (chỉ đọc chi tiết, không tải PDF) | JWT SuperAdmin |
| Dựng lại wiki catalogue | `POST /knowledgebase/{kb}/wiki/catalogue/rebuild`, trạng thái `GET …/wiki/catalogue/status` | Tenant Admin + quản lý KB (API key `ingest` được) |

Khoảng trống hiện tại của REST đối với agent ngoài:

- Không có cách tìm tài liệu theo `external_id` (phải liệt kê rồi lọc
  `metadata`, hoặc dựa vào `ingest.sqlite`).
- Không có endpoint tra cứu hiệu lực; `check_legal_status` chỉ là tool của
  agent chat, tra cứu danh mục bằng tay cần SQL hoặc JWT SuperAdmin.
- Quét duyệt và tìm danh mục cần JWT của người SuperAdmin.

### 3.5 MCP endpoint hiện có của WeRAG

WeRAG đã có **MCP server** (Streamable HTTP, `internal/mcpserver`) tại
`/mcp/{endpoint_id}`, xác thực Bearer token `mcp_…`. Tạo endpoint:
`POST /mcp-endpoints` `{"name","knowledge_base_ids":[…],"tools":[…],"rate_limit_per_minute"}`,
danh mục tool `GET /mcp-endpoints/tools`.

Tool có sẵn: `list_knowledge_bases`, `search_knowledge`, `grep_chunks`,
`list_documents`, `read_document`, `ask`, `wiki_search`, `wiki_read_page`,
`wiki_index`, `add_document`, `update_document`, `delete_document`.

Hạn chế với việc nạp VBPL: `add_document` chỉ nhận **Markdown hoặc URL**,
không nhận tệp HTML kèm metadata, nên **không dùng được** cho luồng vbpl
(mất markup `prov-*` và `external_id`).

**Nhóm tool `legal` (chỉ đọc, có từ 2026-10-17, `internal/mcpserver/tools_legal.go`)** —
bật khi tạo endpoint (nằm trong allowlist mặc định vì không ghi), cần
capability `retrieve`; không bị giới hạn theo KB của endpoint vì danh mục
là luật công khai:

| Tool | Tham số | Trả về |
|---|---|---|
| `legal_check_status` | `document_number` hoặc `title` | Báo cáo như tool agent `check_legal_status` (text) + `structuredContent.reports`, `derived_status` |
| `legal_find_related` | `document_number`/`title`, `mode` guidance/amendments/history/basis, `depth` 1–3, `include_expired` | Văn bản liên quan theo bậc, kèm `knowledge_id` nếu đã có trong KB |
| `legal_catalog_search` | `q`, `limit` ≤30 | Dòng danh mục: `id` (= `external_id`), số hiệu, loại, ngày, trạng thái, `url` |
| `legal_list_ingested` | `vbpl_ids` (≤100) và/hoặc `knowledge_base_id`, `page`, `page_size` | Tài liệu trong KB công khai nối với danh mục; `missing_vbpl_ids` cho id chưa nạp; `superseded_by` nếu đã bị thay thế |
| `legal_expiry_audit` | `knowledge_base_id?`, `limit` ≤1000 | `remove` (danh mục ghi hết hiệu lực toàn bộ) và `review` (còn hiệu lực theo portal nhưng đã bị thay thế/làm hết hiệu lực); không xoá gì |
| `legal_catalog_sync_status` | – | Ngày snapshot, `stale` (>48 h), số văn bản theo trạng thái, số quan hệ, số tài liệu/`unlinked` từng KB công khai |
| `legal_list_missing` | `doc_type?`, `min_year` (2010), `issued_after?`, `include_metadata_only?`, `limit` ≤500 | Văn bản trong phạm vi (loại mà một KB "VBPL – <loại>" nắm, trung ương, ≥ min_year, chưa hết hiệu lực toàn bộ, có metadata đầy đủ) **chưa có trong KB công khai**, mới nhất trước, mỗi dòng kèm `knowledge_base_id` đích suy từ tên KB |
| `legal_list_requests` | `status` requested (mặc định)/added/rejected/all, `limit` ≤500 | Đề nghị bổ sung văn bản (bảng `legal_document_requests`, migration 000162): câu hỏi chat nêu số hiệu đầy đủ mà không KB nào có toàn văn. Mỗi dòng: `vbpl_id` (rỗng nếu danh mục không biết số hiệu), số hiệu, tên, loại, trạng thái, `request_count` (mỗi phiên chat tính một lần), `last_requested_at`, `url`, `portal` (khi SuperAdmin đã "Kiểm tra cổng" hoặc lần nạp bị từ chối vì thiếu toàn văn: `{checked_at, status, html, pdf_file, exists}`). `legal_ingest_document` (MCP hoặc SuperAdmin `/system/legal-requests`) tự chuyển các đề nghị của văn bản đó sang `added` |

**Nhóm tool `legal_write` (có từ 2026-10-17, `internal/mcpserver/tools_legal_write.go`)** —
mọi tool đều "destructive": không nằm trong allowlist mặc định, cần
capability `ingest`, chỉ ghi vào KB công khai (`owner_tenant_id = 0`) nằm trong
`knowledge_base_ids` của endpoint, và mỗi lần ghi được ghi vào bảng
`legal_feed_log` (migration 000152: action, vbpl_id, knowledge_id, KB, số hiệu,
lý do, endpoint, chi tiết JSON):

| Tool | Tham số | Hành vi |
|---|---|---|
| `legal_ingest_document` | `vbpl_id`, `knowledge_base_id` (id hoặc tên), `reason?`, `allow_pdf?` (mặc định false) | Tải chi tiết từ gateway vbpl.vn ngay lúc gọi (cùng byte và md5 với `ingest_vbpl.py`), từ chối "Hết hiệu lực toàn bộ" và văn bản không có toàn văn; không có HTML nhưng có bản gốc PDF (scan) → từ chối "chỉ có bản PDF scan; bật allow_pdf để nạp", với `allow_pdf=true` thì tải PDF (≤50 MB) và nạp nó (parser pdf mặc định của KB OCR; metadata thêm `source_format=pdf_ocr`, `source_file`); kết quả có `source_format` (`html` \| `pdf_ocr`); không có cả hai → "vbpl.vn không có toàn văn (HTML hay PDF)"; đã có cùng md5 → `unchanged`; khác md5 → xoá bản cũ rồi tải lại → `replaced`; đang ở KB khác → từ chối; metadata như mục 2.4 |
| `legal_remove_document` | `vbpl_id` hoặc `knowledge_id`, `reason` (bắt buộc) | Xoá một tài liệu khỏi KB công khai; dòng danh mục giữ nguyên |
| `legal_apply_expiry_audit` | `knowledge_base_id?`, `expected?`, `limit` ≤1000, `reason?` | Xoá nhóm `remove` của `legal_expiry_audit`; nếu số thực tế lớn hơn `expected` thì từ chối (danh mục đã đổi); không bao giờ đụng nhóm `review` |
| `legal_review_scan` | – | Chạy quét duyệt tới khi hết (tối đa 10 vòng × 5.000); trả `created`, `pending` |
| `legal_wiki_rebuild` | `knowledge_base_id` | Dựng lại wiki catalogue của KB (chạy nền) |

Quy tắc phân quyền phải biết: bộ quyết định KB (`internal/application/access`)
trước đây không cho principal máy (API key, endpoint MCP) đụng KB công khai.
Từ 2026-10-17, principal máy **của tenant nền tảng 10000** ("SuperAdmin")
được quyền Admin trên KB công khai lưu trong tenant đó (`decideMachineKB`);
key của tenant khác vẫn bị từ chối. Vì vậy endpoint nạp dữ liệu phải được
SuperAdmin tạo trong tenant 10000 và liệt kê 6 KB công khai trong
`knowledge_base_ids` (để trống thì `list_knowledge_bases` của tenant không
trả KB công khai).

Tạo endpoint: `POST /mcp-endpoints` (JWT SuperAdmin, `X-Tenant-ID: 10000`)
với `tools` = các tên trên và `knowledge_base_ids` = 6 KB; gọi
`POST /mcp/{endpoint_id}` (JSON-RPC `tools/call`, header
`Authorization: Bearer mcp_…`, `Accept: application/json, text/event-stream`).
Trên dev đã có endpoint `legal-feed-test` (tenant 10000, đủ 11 tool legal +
`list_knowledge_bases`) để thử; token chỉ hiện lúc tạo/rotate.

---

## 4. Quy trình vận hành cho agent nạp dữ liệu

Chu kỳ chuẩn (có thể giao cho agent chạy theo lịch sau khi sync đêm xong):

1. **Kiểm tra sync đêm**: `docker logs --tail 50 WeKnora-vbpl-sync` và mốc
   `/home/orca/data/vbpl/.sync.ok` mới hơn `.sync.started`. Nếu thất bại,
   chạy lại `sudo docker exec WeKnora-vbpl-sync sync.sh`; thất bại lần nữa
   → báo người (thường là portal đổi action id hoặc 500 kéo dài).
2. **Tìm văn bản cần đẩy**: so `state.sqlite` (kept = 1, có `html/<id>.html`,
   `effStatus` trong tập cho phép ở 2.3) với `ingest.sqlite`:
   - chưa có trong `uploads` → **mới**;
   - có nhưng sha256 khác → **đổi nội dung**: xoá knowledge cũ
     (`DELETE /knowledge/{id}`) rồi tải lại;
   - có trong `uploads` nhưng `effStatus` nay là `Hết hiệu lực toàn bộ` →
     **gỡ** (xoá knowledge, giữ dòng trong `uploads` với ghi chú, hoặc xoá
     dòng để không tải lại).
3. **Đẩy**: `ingest_vbpl.py --api … --kb <KB theo loại> --doc-type "<loại>" --eff-status "Còn hiệu lực" --eff-status "Hết hiệu lực một phần" …`
   (mỗi KB một lệnh, hoặc viết wrapper lặp qua bảng loại → KB). Script đã
   idempotent, tự chờ backend khởi động lại, nghỉ 0,5 s giữa hai lần tải.
   Trước khi nạp số lượng lớn: `nvidia-smi` (GPU A6000 dùng chung với
   embedding; không nạp khi đang huấn luyện).
4. **Chờ parse xong**: `GET /knowledge-bases/{kb}/knowledge?parse_status=failed`
   phải rỗng; `pending/processing` giảm dần. Tài liệu `failed` → `POST /knowledge/{id}/reparse` một lần, còn lỗi thì báo.
5. **Quét duyệt**: `POST /system/legal-reviews/scan` (JWT SuperAdmin) để tạo
   `legal_document_links` cho tài liệu mới; kiểm tra
   `GET /system/legal-reviews?status=pending` — tài liệu vbpl phải tự duyệt,
   nếu còn pending là `external_id` sai.
6. **Dựng lại wiki** cho KB vừa thay đổi (mục 3.4), ~1 phút/6 KB.
7. **Báo cáo**: số mới / đổi / gỡ / lỗi theo KB, ngày snapshot danh mục
   (`max(source_updated_at)` trong `legal_documents`).

Quy tắc an toàn:

- Chỉ ghi vào 6 KB `VBPL – …`; không tạo KB mới, không đổi cấu hình parse.
- Không chạy `legal-catalog-import` không có `-merge` với tệp bộ phận.
- Không xoá hàng loạt theo tên tệp; luôn xác định bằng `external_id`.
- Không chỉnh mã Go trong `/home/WeRAG` khi người dùng đang thử (backend dev
  tự khởi động lại và huỷ các parse đang chạy).

---

## 5. Kiểm tra tình trạng hiệu lực

### 5.1 Logic đang dùng (`internal/agent/tools/check_legal_status.go`)

Với một văn bản, hệ thống trả về:

1. **Trạng thái nguồn** (`legal_documents.status`, kèm ngày snapshot
   `source_updated_at`): Còn hiệu lực / Hết hiệu lực toàn bộ / Hết hiệu lực
   một phần / Chưa có hiệu lực / Ngưng hiệu lực / Không còn phù hợp / Chưa
   xác định.
2. **Trạng thái suy ra** (`derived_status`): khi nguồn nói còn hiệu lực (hoặc
   một phần, hoặc chưa xác định) và `expired_on` rỗng, nhưng có cạnh **đến**
   kiểu `replaces` hoặc `expires` từ một văn bản đã có hiệu lực
   (`effective_on ≤ hôm nay`) → kết luận **"Hết hiệu lực toàn bộ"** từ ngày
   đó (hoặc "Có thể đã hết hiệu lực toàn bộ" nếu nguồn ghi một phần). `repeals`
   **không** dùng để suy ra vì portal gộp bãi bỏ một phần vào đó.
3. Danh sách văn bản tác động (thay thế, bãi bỏ, sửa đổi, hướng dẫn, đình
   chỉ…) và văn bản nó tác động, kèm ngày và trạng thái.

Hệ quả cho KB: tài liệu có trạng thái (nguồn hoặc suy ra) hết hiệu lực toàn
bộ vẫn tìm được nhưng bị nhân điểm ×0,75 và mang ghi chú pháp lý cho model;
quyết định vận hành là **gỡ** chúng khỏi KB (mục 4 bước 2).

### 5.2 Cách kiểm tra theo lô (chưa có API, dùng SQL)

Chạy trong container Postgres (`sudo docker exec -i WeKnora-postgres psql -U … -d …`):

```sql
-- tài liệu trong KB công khai mà danh mục đã ghi hết hiệu lực toàn bộ
SELECT k.id AS knowledge_id, k.knowledge_base_id, d.id AS vbpl_id, d.document_number, d.status, d.expired_on
FROM knowledges k
JOIN knowledge_bases kb ON kb.id = k.knowledge_base_id AND kb.owner_tenant_id = 0
JOIN legal_documents d ON d.id = k.metadata->>'external_id'
WHERE k.deleted_at IS NULL AND d.status LIKE 'Hết hiệu lực toàn bộ%';

-- tài liệu còn hiệu lực theo nguồn nhưng đã bị thay thế / làm hết hiệu lực
SELECT k.id AS knowledge_id, d.id AS vbpl_id, d.document_number, by.document_number AS replaced_by, by.effective_on
FROM knowledges k
JOIN knowledge_bases kb ON kb.id = k.knowledge_base_id AND kb.owner_tenant_id = 0
JOIN legal_documents d ON d.id = k.metadata->>'external_id'
JOIN legal_document_relations r ON r.to_id = d.id AND r.relation IN ('replaces','expires')
JOIN legal_documents by ON by.id = r.from_id AND by.effective_on <= CURRENT_DATE
WHERE k.deleted_at IS NULL AND d.expired_on IS NULL
  AND d.status IN ('', 'Chưa xác định', 'Còn hiệu lực', 'Hết hiệu lực một phần');
```

Kết quả truy vấn 1 là danh sách gỡ bắt buộc; truy vấn 2 là danh sách cần
người xem xét (xem điều khoản chuyển tiếp) trước khi gỡ. Giữ hai nhóm tách
biệt trong báo cáo.

### 5.3 Hợp đồng JSON đề xuất cho kết quả kiểm tra

Dùng chung cho SQL, REST hoặc MCP tool, để agent khác xử lý máy:

```json
{
  "vbpl_id": "179941",
  "document_number": "30/2020/NĐ-CP",
  "title": "…",
  "source_status": "Còn hiệu lực",
  "source_snapshot": "2026-10-14",
  "effective_on": "2020-03-05",
  "expired_on": null,
  "derived_status": "Hết hiệu lực toàn bộ",
  "derived_from": {"relation": "replaces", "vbpl_id": "…", "document_number": "…", "effective_on": "2026-01-01"},
  "in_knowledge_base": {"kb_id": "c1d2371a-…", "knowledge_id": "…"},
  "action": "remove" | "review" | "keep"
}
```

`action`: `remove` khi `source_status` bắt đầu bằng "Hết hiệu lực toàn bộ";
`review` khi chỉ `derived_status` nói hết hiệu lực; `keep` còn lại.

---

## 6. Đề xuất bộ công cụ MCP "legal-feed"

Mục tiêu: agent ngoài làm toàn bộ mục 4 và 5 qua MCP, không cần SSH, SQL hay
JWT của người. Hai lựa chọn triển khai:

- **(A) Mở rộng MCP server có sẵn** (`internal/mcpserver`): thêm nhóm tool
  `legal` vào danh mục `GET /mcp-endpoints/tools`, bật theo endpoint như các
  nhóm `ingest`/`wiki`. Ưu: tái dùng xác thực, rate limit, phạm vi KB; một
  URL cho mọi client. **Khuyến nghị.**
- (B) MCP server riêng (Python, stdio) bọc `crawl_vbpl.py` +
  `legal-catalog-import` + REST. Chỉ cần nếu muốn chạy crawler trên máy
  khác; crawl vẫn nên ở container `vbpl-sync`.

Trạng thái: **cả ba giai đoạn đã xây và chạy thật 2026-10-17** — xem hai bảng
ở mục 3.5. Dưới đây là thiết kế gốc để đối chiếu.

| Tool | Tham số | Trả về | Dựa trên |
|---|---|---|---|
| `legal_check_status` ✅ | `document_number` hoặc `title` | báo cáo + `derived_status` | `CheckLegalStatusTool` (agent tool bọc lại) |
| `legal_find_related` ✅ | `document_number`/`title`, `mode`, `depth`, `include_expired` | văn bản liên quan kèm `knowledge_id` nếu đã trong KB | `FindRelatedLegalDocumentsTool` |
| `legal_catalog_search` ✅ | `q`, `limit` | dòng danh mục | `LegalReviewService.SearchCatalog` |
| `legal_catalog_sync_status` ✅ | – | ngày snapshot, `stale`, số văn bản theo trạng thái, KB công khai | SQL trên `legal_documents`/`knowledges` (backend không thấy `.sync.ok`) |
| `legal_list_ingested` ✅ | `vbpl_ids?`, `knowledge_base_id?`, phân trang | tài liệu KB công khai + dòng danh mục + `superseded_by` | SQL `knowledges.metadata->>'external_id'` |
| `legal_ingest_document` | `vbpl_id`, `kb_id?` (mặc định theo loại) | `knowledge_id`, `created|unchanged|replaced` | tải HTML từ thư mục crawl/MinIO, gọi `KnowledgeService.CreateKnowledgeFromFile` với metadata 2.4; thay thế khi hash khác |
| `legal_remove_document` | `vbpl_id` hoặc `knowledge_id`, `reason` | ok | `DeleteKnowledge`; ghi log lý do |
| `legal_expiry_audit` ✅ (chỉ đọc) | `knowledge_base_id?`, `limit` | hai danh sách `remove`/`review` (mục 5.2); bản gỡ thật (`dry_run=false`) thuộc giai đoạn 2 | truy vấn 5.2 trong Go |
| `legal_review_scan` ✅ | – | số liên kết tạo, số pending | `legalReviewService.Scan` lặp tới 0 |
| `legal_wiki_rebuild` ✅ | `knowledge_base_id` | trạng thái | `WikiCatalogueOverviewService.StartRebuild` |

Thiết kế bắt buộc:

- Hai nhóm trong catalog endpoint: `legal` (đọc, capability `retrieve`) và
  `legal_write` (ghi, capability `ingest`). Tool ghi chỉ chạy trên KB
  `owner_tenant_id = 0` nằm trong phạm vi endpoint; từ chối KB tenant.
- Mọi tool ghi idempotent theo `vbpl_id`; không có tool "xoá theo tên".
- `legal_expiry_audit` chỉ đọc; gỡ thật là `legal_apply_expiry_audit`, ghi
  vào `legal_feed_log` (migration 000152).
- Tool crawl (gọi vbpl.vn) **không** đưa vào MCP: crawl chạy ở container
  theo lịch; MCP chỉ báo trạng thái (`legal_catalog_sync_status`). Nếu cần
  kích hoạt tay, thêm `legal_catalog_sync_run` gọi `docker exec` qua một
  job queue, không gọi trực tiếp từ handler HTTP.
- Trước khi sửa `DefaultAllowedTools` hay `mcpserver.Server`, chạy
  `impact` (GitNexus) — lần trước `DefaultAllowedTools` xếp CRITICAL.

Thứ tự làm: (1) ✅ tool đọc; (2) ✅ `legal_remove_document`,
`legal_apply_expiry_audit`, `legal_feed_log`; (3) ✅ `legal_ingest_document`,
`legal_review_scan`, `legal_wiki_rebuild` — tất cả 2026-10-17, chạy thật
trên dev (round trip gỡ → nạp lại 23/2016/TT-BCA, từ chối văn bản hết hiệu
lực và văn bản đang ở KB khác). `ingest_vbpl.py` giờ là công cụ dự phòng cho
nạp hàng loạt (nhanh hơn vì đọc HTML đã crawl, không gọi gateway).

Chu kỳ vận hành qua MCP (thay cho mục 4 khi dùng agent ngoài):
`legal_catalog_sync_status` (snapshot mới, không `stale`) →
`legal_expiry_audit` → `legal_apply_expiry_audit(expected=N)` →
`legal_list_missing` (có thể lọc `issued_after` = lần nạp trước) →
`legal_ingest_document` từng văn bản (≤2 lần/giây vì gateway giới hạn) →
chờ `parse_status = completed` (`legal_list_ingested`) → `legal_review_scan`
→ `legal_wiki_rebuild` cho KB thay đổi. `legal_list_missing` thay cho việc so
state.sqlite với ingest.sqlite; nó không biết văn bản có toàn văn hay không
(chỉ gateway biết), nên `legal_ingest_document` sẽ báo "vbpl.vn không có
toàn văn (HTML hay PDF)" cho những dòng không có — đó là bình thường, không
phải lỗi.

**Văn bản chỉ có PDF scan** (đo 2026-10-18): trong các văn bản trung ương còn
hiệu lực bị thiếu, khoảng 1/6 không có HTML nhưng có bản gốc PDF
(`documentContentFileName`, ví dụ `VanBanGoc_74.TT.pdf`), phần còn lại cổng
không có gì ("Không có nội dung văn bản"); văn bản địa phương phần lớn có
HTML. PDF là ảnh scan không có lớp chữ: nạp bằng `allow_pdf=true`, parser pdf
mặc định của KB OCR nó (nhận ra Điều 1…, lỗi OCR điển hình "NĐ-CP"→"ND-CP",
"BỘ"→"BQ"), chunk không có markup `prov-*` nên cấu trúc Điều/Khoản yếu hơn
HTML. OCR của tài liệu KB không tính vào hạn mức token. Khi vbpl.vn sau này
đăng HTML, nạp lại (không `allow_pdf`) sẽ thay bản PDF (md5 khác →
`replaced`). Trên giao diện SuperAdmin (Đề nghị bổ sung văn bản): nút "Kiểm
tra cổng" (`POST /system/legal-requests/{id}/portal-check`) và "Nạp bản PDF
(OCR)" (`PUT /system/legal-requests/{id}` với `allow_pdf: true`).

---

## 7. Hiện trạng dev ngày 2026-10-17 và việc tồn đọng

Số liệu đọc từ DB dev và thư mục crawl:

| Mục | Giá trị |
|---|---|
| Danh mục `legal_documents` | 47.269 dòng: Còn hiệu lực 23.268; Hết hiệu lực toàn bộ 18.886; một phần 2.323; Chưa xác định 1.928; rỗng 500; Ngưng 189; Chưa có hiệu lực 97; Không còn phù hợp 77 |
| Quan hệ | 131.036 cạnh |
| Snapshot danh mục mới nhất | 2026-10-14 |
| Crawl `state.sqlite` | 57.687 văn bản trung ương liệt kê, 17.855 trong phạm vi, 17.852 có chi tiết; trong phạm vi: 8.884 còn hiệu lực, 6.843 hết toàn bộ, 1.985 một phần, 97 chưa có hiệu lực |
| Tài liệu trong 6 KB công khai | 10.924, tất cả `parse_status = completed` |
| Liên kết duyệt pháp lý | 5.183 approved; **5.924 tài liệu KB chưa có dòng liên kết** |

Ba việc phát hiện sáng 2026-10-17 và đã xử lý trong ngày:

1. **Sync đêm thất bại** 2026-10-16 và 2026-10-17 (container `unhealthy`,
   lần OK cuối 2026-10-15 02:32 VN). Nguyên nhân: vbpl.vn trả HTTP 500/502
   cho server action liệt kê đúng lúc 02:30 ba đêm liền (đêm 10-15 tự hồi
   sau 60 s, hai đêm sau không), còn `crawl_vbpl.py` chỉ thử lại lỗi 5xx
   2 lần (5 s, 10 s) — ngưỡng dành cho văn bản lỗi 500 vĩnh viễn — rồi
   thoát. Ban ngày cùng action id trả 200. **Đã sửa**: `Client.fetch(...,
   patient=True)` cho bước liệt kê, dùng lùi như 429 (30 s → 10 phút, 6 lần,
   ~25 phút tổng); image build lại, container recreate; chạy tay
   `sync.sh` thành công: snapshot 2026-10-17, 17.864 văn bản, 131.088 quan
   hệ. Nếu portal còn bảo trì dài hơn 25 phút lúc 02:30, đổi
   `VBPL_SYNC_CRON` sang `30 3 * * *`.
2. **Quét duyệt chưa chạy sau đợt nạp 2026-10-13** (5.924/10.924 tài liệu
   không có liên kết). Đã chạy `POST /system/legal-reviews/scan` ba lần
   (5.000 + 924 + 0; mỗi lần tối đa 5.000) → 10.924 liên kết, tất cả tự
   duyệt `vbpl-import`, 0 pending.
3. **Tài liệu KB đã hết hiệu lực toàn bộ**: 4 theo snapshot 10-14, thêm 10
   sau snapshot 10-17 (`legal_expiry_audit`) — đã gỡ 14 qua
   `DELETE /knowledge/{id}`. Nhóm `review` (còn hiệu lực theo portal nhưng
   đã bị thay thế) có **547** tài liệu, phần lớn TTLT cũ — chờ người xem.
4. Nạp bổ sung 17 văn bản mới (tất cả `NEW`, không có đổi nội dung) từ
   snapshot 10-17 bằng `ingest_vbpl.py` (Luật 1, NQ 1, NĐ 5, QĐ 1, TT 9).

5. **Dòng danh mục đã duyệt bị đóng băng** (phát hiện tối 2026-10-17 khi thử
   `legal_list_missing` → `legal_ingest_document`): 181 dòng `legal_documents`
   mang `reviewed_by` từ đợt duyệt hàng loạt 2026-10-12, nên import đêm
   không ghi đè mà cất khác biệt vào `source_pending`; 38 dòng đang có khác
   biệt, 10 dòng đổi trạng thái hiệu lực (ví dụ Luật Tiếp cận thông tin
   104/2016/QH13: dòng ghi "Còn hiệu lực", portal "Hết hiệu lực toàn bộ"; 9
   luật khác đã "Hết hiệu lực một phần"). Hai dòng (142841 Luật 64/2020,
   142847 Luật Doanh nghiệp 59/2020) không còn tồn tại trên portal (gateway
   400) — id cũ của snapshot HF, chưa bị prune. Các tool `legal_expiry_audit`,
   `legal_apply_expiry_audit`, `legal_list_missing`, `legal_catalog_sync_status`
   nay tính cả `source_pending->>'status'` và báo số dòng duyệt có khác biệt.
   **Quyết định của người dùng 2026-10-17: SuperAdmin phải duyệt** (chịu
   trách nhiệm với KB công khai), không bỏ cờ duyệt. Đã thêm bộ lọc
   **"Nguồn thay đổi"** (`GET /system/legal-reviews?status=source_changed`)
   trong màn Quản trị → Văn bản pháp lý: liệt kê tài liệu đã duyệt mà dòng
   danh mục có `source_pending`; hộp thoại duyệt sẵn có nút chép giá trị
   portal vào biểu mẫu, bấm Duyệt sẽ lưu và xoá `source_pending`. 37/38 dòng
   có khác biệt hiện ra ở đó. Trong 21 dòng đã duyệt không còn liên kết
   sống, 18 là dòng `manual` (giữ nguyên, không có nguồn portal) và 3 là dòng
   `vbpl` (101873, 142841, 142847) — **người dùng yêu cầu gỡ cờ duyệt
   2026-10-17**: đã gỡ (bản sao trước khi gỡ trong scratchpad phiên làm việc),
   chạy lại import → 104/2016/QH13 nay "Hết hiệu lực toàn bộ" từ 01/09/2026;
   142841 (Luật 64/2020) và 142847 (Luật Doanh nghiệp 59/2020) hoá ra là bản
   trùng từ snapshot HF của hai dòng đang sống (142882, 142881, portal ghi
   "Hết hiệu lực một phần"), không quan hệ, không liên kết sống → đã xoá
   (bản sao CSV trong scratchpad) để `check_legal_status` không trả hai kết
   quả cho cùng số hiệu. Bài học: id của snapshot HF có thể đã bị portal
   thay bằng id mới; dòng vbpl không xuất hiện trong crawl nào nữa là ứng
   viên để prune, nhưng `-prune` toàn cục sẽ xoá cả văn bản địa phương nạp
   bằng `-merge`, nên vẫn xử lý tay.
   `check_legal_status` của agent chat đọc `status` của dòng đã duyệt, nên
   duyệt sớm các thay đổi trạng thái hiệu lực.

Việc dài hạn đã quyết định nhưng chưa làm: văn bản địa phương (đồ thị theo
tỉnh, cột province trong danh mục); thẻ chủ đề tự gán cho KB VBPL; bộ tool
MCP mục 6.

---

## 8. Danh mục tham chiếu nhanh

| Thành phần | Đường dẫn |
|---|---|
| Crawler | `scripts/legal_catalog/crawl_vbpl.py` |
| Nạp KB | `scripts/legal_catalog/ingest_vbpl.py` |
| Importer danh mục | `cmd/legal-catalog-import/main.go` |
| Ánh xạ quan hệ | `internal/legalcatalog/vbpl.go` |
| Container sync | `deploy/vbpl-sync/` (README tiếng Việt, `sync.sh`) |
| Chuyển HTML → chunk Điều | `internal/vbpl/`, `internal/application/service/knowledge_vbpl.go` |
| Tool hiệu lực / liên quan | `internal/agent/tools/check_legal_status.go`, `find_related_legal_documents.go` |
| Duyệt pháp lý | `internal/application/service/legal_review.go`, `internal/router/routes_legal_review.go` |
| MCP server | `internal/mcpserver/`, `internal/types/mcp_endpoint_tools.go`, `docs/api-next/mcp.md` |
| Schema danh mục | `migrations/versioned/000129_legal_catalog.up.sql`, `000130_legal_document_review.up.sql` |
| Wiki catalogue | `internal/application/service/wiki_catalogue_overview.go` |
| Dữ liệu crawl | `/home/orca/data/vbpl/` (host), MinIO `werag-vbpl/vbpl/` |
