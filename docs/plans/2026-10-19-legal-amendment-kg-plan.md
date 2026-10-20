# Đồ thị sửa đổi điều khoản (legal amendment KG) — kế hoạch v3

Ngày: 19–20/10/2026 · Nhánh: `feat/legal-amendment-kg` (worktree `/home/WeRAG-wt/legal-amendment-kg`) · Trạng thái: **plan v3 (20/10) — đã duyệt, đang triển khai; quyết định ở mục 10** (hiện chỉ có script đo).

## 1. Vấn đề

Chat thuế hộ kinh doanh 19/10 (bcac96e5) tính bằng ngưỡng 500 triệu đã bị Nghị định 141/2026 thay bằng 01 tỷ trong Nghị định 68/2026. Lớp lỗi chung: **điều khoản của văn bản A bị văn bản B tác động (sửa, bổ sung, bãi bỏ, thay cụm từ, không áp dụng, tạm ngưng…), mô hình vẫn đọc lời văn cũ của A**.

Hiện trạng:

| Tầng | Ở đâu | Biết gì | Thiếu |
|---|---|---|---|
| Văn bản ↔ văn bản | `legal_document_relations` (vbpl.vn, 131.117 cạnh, 5.916 `amends`) | B sửa A | không biết điều khoản nào |
| Điều khoản | `chunks.metadata.legal.amends` trên chunk của B (bộ đọc vbpl HTML) | trích lời văn mới / thay cụm từ | không có bãi bỏ, bổ sung, không áp dụng, tạm ngưng; 4.340/34.654 mục không ghi văn bản đích; ghép A bằng chuỗi lúc trả lời |

Lời văn hiện hành chỉ áp cho dạng thay cụm từ (`LegalCurrentWording`); sửa cả khoản/Điều chỉ hiện ở khối "Lịch sử sửa đổi" **sau** câu trả lời.

## 2. Số liệu đo (dev)

### 2.1 Dữ liệu
- 273.139 chunk vbpl / 10.813 văn bản; regex câu dẫn chọn 16.811 chunk / 3.398 văn bản (chunk dài nhất 5.531 ký tự; 104 chunk ứng viên nhiều Điều). 22.228 chunk pháp luật không phải vbpl (PDF/OCR).
- Danh mục: 47.316 số hiệu, 304 dạng, 27 loại, 1.258 cơ quan. 117.329 trích dẫn số hiệu đầy đủ trong chunk, 96 % khớp đúng danh mục.
- HTML gốc vbpl (`/home/orca/data/vbpl/html`, 10.991 trang) có **11.042 liên kết trích dẫn** `/van-ban/chi-tiet/<id>` (9.568 là số hiệu; 9.763 id có trong danh mục, 1.279 không) nhưng chỉ ở 478 trang (4 %); bộ chuyển đổi hiện bỏ liên kết.
- Văn bản đích trong nhãn chuẩn (887 thao tác): **78,7 % có số hiệu đi kèm thao tác**, 3,1 % số hiệu ở chỗ khác trong đoạn/tiêu đề, **16,5 % chỉ có tên, không số ở đâu** (gần như toàn luật/pháp lệnh bị luật sửa: Luật Hải quan, Trợ giúp pháp lý, Đấu thầu…), 1,8 % không xác định. Tên trùng nhiều văn bản theo thời gian (Luật An ninh mạng 24/2018 và 116/2025; Luật Đấu thầu 2005/2013/2023); cột `status` danh mục có thể lỗi thời (24/2018 vẫn "Còn hiệu lực").

### 2.2 Trích thao tác (bộ thử 200 đoạn phân tầng + bộ phát triển 160 đoạn ngẫu nhiên, nhãn tay)
"Chặt" = đúng op + điều khoản + số hiệu A + cụm từ (không tính đích phụ lục/mẫu biểu). Qwen chạy 1 luồng, không structured output, đoạn rút gọn.

| Hệ thống | 200 phân tầng: F1 · chặt P/R | 160 ngẫu nhiên: F1 · chặt P/R | Văn bản đích (160) | Đoạn rỗng gán nhầm |
|---|---|---|---|---|
| Qwen3.6-35B-A3B | 0,87 · 0,74/0,83 | **0,81 · 0,76/0,76** | 80/87 | 0/42 |
| Gemma 4 E4B (prompt) | 0,63 · 0,63/0,47 | 0,60 · 0,62/0,45 | 71/87 | 2/42 |
| Bộ đọc vbpl | 0,63 · 0,69/0,32 | 0,63 · 0,75/0,44 | 46/87 | 0/42 |

Độ chính xác chặt theo nguồn của cạnh (bộ 160): **Qwen và bộ đọc cùng tìm ra 0,99 (179/181, phủ 41 % nhãn chuẩn)**; chỉ Qwen 0,59; chỉ bộ đọc 0,14. Bỏ yếu tố văn bản đích, "chỉ Qwen" vẫn 0,62.

Dạng câu bị bỏ sót bởi regex ứng viên hiện tại: "không áp dụng … Điều … <số hiệu>" 1/26 chunk, "(tạm) ngưng hiệu lực" 8/100, "Điều … hết hiệu lực" 15/130, "tiếp tục áp dụng … <số hiệu>" 21/466.

### 2.3 Sự cố vLLM (đã xử lý)
Qwen3.6-AWQ trên vLLM 0.31 sập (CUDA illegal memory access, Xid 31) 4 lần (18/10 ×2, 19/10 ×2); 19/10 cả hai lần cùng một đầu vào (NĐ 24/2022 mục 19). Thêm `--no-async-scheduling` (20/10): đầu vào đó qua 1 + 4 song song, 323 lượt đo sau không sập, tốc độ −10 %. Không dùng structured outputs phía server cho Qwen.

## 3. Quyết định đã chốt

1. KG trong Neo4j, không bảng quan hệ chuẩn hoá (chỉ một bảng nhỏ lưu quyết định duyệt).
2. Regex chọn đoạn → trích thao tác (LLM / mô hình nhỏ) → code kiểm chứng → KG; văn bản đích chưa có → tạo yêu cầu bổ sung.
3. **Hai mức tin cậy** (20/10):
   - **Mức 1 — thay lời văn:** ít nhất hai nguồn cùng ra một cạnh **giống hệt theo quy tắc 6.1**, và đường đồng thuận × loại op đó **đã đạt cổng 6.2** trên bộ khoá kín. Chưa đường nào đạt cho tới khi đo (con số 0,99 của Qwen + bộ đọc chưa gồm ngày và phạm vi).
   - **Mức 2 — chưa xác minh:** một nguồn đã qua kiểm chứng, hoặc đường chưa đạt cổng → điều khoản mang trạng thái `UNVERIFIED` (4.12) và chịu cổng câu trả lời (4.11); vào hàng duyệt SuperAdmin; được chấp nhận → mức 1.
   - **Tạm thời (20/10, người dùng yêu cầu nối ngay):** khi chưa có nguồn Qwen, cạnh **thay/bãi bỏ cụm từ do bộ đọc tìm ra** giữ hành vi hiện nay (thay vào lời văn, ghi nguồn, mức `legacy_parser_phrase`; độ chính xác bộ đọc nhóm này 0,98) để không thụt lùi ca NĐ 68/141; cạnh sửa đổi/bổ sung có lời văn trích của bộ đọc là mức 2 (mô hình thấy lời văn mới kèm "chưa xác minh" trước khi trả lời). Bỏ chế độ tạm này khi đường Qwen + bộ đọc đạt cổng 6.2. Có công tắc `legal.current_wording.resolver`.
   - Ca "không xác định" (đích ngoài danh mục, tên ứng nhiều văn bản, Qwen chọn `none`) **không bao giờ** vào mức 1.
4. Yêu cầu bổ sung văn bản có **cờ người yêu cầu** (`user`, `domain_pack`, `amendment_graph`).
5. Mô hình trích chính: **Qwen** (Gemma prompt không đạt). Mô hình nhỏ (nhận diện số hiệu + gán cạnh) là hướng thử nghiệm ở mục 8.

## 4. Thiết kế

### 4.1 Luồng

```
chunk B ─regex ứng viên (sửa/bổ sung/bãi bỏ/thay/không áp dụng/ngưng/hết hiệu lực/tiếp tục áp dụng)─▶
  lớp thực thể (code): số hiệu (regex + chuẩn hoá + tra danh mục) · tên văn bản (tra theo ngày) ·
                       Điều/khoản/điểm · cụm từ trong ngoặc · nhãn mới · số hiệu ở tiêu đề cha / tên B
  lớp quan hệ: Qwen (JSON, chọn văn bản đích trong DANH SÁCH số hiệu/id lớp thực thể đưa vào) [+ mô hình nhỏ, mục 8]
  ─▶ code: triển khai danh sách, kiểm chứng (4.5), gộp nguồn, xếp mức ─▶
chunks.metadata.legal.amend_ops (nguồn gốc) ─▶ Neo4j (bản chiếu, dựng lại được)
                                         └▶ đích chưa có → legal_document_requests (requesters += amendment_graph)
```

### 4.2 Lớp thực thể (tất định)
- **Một bộ nhận diện số hiệu dùng chung** `FindCitations` thay ≥ 7 regex + 3 kiểu chuẩn hoá đang có (cần impact analysis): xử lý dấu cách lạc ("TT- BNNPTNT"), gạch ngang thay gạch chéo, `Ð` (U+00D0), QCVN, "QĐ-TTg" (không cắt thành "QĐ-TT"), không đọc ngày thành số hiệu ("ngày 15/11/2018"), viết tắt "NĐ 68/2026", "Luật 59/2024".
- **Sửa khoá danh mục** (ghi DB, cần duyệt): `CatalogNumberKey` gập `Ð`; 27 dòng Nghị quyết "66.x" bị lưu thành "x/…".
- **Tra tên theo ngày** (16,5 % đích chỉ có tên): văn bản cùng loại, đúng tên, đã ban hành trước B và còn hiệu lực tại ngày ban hành của B (dùng `issued_on`/`effective_on`/`expired_on`, không dựa cột `status`); khi B nằm giữa ngày ban hành và ngày hiệu lực của một luật cùng tên mới hơn, hoặc ra nhiều hơn một → mức 2 + hàng duyệt. Dùng chung với `lookupAmended`.
- **Giữ liên kết vbpl** khi chuyển HTML → chunk (`metadata.legal.citations`: khoảng ký tự + id vbpl): nhãn vàng có id cho trích dẫn và cho đánh giá.
- Qwen nhận **danh sách đóng** các văn bản ứng viên (số hiệu đã chuẩn hoá + id) từ đoạn, tiêu đề cha, tên B và tra tên; Qwen chọn, không tự viết số hiệu.

### 4.3 Thao tác
`sua_doi`, `bo_sung`, `thay_cum_tu`, `bai_bo_cum_tu`, `bai_bo`, `thay_the_van_ban`, `bai_bo_van_ban`, `dinh_chi`, **`khong_ap_dung`** (đích + điều kiện phạm vi "đối với …" + thời gian), **`tam_ngung`** (từ … đến …), **`het_hieu_luc`** (một phần), **`tiep_tuc_ap_dung`** (chuyển tiếp), `khac`.
- Phân biệt bắt buộc: "các quy định tại khoản 4 Điều 5 **của TTLT 27/2013** không áp dụng đối với …" (thao tác lên A) với "**Nghị định này** không áp dụng đối với đối tượng quy định tại … Luật Đầu tư công" (phạm vi của B, A chỉ được tham chiếu → không cạnh sửa đổi). 4/6 mẫu ngẫu nhiên là dạng tham chiếu.
- Danh sách nhiều văn bản ("không áp dụng Điều 23 của các văn bản sau: 1. …; 2. …") → mỗi văn bản một thao tác.

### 4.4 Nguồn gốc dữ liệu
- Kết quả trích (mọi nguồn) lưu ở `chunks.metadata.legal.amend_ops`: op, đích, **bằng chứng** (khoảng ký tự câu dẫn, khoảng lời văn trích ứng với đích, băm chunk B), `source`, `extractor` {model, prompt_version, at}, `rejected_reason`. Đích A theo `ProvisionIDs`/`SubdivisionRefs` khi có; bản OCR/tải lên không có định danh tin cậy → chỉ mức 2.
- `legal.amends` của bộ đọc giữ nguyên làm một nguồn.
- Bảng `legal_amendment_reviews` (edge_key, evidence_hash, decision, note, by, at); `edge_key` = băm (catalog_id B, catalog_id A, op, đích, from, to, khoảng câu dẫn); bằng chứng đổi → quyết định cũ không tự gắn; áp lúc dựng; `reject` không dựng.

### 4.5 Kiểm chứng (code)
- Đích tồn tại trong cấu trúc A (bo_sung: nhãn mới chưa tồn tại); `from` có nguyên văn trong **phiên bản A ngay trước B**; lời văn trích ghép đúng đích (nhãn đầu khối "2."/"a)"/"Điều 5." khớp); động từ câu dẫn khớp op; chủ ngữ "văn bản này" → không phải thao tác lên A; xung đột cùng phạm vi cùng ngày → đánh dấu.

### 4.6 Mô hình Neo4j (tiền tố `LEGAL_`, toàn nền tảng)
- `LEGAL_Doc {key: catalog_id | kb:<knowledge_id>, number, number_key, issuer, doc_type, issued_on, effective_on, expired_on}`; `LEGAL_Provision {key, level, dieu, khoan, diem, provision_id, chunk_id, offset}` với `HAS` lấy từ `ProvisionIDs`/vị trí, không tích danh sách; `LEGAL_DocRef {name_as_written}`.
- `AMENDS {op, from, to, new_label, after, scope, eff_from, eff_to, tier, sources, review, edge_key, src_chunk, evidence}`; `CATALOG {relation}` chép từ danh mục.
- `build_id` trên mọi nút/cạnh; dựng bản mới → kiểm tra (số cạnh, cặp NĐ 68/141) → chuyển `legal.amend_graph.active_build` → xoá bản cũ; đọc lọc theo bản hoạt động.

### 4.7 Bộ giải lời văn hiện hành (dùng chung cho lời văn, lịch sử, cổng câu trả lời)
- **Ngày tham chiếu** cho mỗi câu trả lời: mặc định hôm nay (giờ VN); câu hỏi nêu thời điểm → ngày đó (bộ tách ngày tất định; không chắc → hôm nay + nói rõ).
- Mỗi cạnh có khoảng `[eff_from, eff_to)`. `eff_from` = ngày hiệu lực của B (danh mục `effective_on` hoặc ngày riêng trong điều khoản thi hành của B). **Chỉ có ngày ban hành → ngày chưa biết** → `UNRESOLVED` (không suy diễn; cả trong đường jsonb dự phòng — bỏ fallback ngày ban hành của `phraseEffect`).
- **Văn bản sửa đổi B hết hiệu lực sau đó:** lời văn B đã đưa vào A **giữ nguyên**, trừ khi có cạnh mới hơn tác động cùng phạm vi (sửa lại, bãi bỏ, khôi phục lời văn cũ bằng thao tác rõ ràng) — đó là các cạnh bình thường, không có quy tắc "tự sống lại". Riêng `tam_ngung` có `eff_to` của chính nó. Không lọc bỏ cạnh vì B đã hết hiệu lực (khác `phraseChanges` hiện tại).
- Giải theo thứ tự `eff_from`; kiểm chứng `from` trên phiên bản A ngay trước B **chỉ khi mọi cạnh trước đã giải**, ngược lại `UNRESOLVED`. Cùng ngày, cùng phạm vi, khác nội dung → `CONFLICT`. Đánh số lại trong cùng B áp đồng thời. Sửa của văn bản sửa đổi → lần về A.
- Kết quả theo từng điều khoản (khoản/điểm) là một **trạng thái** (4.12) + lời văn + nguồn.
- Đưa cho mô hình (`model_content`, `model_match`, `content_head` đồng bộ; bản lưu không đổi):
  - `CURRENT_VERIFIED` (mức 1): lời văn hiện hành thay đúng phạm vi, mở "[Lời văn hiện hành theo <B>, từ <ngày>]", bản cũ một dòng "[trước <ngày>: …]"; `bai_bo` → "[… đã bị bãi bỏ theo <B>, từ <ngày>]"; `bo_sung` chèn đúng vị trí.
  - `UNVERIFIED`, `UNRESOLVED`, `CONFLICT`, `AMENDED_TEXT_MISSING`: giữ lời văn gốc nhưng bọc "[LỜI VĂN HIỆN HÀNH CHƯA XÁC MINH: lý do …]" + nguồn có thể đã sửa; cổng 4.11 áp dụng.
  - `NOT_APPLICABLE_IN_PERIOD`, `PARTIALLY_EXPIRED`, `CONDITIONALLY_NOT_APPLICABLE`: xem 4.12.
- Lời văn mới lấy từ khoảng trích đã lưu của **chunk B được phép** (4.13), không qua `expandLegalRelations` (đường đó loại văn bản hết hiệu lực — không dùng cho việc này).
- Đồ thị lỗi / không có bản hoạt động / quá hạn → mọi điều khoản pháp luật trong kết quả mang `UNVERIFIED` (lý do "chưa kiểm tra được lời văn hiện hành"); đường jsonb chỉ để gợi ý nguồn; timeout cố định (mục tiêu p95 < 150 ms).
- `LegalAmendmentHistory` dùng cùng bộ giải.

### 4.8 Yêu cầu bổ sung văn bản
Migration `000176` (000173 domain_pack_formula_overrides, 000174 drop_replaced_guest_subscriptions, 000175 sửa khoá danh mục) `requesters jsonb not null default '[]'` + backfill. `Record`: hợp tập nguyên tử ở mọi nhánh; yêu cầu từ đồ thị không ghi đè `last_*`, không tăng `request_count`, gộp `detail.amended_by[]` có giới hạn; khoá `(number_key, legal_document_id)` với id danh mục khi đã ghép. API lọc `?requester=`; UI huy hiệu + bộ lọc (en/vi).

### 4.9 Chạy hàng loạt
`cmd/legal-amend-graph` (extract | build | stats); khung đêm (`wiki.run_window`), 1 luồng (tăng sau khi chạy thử sạch), giới hạn token, ngắt mạch (5xx/từ chối kết nối → dừng 10 phút; 3 lần → dừng hẳn), thử lại chunk lỗi; không chạy cùng lúc với wiki. Ước tính 16,8 nghìn đoạn × ~3–12 s ≈ 2–5 đêm. Văn bản mới: hook sau parse → hàng đợi đêm.

### 4.10 Trang duyệt SuperAdmin
`/platform/admin/legal-amendments`: lọc theo mức/nguồn/op/văn bản; câu dẫn B, đích A, lời văn sẽ áp; Chấp nhận/Bác bỏ; danh sách lệch (danh mục có `amends` B→A mà đồ thị không có cạnh; trích dẫn tên trùng không giải được).

### 4.11 Cổng câu trả lời (code, không dựa prompt)
- **Đầu vào:** ngày tham chiếu; các điều khoản đã đưa cho mô hình, mỗi cái có id điều khoản, trạng thái (4.12), giá trị nhạy cảm trích bằng regex từ lời văn của chính điều khoản (con số, tỷ lệ, thời hạn, mức phạt), id chunk B được phép; câu trả lời nháp kèm trích dẫn `chunk_id` (đã có).
- **Quy tắc:** câu trả lời **trích dẫn hoặc nêu giá trị nhạy cảm** của một điều khoản không ở `CURRENT_VERIFIED`/`ORIGINAL_IN_FORCE` → theo 10.3: **chặn giá trị khẳng định** (code thay đoạn đó bằng câu do code sinh: "Quy định tại <điều khoản> có thể đã được sửa đổi bởi <B>; chưa xác minh được mức hiện hành — cần kiểm tra") hoặc **giữ kèm câu điều kiện do code chèn**. Câu hỏi rõ ràng về lịch sử (ngày tham chiếu quá khứ, "trước đây", "bản gốc") → cho nêu lời văn gốc có ghi nguồn và thời điểm.
- **Phát hiện:** khớp `chunk_id` trích dẫn + giá trị nhạy cảm chuẩn hoá ("01 tỷ" = "1.000.000.000" = "1 tỷ đồng") + cụm 8 từ trùng (như `answerUsesProvision`); không chỉ dò số trong văn bản.
- **Vị trí:** cùng điểm móc với khối "Lịch sử sửa đổi" (`legal_history.go` trước `closeAnswerStream`, quick-answer bus, finalize) → mọi agent.
- **Kiểm thử:** hkd-1/hkd-2 với điều khoản bị ép `UNVERIFIED`; diễn đạt lại con số; câu trả lời trộn nhiều điều khoản.

### 4.12 Trạng thái điều khoản của bộ giải
| Trạng thái | Khi nào | Đưa cho mô hình | Cổng 4.11 |
|---|---|---|---|
| `ORIGINAL_IN_FORCE` | không cạnh nào tác động tới ngày tham chiếu và danh mục không báo A bị sửa | lời văn gốc | không |
| `CURRENT_VERIFIED` | mọi cạnh tác động đều mức 1 và đã giải | lời văn hiện hành | không |
| `UNVERIFIED` | có cạnh mức 2 | gốc + bọc chưa xác minh | có |
| `UNRESOLVED` | ngày không rõ, cạnh trước chưa giải, ánh xạ danh tính (4.13) mơ hồ | như trên | có |
| `CONFLICT` | hai cạnh cùng phạm vi cùng ngày khác nội dung | các bản + nguồn | có |
| `AMENDED_TEXT_MISSING` | danh mục báo A bị sửa nhưng chunk B **không có / không được dùng / chưa được đọc đủ / không có danh sách sửa đổi**. Khi B có trong KB được phép và đã đọc đủ mà không nêu khoản này → `ORIGINAL_IN_FORCE` + tối đa một dòng thông tin mỗi văn bản (không qua cổng); B có nhắc "Điều N" của A ngoài các cạnh đã đọc → `UNVERIFIED` cho Điều đó (20/10, sau review: tránh chặn nhầm ~93 % chunk văn bản bị sửa) | gốc + "đã bị sửa đổi bởi <số hiệu B>" | có |
| `NOT_APPLICABLE_IN_PERIOD` | `tam_ngung` phủ ngày tham chiếu | "[Tạm ngưng hiệu lực từ … đến … theo <B>]" + gốc | có |
| `PARTIALLY_EXPIRED` | `het_hieu_luc` (một phần) từ ngày X, **không có ngày kết thúc** (khác tạm ngưng) | "[Hết hiệu lực từ … theo <B>]" | có |
| `CONDITIONALLY_NOT_APPLICABLE` | `khong_ap_dung` có điều kiện đối tượng/phạm vi | gốc + "[Không áp dụng đối với <điều kiện> theo <B>]" + kết quả so điều kiện với câu hỏi `match`/`no_match`/`unknown` (so khớp tất định; mặc định `unknown`) | `match`/`unknown` → có |

### 4.13 Danh tính và phạm vi truy cập
- Chunk A truy xuất được (bản sao trong một KB) → id danh mục qua `knowledges.metadata.source_doc_id` **và** khớp `doc_number`; điều khoản qua `ProvisionIDs`; thiếu/mơ hồ/không khớp → `UNRESOLVED`. Đo tỷ lệ ánh xạ được trên toàn kho trước khi bật.
- **Lời văn B chỉ được chèn khi chunk B nằm trong phạm vi tìm của câu hỏi** (KB của lượt tìm hoặc KB công khai nền tảng — 10.2), kiểm tra quyền trên **đúng chunk** trước khi đưa vào `model_content`; ngược lại `AMENDED_TEXT_MISSING`.

## 5. Các bước (Opus viết, Fable review mỗi bước, sửa trước bước sau)

1. ✅ Đo: script `scripts/legal_amend_kg/amend_eval.py`, bộ 200 + 160 nhãn tay, so Qwen/Gemma/bộ đọc, xử lý sự cố vLLM.
2. **Lớp thực thể** (4.2): `FindCitations`, tra tên theo ngày, giữ liên kết vbpl, kiểm tra ánh xạ danh tính (4.13); unit test. Sửa khoá danh mục chỉ sau 10.1. — **được bắt đầu**.
3. **Bộ giải + kiểm chứng + trạng thái + ngày tham chiếu** (4.5, 4.7, 4.12) trên dữ liệu đọc từ Postgres; test từng op, chuỗi, B hết hiệu lực, xung đột, ca không xác định. — **được bắt đầu**.
4. **Cổng câu trả lời** (4.11): cài ở chế độ chỉ ghi log; hành vi cuối theo 10.3.
5. Neo4j phần khung: lược đồ, `build_id`, dựng/chuyển nguyên tử, đọc lọc theo bản hoạt động (4.6).
6. Yêu cầu bổ sung + cờ người yêu cầu (4.8).
7. Bảng `legal_amendment_reviews` + trang duyệt SuperAdmin + danh sách lệch (4.10).
8. Trích bằng Qwen prompt v2 (danh sách đóng + `none`, op mới) → đo trên bộ khoá kín + bộ "không áp dụng" → cổng 6.2 theo đường × op.
9. Chạy đêm toàn kho → `amend_ops`; hook sau ingest.
10. Dựng đồ thị đầy đủ → kiểm NĐ 68/141, tên trùng, số `DocRef`.
11. (Phần nối đã kéo lên bước 3 theo yêu cầu người dùng 20/10; bước này còn: nối dữ liệu Qwen/đồ thị) Nối vào `LegalCurrentWording`, `applyCurrentWording`, `LegalAmendmentHistory`, cổng 4.11; eval đầu-cuối.
12. Bật theo từng đường × op đã đạt cổng (mặc định mọi thứ ở mức 2).
13. (Thử nghiệm, mục 8) mô hình nhỏ.

## 6. Đánh giá và điều kiện bật

### 6.1 Quy tắc "cạnh giống hệt"
Hai nguồn trùng khi khớp **tất cả**: id danh mục A và B (không phải chuỗi số hiệu); điều khoản (điều/khoản/điểm chuẩn hoá; `bo_sung` gồm cha + nhãn mới + `after`); op; lời văn thay thế (`sua_doi`/`bo_sung`: băm khoảng trích sau chuẩn hoá khoảng trắng; cụm từ: `from`/`to` sau chuẩn hoá Unicode NFC + khoảng trắng); khoảng hiệu lực (`eff_from` bằng nhau); phạm vi/điều kiện (văn bản điều kiện chuẩn hoá bằng nhau). Trường do **nguồn dùng chung** cung cấp (vd. ngày lấy từ danh mục cho cả hai) không tính là bằng chứng đồng thuận; thiếu ở bất kỳ nguồn → không trùng.

### 6.2 Cổng bật mức 1
- **Bộ thử khoá kín mới** (~300 đoạn ngẫu nhiên chia theo văn bản B, chưa dùng thiết kế; nhãn đủ các trường ở 6.1) + bộ ~60 đoạn "không áp dụng/tạm ngưng/hết hiệu lực/tiếp tục áp dụng" (đo cả nhầm "văn bản này" thành thao tác). Bộ 160 là bộ phát triển.
- Mỗi (đường đồng thuận × op): độ chính xác theo 6.1 với **cận dưới khoảng tin cậy 95 % (Wilson) ≥ 0,97**, tối thiểu 30 cạnh; không đủ mẫu → giữ mức 2. Báo thêm số cạnh đúng thêm và số lần thay lời văn sai thêm so với hiện trạng.
- Đầu-cuối: hkd-1/hkd-2 + ≥ 3 câu hỏi có đáp án con số cho mỗi op; ngày tham chiếu quá khứ; chuỗi sửa; B hết hiệu lực; tên trùng (An ninh mạng 2018/2025); điều khoản `UNVERIFIED` phải qua cổng 4.11; kiểm tra chính `model_content`.
- p95 độ trễ `search_knowledge` khi có đồ thị và khi Neo4j tắt; thử chuyển bản dựng nguyên tử.

## 7. Rủi ro
- Lời văn sai ở mức 1 → câu trả lời sai số: kiểm soát bằng cổng 6.2; điều khoản chưa xác minh qua cổng 4.11.
- Tên trùng/danh mục lỗi thời → tra tên theo ngày, không theo `status`; ca mơ hồ vào hàng duyệt.
- Đích phụ lục/mẫu biểu viết tự do: chỉ lưu dạng chữ, không áp vào lời văn.
- vLLM Qwen: theo dõi Xid sau `--no-async-scheduling`; ngắt mạch.
- Neo4j trên đường trả lời: timeout + fallback có ghi chú.

## 8. Hướng thử nghiệm: mô hình nhỏ (sau lộ trình chính)
- **Nhận diện số hiệu** (bộ mã hoá cỡ base gắn nhãn token) huấn luyện từ 117 nghìn trích dẫn khớp danh mục + 11 nghìn liên kết vbpl + biến thể sinh từ 47 nghìn số hiệu (viết tắt, gạch, dấu cách, `Ð`, lỗi OCR, thiếu năm) + mẫu âm (ngày, tiền, "khoản 2"). Trên văn bản sạch regex + chuẩn hoá đã ~100 % (285/285 đích) → chỉ dùng nếu bằng regex trên văn bản sạch và hơn trên văn bản nhiễu (đo trên 3 tập: sạch, nhiễu tổng hợp, OCR thật 22 nghìn chunk).
- **Gán cạnh** (phân loại cặp thực thể có đánh dấu, hoặc so với Gemma E4B fine-tune LoRA sinh JSON): chỉ chọn trong thực thể lớp 4.2 tìm ra → không bịa số hiệu. Dữ liệu: nhãn vàng + nhãn bạc từ Qwen∩bộ đọc (kiểm tra tay căn đoạn), chia theo văn bản; lưu ý lỗi tương quan khi học từ nhãn bạc.
- Thí nghiệm bác bỏ rẻ trước; đường đồng thuận mới (Qwen+mô hình nhỏ, bộ đọc+mô hình nhỏ) chỉ vào mức 1 khi tự đạt mục 6. GPU: .241 (~7,5 GB trống).

## 9. Lịch sử phản biện
- Codex 19/10 (9 điểm: thay đúng phạm vi, khoảng hiệu lực, khoá theo id + bằng chứng, kiểm chứng theo phiên bản, dựng nguyên tử, đồng bộ model_content, hợp tập requesters, bộ thử không thiên lệch, an toàn tải) — đã đưa vào 4.x/6.
- Codex 20/10 về mô hình NER (đặt bài toán có liên kết, không coi là phiếu độc lập, dữ liệu, chọn mô hình, thí nghiệm bác bỏ, siết điều kiện bật) — đã đưa vào 3, 6, 8.
- Codex 20/10 phản biện plan v2: 5 chặn + 3 nên sửa → 3.3, 4.7, 4.11–4.13, 5, 6.
- Codex 20/10 phản biện plan v2.1: chặn còn lại (quy tắc so khớp, fallback `phraseEffect`/`phraseChanges`, cổng câu trả lời chưa đặc tả, trạng thái op không đổi lời văn, danh tính/quyền chunk B, gộp mục 10 vào thân plan) → plan v3; kết luận: được bắt đầu bước 2–3.

## 10. Quyết định của người dùng (20/10)
- **10.1 Sửa khoá danh mục: ĐƯỢC PHÉP.** Cách làm (theo Codex): bản chạy thử ra diff → áp dev → live qua migration riêng (kiểm tra va chạm khoá, có down/đường lùi).
- **10.2 Lời văn B ngoài phạm vi tìm: ĐỒNG Ý đề xuất** — chỉ dùng lời văn B từ KB trong phạm vi câu hỏi hoặc KB công khai nền tảng; ngoài đó chỉ nêu số hiệu B, trạng thái `AMENDED_TEXT_MISSING`.
- **10.3 Điều khoản chưa xác minh: ĐỒNG Ý đề xuất** — cổng 4.11 **chặn giá trị khẳng định** (con số/quy định hiện hành) từ điều khoản không ở `CURRENT_VERIFIED`/`ORIGINAL_IN_FORCE`; chỉ cho nêu lời văn gốc có ghi nguồn khi câu hỏi rõ ràng về lịch sử.
- Triển khai: người dùng cho phép (20/10), code do subagent viết, Fable review từng bước.
