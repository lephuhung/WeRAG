# TVPL ↔ vbpl: số hiệu làm điểm chung (2026-11-08)

Branch `feat/tvpl-number-key` (worktree `/home/WeRAG-wt/tvpl-number-key`, from
`feat/legal-upload-permission` a02d8bf2). Extension repo `/home/tvpl-weknora-extension`.

## Vấn đề

1. **422 với văn bản mới**: `LegalPushService.Push` → `resolveDoc` chỉ nhận văn bản A đã có trong
   `legal_documents` (danh mục vbpl, mới nhất 2026-11-03). Văn bản mới trên TVPL (15/2026/NĐ-CP,
   348/2026/NĐ-CP, 20/2026/NQ-CP… — đã kiểm: vắng thật, không phải lệch key) → 422
   "số hiệu … không có trong danh mục". Chưa từng có `push_full` thành công; 0 knowledge có
   `metadata.source = 'tvpl'` → không cần backfill.
2. **Id điều/khoản lệch**: tệp TVPL đặt id `tvpl-<lawId>-dieu:5/khoan:2`. `knowledge_vbpl.go`
   không phân biệt nguồn: chunk ghi `Source:"vbpl"` (`internal/vbpl/chunk.go:473`) và
   `provision_ids` = id TVPL. Hệ quả: `SourceURL` dựng link vbpl.vn sai/hỏng
   (`chunk_snippet.go:368`); `editorHolds` (`legal_amend_edge.go:278`) thấy id khác → loại cạnh
   biên tập viên vbpl im lặng; lấy nội dung mới của B theo id (`legal_amend_editor.go:480`) hụt.
3. **Id dòng danh mục**: nếu dòng TVPL tồn tại rồi vbpl có cùng văn bản: `ResolveTwins` bỏ id vbpl
   (alias sang dòng TVPL) → trạng thái hiệu lực vbpl không bao giờ cập nhật dòng đó;
   `legal_ingest_document` theo id vbpl chỉ dedup bằng `external_id` → bản toàn văn thứ hai.

## Nguyên tắc

- **Số hiệu (`number_key` = `CatalogNumberKey`) là khoá ghép** giữa TVPL và vbpl. Số hiệu không
  duy nhất → khi một key có nhiều dòng, phân định bằng `legalcatalog.SameDocument` (ngày ban hành,
  loại, cơ quan, địa danh, tiêu đề). Không bao giờ để số hiệu một mình quyết giữa nhiều văn bản.
- Dòng danh mục TVPL là **tạm**: khi vbpl có cùng văn bản, **dòng vbpl thắng**, mọi tham chiếu
  chuyển sang id vbpl, dòng TVPL bị xoá.
- Nội dung TVPL không hiện link ra người dùng cuối (không `source_url` trong câu trả lời).

## Phần 0 — Push tạo dòng danh mục cho văn bản mới (sửa 422)

`internal/application/service/legal_push.go`

- `readLegalPushMeta`: đọc thêm `title`, `eff_from` (YYYY-MM-DD).
- Trong `Push`, khi `resolveDoc` trả nil **và** key không có dòng nào (`len(cands)==0`, lý do
  "không có trong danh mục") **và** mode là `full`/`auto` → `createFeedDoc`:
  - bắt buộc `doc_number`, `doc_type`, `issuer`, `issue_date`; `title` lấy metadata, thiếu thì
    `<title>` của HTML; thiếu cả hai → 422 như cũ (kèm lý do thiếu trường gì).
  - Dòng mới: `ID = "<feed>-" + source_doc_id` (vd `tvpl-576509`; ổn định, ≤64), `Source = feed`
    (hằng mới `LegalDocumentSourceTVPL`/dùng meta.feed), `NumberKey = CatalogNumberKey`,
    `Status` = "Chưa có hiệu lực" nếu `eff_from` > hôm nay (giờ VN), "Còn hiệu lực" nếu ≤ hôm nay,
    "Chưa xác định" nếu không có `eff_from`; `ReviewedAt` rỗng (chờ SuperAdmin).
  - Ghi bằng `INSERT … ON CONFLICT (id) DO NOTHING` rồi đọc lại (hai lượt push đồng thời).
  - `res.CatalogueCreated = true`; feed log action giữ `push_full`, detail có `catalogue_created`.
- Key **có** dòng nhưng không dòng nào khớp ngày/cơ quan → vẫn 422 kèm candidates (người push chọn
  `legal_document_id`); không tự tạo — tránh nhân đôi khi TVPL ghi cơ quan khác vbpl.
- Mode `links` với A chưa có → vẫn 422.
- Quyền: giữ như hiện tại (handler: SuperAdmin hoặc `can_upload_legal_documents`; ghi KB kiểm lại
  trong `ingest`).

## Phần 1 — Chunk ghi đúng nguồn

`internal/application/service/knowledge_vbpl.go` (hàm chuyển HTML prov-* thành chunk)

- Đọc `metadata.source` của knowledge (`vbpl.Metadata` ghi `"vbpl"`; push ghi `meta.feed`).
  Rỗng hoặc `vbpl` → như cũ. Khác → với mọi chunk: `legal.Source = source`,
  `legal.ProvisionIDs = nil`, `legal.Citations = nil`, `legal.EditorLinks = nil`,
  `AppendixFiles = nil` (link tải vbpl.vn). Giữ `SourceDocID` (= id danh mục).
- Hệ quả mong muốn: `SourceURL`/`SectionURL` trả "" (điều kiện `Source=="vbpl"`); `editorHolds`
  rơi về so số Điều; `heldProvisions` trong push dùng nhánh cấu trúc + id tổng hợp `fp-`.
- `legal_amend_editor.go` (lấy nội dung mới của B theo `source_provision_id`): thêm nhánh dự phòng
  theo `source_path` khi chunk của B không có `provision_ids` (nguồn không phải vbpl), dùng cùng
  `legalProvisionSpan`. Xác minh lại các chỗ khác đọc `ProvisionIDs` (`legal_amend_validate.go`,
  `citation.go`) chịu được map rỗng.

## Phần 2 — Import vbpl nhận lại dòng TVPL theo số hiệu

`internal/legalcatalog/twins.go`, `cmd/legal-catalog-import/main.go`,
`internal/application/repository/legal_catalog.go`

- `cmd/legal-catalog-import`: thêm `source`, `reviewed_by`, `title`… vào select catalogued rows.
- `ResolveTwins`: dòng catalogued có `Source` khác vbpl/manual (dòng feed) được đối xử như
  placeholder: một id vbpl mới `SameDocument` với nó (cùng `number_key`) → `replace(feedRow, d)`:
  id vbpl được import, dòng feed alias sang id vbpl. Dòng feed không bao giờ là survivor của một
  id vbpl. Dòng manual giữ hành vi cũ.
- Sau `UpsertDocuments`: với mỗi alias `feedID → vbplID` gọi repository `AdoptLegalDocument(ctx,
  fromID, toID)` (một transaction):
  - `knowledges.metadata.external_id` (KB công khai) và `chunks.metadata.legal.source_doc_id`
    của các knowledge đó;
  - `legal_document_links.legal_document_id`, `legal_document_requests.legal_document_id`
    (unique `(number_key, legal_document_id)` → gộp/bỏ trùng), `legal_document_drafts`
    (unique theo doc → nếu cả hai có: giữ bản của toID);
  - `legal_provision_links.target_doc_id/source_doc_id` (unique edge theo provision id — không đổi
    vì id provision không chứa doc id; `fp-` tổng hợp vẫn giữ nguyên);
  - `legal_provision_link_candidates.target_doc_id/amender_doc_id`;
  - `legal_document_relations` from/to (bỏ self-edge, ON CONFLICT DO NOTHING);
  - dòng feed đã được SuperAdmin duyệt (`reviewed_at`): dòng vbpl thừa kế `reviewed_by/at` và các
    trường đã duyệt; giá trị portal khác nhau đỗ vào `source_pending` (đúng quy tắc "reviewed
    rows stay reviewed");
  - xoá dòng feed; `legal_feed_log` giữ nguyên (lịch sử).
- Knowledge vẫn là văn bản TVPL cho tới khi SuperAdmin chạy `legal_ingest_document <vbplID>`:
  external_id đã là vbplID, cùng KB → nhánh "same KB + other bytes → replace" có sẵn thay bằng
  HTML vbpl (có id provision và thẻ biên tập viên thật).
- Log import: số dòng feed được nhận lại.

## Phần 3 — Nạp từ vbpl không tạo bản thứ hai

`internal/application/service/legal_feed.go` (`IngestDocument` theo vbpl id, trước `IngestHTML`)

- Khi không knowledge nào có `external_id = vbplID`: tìm dòng feed cùng `number_key` với
  `detail.DocNum` mà `SameDocument` với dòng dựng từ `detail`. Có đúng một → đảm bảo có dòng
  danh mục vbplID (upsert từ `detail` nếu chưa có) → `AdoptLegalDocument(feedID, vbplID)` → tiếp
  tục `IngestHTML` (thay bản TVPL trong cùng KB; khác KB → refuse sẵn có, thông báo rõ).
- Nhiều dòng feed khớp → không tự gộp, refuse kèm danh sách.

## Phần 4 — Extension (và converter Python cho khớp)

`/home/tvpl-weknora-extension` + `scripts/tvpl/tvpl_convert.py` (`scripts/diff-python.mjs` so hai
bản).

- Metadata gửi thêm `title` (`<title>` trang, bỏ hậu tố site nếu có) và `eff_from`.
- `summarizePush` giữ `catalogue_created`; Review hiện "đã tạo dòng danh mục mới (TVPL), chờ
  SuperAdmin duyệt". 422 có `details.candidates` → hiện danh sách để chọn `legal_document_id`
  (đẩy lại với id đó).
- Cảnh báo cổng "0 dấu" đổi lời: không còn "server cần văn bản có trong danh mục".

## Kiểm thử

- Go: push tạo dòng mới (key trống), key có dòng nhưng không khớp → 422 candidates, push lại → links
  mode trên dòng đã tạo, thiếu trường → 422; chunk nguồn tvpl không có `provision_ids`/`source_url`;
  `editorHolds` với chunk không id; `ResolveTwins` feed row → vbpl survivor (+ dòng manual không
  đổi, + feed row không SameDocument → hai dòng); `AdoptLegalDocument` trên SQLite test DB
  (các bảng + unique conflicts); `IngestDocument` nhận lại dòng feed.
- Extension: vitest metadata + panel; diff-python.
- Live trên dev sau merge: push một văn bản mới thật (vd 348/2026/NĐ-CP) mode full.

## Ngoài phạm vi

- Không đổi `ResolveTwins` cho dòng placeholder/manual.
- Không tự động tải lại HTML vbpl sau khi nhận lại (SuperAdmin chạy ingest).
- Không hiện link TVPL cho người dùng cuối.

## Sau review Fable (GO with changes) — các thay đổi bắt buộc

1. (HIGH) `fp-` = sha1(docID#path) chứa id văn bản → `AdoptLegalDocument` tính lại `fp-` trong
   `legal_provision_links.{target,source}_provision_id` và candidates, gộp khi đụng unique
   `ux_legal_provision_links_edge`, sửa `link_id`.
2. (HIGH) Push với nguồn không phải vbpl không bao giờ lưu id phần tử làm provision id (dùng `fp-`);
   `editorHolds` bỏ phép so id khi id đích là `fp-`.
3. (HIGH) Trạng thái: extension/python đọc ô "Tình trạng:" của TVPL → `eff_status`; server dùng nó,
   thiếu thì "Chưa có hiệu lực" (eff_from > hôm nay) / "Chưa xác định" — **không bao giờ suy ra
   "Còn hiệu lực"**; trạng thái bắt đầu "Hết hiệu lực toàn bộ" → không nạp. Câu trả lời agent
   (check_legal_status, ghi chú hiệu lực) đánh dấu dòng feed chưa duyệt "nguồn TVPL, chưa đối chiếu vbpl".
4. (MED) Dòng danh mục tạo sau khi kiểm cấu trúc + tìm được KB đích; ingest lỗi → xoá dòng.
5. (MED) Quyết định tạo/từ chối dùng `legalcatalog.SameDocument` trên mọi dòng cùng key.
6. (MED) Adoption viết lại `detail.mark_key` của candidates (chứa AmenderDocID).
7. (MED) Importer chỉ nhận lại khi đúng một dòng feed khớp; còn lại ghi log cho SuperAdmin.
8. (MED) Thừa kế duyệt sau upsert; `source_pending` = diff; giữ `source='vbpl'`.
9. (MED) Phần 0 đóng/gắn các đề nghị mở cùng `number_key` (legal_document_id rỗng).
10. (LOW/MED) `vbplSkipsQuestionGeneration` áp dụng cho mọi nguồn feed định dạng werag-legal-html/1.
11. (LOW) Nhánh dialect jsonb_set/json_set; khoá dòng feed trong adoption (FOR UPDATE, chỉ PG);
    hook rebuild wiki catalogue cho KB bị ảnh hưởng; kiểm `Citations` trước khi bỏ; hook Phần 3 đặt
    trong `IngestHTML` (phủ portal, request, draft), dòng vbpl tra trong danh mục — thiếu thì refuse.
