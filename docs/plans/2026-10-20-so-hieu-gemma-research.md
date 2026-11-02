# Nhận dạng và chuẩn hoá số hiệu văn bản: có nên fine-tune Gemma 4 E4B? — nghiên cứu

Ngày: 20/10/2026 · Phạm vi: chỉ đọc mã và CSDL (SELECT), phân tích offline; không huấn luyện, không gọi máy chủ LLM ở 10.10.0.240.
Script và dữ liệu trung gian: `/tmp/claude-999/-home-WeRAG/5baf824c-f2b4-432c-b461-a984c1ff7cd2/scratchpad/sohieu/` (bộ chạy Go `goh/` dùng bản sao nguyên văn của `internal/vietnamese_legal`, `proto.py` = nguyên mẫu bộ chuẩn hoá tất định, `chunkstat.py`, `unmatched.py`, `namestat.py`, `protorun.py`).

Quy ước: **[đo]** = số đo trên dữ liệu dev ngày 19–20/10; **[ước]** = ước lượng, chưa kiểm chứng.

---

## 1. Tóm tắt & khuyến nghị

**Khuyến nghị: chưa fine-tune Gemma cho số hiệu.** Hãy làm một bộ nhận dạng tất định duy nhất (regex + chuẩn hoá + tra danh mục) thay cho 6–7 regex rời rạc hiện nay, sửa khoá danh mục, và thêm bước gắn tên-không-số với danh mục. Chỉ dùng LLM cho phần còn lại khó: chọn văn bản đích theo ngữ cảnh, việc này nằm sẵn trong bước trích quan hệ của KG. Chỉ xem lại chuyện fine-tune khi có một bộ thử **văn bản thật có nhiễu** (OCR, tài liệu người dùng) mà phần sót sau khi đã có bộ tất định còn trên khoảng 5 %.

Lý do, theo số đo:

1. **Hình dạng số hiệu gần như đã giải được bằng regex.** Trong 285 văn bản đích có số hiệu mà người gán nhãn ghi trong bộ chuẩn KG, regex hiện tại lấy đúng khoá ở 268 (94,0 %) [đo]. 17 ca sót thuộc ba lớp: dấu cách lạc "TT- BNNPTNT" (10), QCVN (6), QTKĐ (1). Nguyên mẫu tất định (`proto.py`, khoảng 40 dòng) lấy đúng **285/285** [đo].
2. **Chỗ yếu của KG không nằm ở việc nhận dạng số hiệu.** Trong 129 cặp (đoạn, văn bản đích) của bộ giữ riêng: chỉ **45** đích có số hiệu ngay trong đoạn; **45** chỉ có ở `heading_path`/tiêu đề (đích thừa hưởng từ Điều/Mục cha); **8** không có ở đâu; **20** chỉ có tên ("Luật Đấu thầu"); **11** không ghi đích [đo]. Như vậy khoảng 57 % ca đòi hỏi suy theo ngữ cảnh hoặc gắn tên với danh mục. Một mô hình chuẩn hoá số hiệu không chạm tới phần này. Kết quả Gemma 71/87 so với bộ đọc 46/87 (số do người giao việc cung cấp) phản ánh khả năng đọc ngữ cảnh, không phản ánh khả năng nhận dạng mẫu.
3. **Trên toàn kho vbpl**, 117.329 lượt trích số hiệu đầy đủ (đếm khác nhau trong từng chunk) khớp đúng khoá danh mục **96,0 %** [đo]. Nguyên mẫu tất định nâng lên **96,8 %** (+956 lượt) [đo]. Phần sót còn lại chủ yếu là văn bản **không có trong danh mục**: khoảng 3.660 lượt, 3,1 % [đo]. Không mô hình nào gắn được những văn bản này; cần bổ sung danh mục (luồng yêu cầu văn bản).
4. **Câu hỏi chat thật hầu như không có nhiễu số hiệu.** Trong 904 câu hỏi khác nhau (phần lớn do bộ thử sinh ra) có: 0 ca không dấu ("nghi dinh"), 0 ca dấu cách lạc, 0 ca dạng "68-2026-NĐ-CP", 7 ca "NĐ 30" [đo]. Lỗi thật trong chat là **lỗi mã**, ví dụ "1461/QĐ-TTg" bị cắt thành "1461/QĐ-TT" ở 17 câu [đo]. Lỗi này sửa bằng một dòng regex.
5. **Fine-tune có chi phí và rủi ro thật.** Thẻ A6000 trên .240 đang ở mức 45,9/49,1 GB; FP8 Marlin của E4B từng làm sập engine khi thẻ chỉ còn dưới 2–3 GB trống. Qwen trên cùng thẻ vừa sập hai lần và đang bảo trì. Dữ liệu nhiễu phải tự sinh bằng luật, và một bộ sinh nhiễu đã viết được bằng luật thì phần lớn cũng đảo ngược được bằng luật (xem §4).

Nếu vẫn muốn có mô hình (bước 3 trong §7): dùng **LoRA riêng** trên cùng nền gemma-4-E4B-it (vLLM multi-LoRA, `--max-loras 2`), không gộp vào adapter chính tả v4. Đầu ra là JSON gồm khoảng chữ, khoá chuẩn hoá và loại văn bản. **Code** làm phần gắn với danh mục.

---

## 2. Hiện trạng: các bộ nhận dạng trong `/home/WeRAG`

| Nơi | Regex / hàm | Phủ | Không phủ |
|---|---|---|---|
| `internal/vietnamese_legal/document_number.go` | `documentNumberRe`, `FindDocumentNumbers`, `FindDocumentNumberIndex` | số/năm/mã có chữ ("53/2022/NĐ-CP", "104/2016/QH13"); bỏ qua ngày tháng; bỏ khớp dính vào số dài hơn | dấu cách quanh `/` hoặc `-`; "NĐ - CP"; "Ð" (U+00D0); số chấm "66.17/2026/NQ-CP" (đọc thành "17/2026/NQ-CP"); OCR "2O22"; năm 2 chữ số |
| cùng tệp | `administrativeNumberRe`, `CitedNumberSpans` | số không năm "123/QĐ-UBND", "45/BTC-TCT" | **mã chỉ chữ hoa**: "QĐ-TTg" bị cắt thành "QĐ-TT"; dạng Đảng "36-HD/VPTW", "05-CT/TW" |
| cùng tệp | `IsNormativeDocumentNumber` | lọc mã văn bản quy phạm (NĐ, TT, QĐ, NQ, QH…) | — |
| `catalog.go` | `CatalogNumberKey` | NFC, hoa, bỏ khoảng trắng, Đ→D, gộp các loại gạch | **không gộp "Ð" (U+00D0)**; không xử lý ký tự Kirin giống Latin |
| `entity.go` | `DocNumberPattern`, `NormalizeDocumentNumber` | khoá nút Document của đồ thị | chỉ viết hoa và gộp khoảng trắng, **không** Đ→D, nên khoá đồ thị khác khoá danh mục |
| `metadata.go` | `ParseReference` (docNumFullRe/ShortRe/SoRe, năm, số trần, loại, cơ quan) | tham chiếu tự do "Thông tư 15 của Bộ Công an" | lấy nhầm ngày làm số ("ngày 15/11/2018" → "11/2018"); "0912/345" → số; "53/2022 NĐ-CP" → "53/2022" |
| `agent/tools/legal_candidates.go` | `bareLegalRefRe`, `ParseBareLegalReference` → thẻ `legal_candidates` | "Nghị định 53", "Thông tư số 01" | **không nhận "NĐ 30", "TT 15"**; "Nghị định 68/2026" (có năm, thiếu mã) không vào thẻ nào; "Nghị định 53 /2022/NĐ-CP" bị coi là số trần |
| `application/service/legal_request_gate.go` | dùng `FindDocumentNumberIndex` + `Lookup` | gate yêu cầu văn bản thiếu | mọi dạng `FindDocumentNumbers` sót |
| `application/service/legal_amendment_history.go` | `legalDocNumberRe` (`\d+/\d{4}/[A-Za-zĐđ0-9\-]+`), `amendedNumber` → `lookupAmended` (theo quan hệ, rồi theo tiêu đề và ngày) | đích sửa đổi theo số, hoặc theo tên đúng nguyên văn tiêu đề | chữ thường/Đ, dấu cách; tên rút gọn khác tiêu đề danh mục |
| `application/service/legal_validity_note.go` | `legalDocNumberRe` | ghi chú hiệu lực | như trên |
| `application/service/session_legal_catalog.go` | `DocNumberPattern` | gợi ý danh mục vào prompt | như trên |
| `internal/vbpl/amend.go`, `phrase_amend.go` | `amendedDocRe`, `phraseSpacedNumberRe` (đã vá "TT- B"), `fileNumberRe` | bộ đọc vbpl | trùng lặp logic của gói `vietnamese_legal` |
| `scripts/legal_amend_kg/amend_eval.py` | `num_key` | chấm điểm | — |

Nhận xét: có **ít nhất 7 regex số hiệu khác nhau**, mỗi cái vá một kiểu riêng (ví dụ `phraseSpacedNumberRe` chỉ có trong bộ đọc cụm từ), và **ba kiểu khoá** khác nhau: `CatalogNumberKey`, `NormalizeDocumentNumber`, `num_key`. Nhiều lỗi dưới đây đến từ chỗ phân mảnh này, không đến từ độ khó của bài toán.

### 2.1 Các lớp lỗi cụ thể (đều là ví dụ thật hoặc chạy thật qua bản sao mã)

Chạy 46 cách viết qua bản sao nguyên văn của gói (`goh/h`, kết quả trong `variants.out`):

| Lớp | Ví dụ | Hiện tại | Tần suất thật [đo] |
|---|---|---|---|
| Dấu cách lạc trong mã | "01/2020/TT- BNNPTNT", "36/2017/NĐ - CP" | cắt còn "01/2020/TT", rồi tra tiền tố danh mục ra hàng chục văn bản | 937 lượt trong chunk vbpl; 10/285 đích trong bộ chuẩn |
| Mã có chữ thường | "1461/QĐ-TTg" (không năm) | `CitedNumberSpans` cắt còn "QĐ-TT" | 17/904 câu chat |
| "Ð" U+00D0 (Eth) | "25/1999/QÐ-BTC" | regex cắt còn "N"/"Q"; khoá không gộp | **750 dòng danh mục** có Ð trong `number_key` (392 trùng với một khoá D) |
| Số chấm | "66.17/2026/NQ-CP" | đọc thành "17/2026/NQ-CP" | 99 chunk; **danh mục cũng lưu sai** (27 dòng "Nghị quyết số 66.x" có `document_number` = "12/2026/NQ-CP"…), trùng khoá với Nghị quyết 12/2026 thật |
| QCVN / TCVN | "QCVN 06:2022/BXD" | không nhận | 2.201 chunk có "QCVN"; danh mục chỉ 59 dòng nhắc QCVN (QCVN ban hành kèm Thông tư, cần tra theo tiêu đề) |
| Dạng Đảng | "36-HD/VPTW", "05-CT/TW" | không nhận | khoảng 1.300 lượt "…-NQ/TW" trong chunk [đo bằng nguyên mẫu] |
| Thiếu mã hoặc thiếu năm | "NĐ 68/2026", "Nghị định 53/2022 NĐ-CP", "Luật 59/2024" | không vào gate yêu cầu, không vào thẻ ứng viên | chat: 1 ca "Nghị định 86/2013"; kho vbpl: hiếm |
| Viết tắt loại | "NĐ 30" | thẻ ứng viên không nhận | 7 câu chat (có lẽ nói về thể thức NĐ30, không phải hỏi văn bản) |
| Ký tự Kirin giống Latin | "100/2015/NĐ-СР" (С, Р Kirin) | cắt còn "NĐ" | vài ca trong chunk (OCR/nhập liệu) |
| OCR chữ/số | "53/2O22/NĐ-CP" | lấy nhầm "22/NĐ-CP" | chưa thấy trong vbpl (HTML); có thể có trong bản nháp OCR (hiện 0 hàng thật) |
| Tên không số | "Luật Giáo dục", "Bộ luật Tố tụng hình sự" | chỉ `lookupAmended`, so khớp tiêu đề nguyên văn | **53.852/66.713 (81 %)** lượt "Luật/Bộ luật …" trong chunk không kèm số; 45.160 (84 % của số đó) có tiền tố trùng tiêu đề Luật trong danh mục |
| Số trần mơ hồ | "Nghị định 53" | thẻ ứng viên (đúng thiết kế) | danh mục: 86 % số Nghị định trần ứng với hơn 1 văn bản, trung bình 12,7, tối đa 38; "NĐ 53" 30 văn bản, "NĐ 68" 32 |
| `ParseReference` lấy ngày | "ngày 15/11/2018" → "11/2018" | dương tính giả | — |

---

## 3. Dữ liệu & số đo

### 3.1 Danh mục `legal_documents` [đo]

- 47.316 dòng; 42.450 `number_key` khác nhau; 27 loại; 1.258 cơ quan ban hành.
- Loại: Thông tư 17.318, Quyết định 13.452, Nghị định 5.045, TTLT 3.472, VBHN 2.459, Nghị quyết 1.354, Chỉ thị 1.085, Sắc lệnh 980, Công văn 709, Luật 638, …
- **304 dạng số hiệu** (thay chữ số bằng 9, chữ cái bằng A). Năm dạng đầu: `9/9/A-A` 29.853, `9/A-A` 5.524, `9/A` 1.673, `9-A/A` 1.527, `9/9/A-A-A` 1.224. Có 33.857/47.316 (71,6 %) dòng đúng dạng `documentNumberRe`; phần còn lại là số không năm, văn bản cũ trước 1990 ("136-TTg", "69-SL"), "Không số" (394 dòng), 151 dòng kết thúc bằng gạch ("16/2007/TTLT-BLĐTBXH-"), 1.417 dòng có khoảng trắng bên trong.
- **Gần như chỉ có văn bản trung ương**: chỉ 130 dòng mang "UBND" (77 Hà Tĩnh, 37 Nghệ An) và 28 dòng mang "HĐND". Ví dụ "Quyết định 12/2025/QĐ-UBND" (nhiều tỉnh) hầu như không gắn được với danh mục hiện có. Trong chat, 29/290 câu có số đầy đủ không khớp danh mục, chủ yếu là QĐ-UBND, NQ-HĐND địa phương.
- **Mơ hồ:** 2.544 khoá có hơn 1 dòng (7.410 dòng). 717 khoá có hơn 1 cơ quan, phần lớn là Luật QHxx kèm Lệnh công bố, VBHN cùng số (394 khoá có VBHN). Bộ (số, năm, loại) nhưng khác cơ quan: 4.623/17.095 nhóm. Ví dụ "01/2020/TT" thiếu đuôi cơ quan thì có trung bình nhiều ứng viên.
- `legal_document_relations`: 131.117 cạnh (based_on 69.809, guides 21.211, references 16.158, repeals 7.104, replaces 6.658, amends 5.916, …).

### 3.2 Chunk vbpl (281.605 chunk / 10.870 văn bản) [đo]

- 51.869 chunk có ít nhất 1 số hiệu đầy đủ; **117.329 lượt** (khác nhau trong từng chunk), trong đó **112.648 (96,0 %)** khớp đúng `number_key` danh mục; 14.881 là tự trích (số của chính văn bản).
- 18.296 khoá danh mục khác nhau được trích, trong 10.739 văn bản; trung vị 2 lượt/khoá; 8.367 khoá chỉ xuất hiện một lần.
- 4.669 lượt không khớp, phân loại tự động:
  - 3.179: có văn bản cùng số/năm nhưng khác đuôi trong danh mục. Đa số là văn bản thiếu trong danh mục; một ít là lỗi gõ ngay trong nguồn: "24/2018/TT-TNMT" (thiếu B), "05/2006/QĐBTNMT".
  - 479: không có số/năm nào khớp, tức văn bản thiếu hẳn ("14/2017/NĐ-CP", "164/2017/NĐ-CP"…).
  - 937: bị cắt do dấu cách/gạch.
  - 74: đuôi bị tách bằng "/" ("44/2007/TTLT/BTC-BKH&CN") hoặc có ký tự Kirin.
- 8.721 lượt số không năm (dạng hành chính), 1.476 khớp danh mục.
- Tên không số: xem §2.1 (81 % lượt nhắc Luật không kèm số).
- Kiểu dạng "Nghị định 53" (loại + số trần, không "/") trong văn bản chính thức: chỉ 1.312 lượt trên toàn kho. Văn bản chính thức hầu như luôn ghi số đầy đủ.
- Nguồn ví dụ âm (để huấn luyện hoặc kiểm thử): 8.824 chunk có ngày dạng dd/mm/yyyy, 7.746 có số tiền dạng "1.000.000", 182.693 có "Điều N".

### 3.3 Chat (`messages`) [đo]

- 3.994 tin người dùng, **904 nội dung khác nhau**. Theo tổ chức: 757 thuộc 10000 (nền tảng/SuperAdmin, phần lớn là bộ thử), 142 thuộc 10003 (tenant thử), còn lại lẻ tẻ. Không đủ làm dữ liệu huấn luyện. Phần lớn câu do bộ thử sinh nên **lệch về dạng sạch**.
- 290 câu có số đầy đủ (261 khớp danh mục); 105 câu có số không năm; 14 câu chỉ có "loại + số trần", 13 trong đó được thẻ ứng viên bắt; 7 câu "NĐ 30"; 27 câu nêu tên Luật không số; 17 câu bị cắt "QĐ-TTg".
- Câu chat ví dụ (đã ẩn danh): "Theo Quyết định 2065/QĐ-TTg, Bộ Xây dựng có t…", "Nghị định 361 căn cứ vào những văn bản…", "…Điều 5 Nghị định 13-CP, hình thức hợp đồng…".
- `legal_document_requests`: 4 hàng. `legal_document_drafts` (OCR) chưa có dữ liệu thật đáng kể. **Không có kho văn bản OCR có nhiễu để đo**, đây là lỗ hổng chính của nghiên cứu.

### 3.4 Bộ chuẩn KG (đánh giá ngoài) [đo]

- `scratchpad/akg/hgold.jsonl` (bộ giữ riêng, 160 dòng) và `scripts/legal_amend_kg/data/gold-2026-10-19.jsonl` (200 đoạn): 346 cặp (đoạn, `doc`), trong đó 285 có số hiệu, 49 chỉ có tên, 12 rỗng.
- Lấy khoá từ chính chuỗi `doc`: hiện tại 268/285; nguyên mẫu tất định 285/285.
- Khoá đích có trong các khoảng số hiệu của đoạn: 167/285. Số hiệu đầu tiên trong đoạn trùng đích: 118/285. Số lượng số hiệu khác nhau trong đoạn: 0 (93 đoạn), 1 (84), 2 (32), 3 (27), 4 (7), ≥5 (42). Bài toán thật là **chọn đúng** trong nhiều số hiệu, hoặc **thừa hưởng** từ ngữ cảnh.

### 3.5 Nguyên mẫu tất định (`proto.py`) [đo]

Gộp homoglyph (Ð, Kirin), cho phép khoảng trắng quanh `/` và `-`, nhận số chấm, dạng "68-2026-NĐ-CP", sửa O→0 trong năm, nhận QCVN/TCVN/QTKĐ, dạng Đảng, mã có chữ thường (QĐ-TTg), và loại + số (+năm) cả khi viết tắt NĐ/TT/QĐ/NQ:

- 46 cách viết thử: đúng 43. Còn sót: "53/2O22" lọt sang nhánh số không năm, "53/22/NĐ-CP" (năm 2 chữ số), "Nghi dinh 100/2019" (không dấu). Ca không dấu nên nhờ bước chuẩn hoá dấu của gate chính tả hoặc bảng loại không dấu.
- Kho vbpl: 112.660 → **113.616** lượt khớp danh mục (+956). "Mất" 216 lượt, phần lớn là số 66.x mà danh mục lưu sai, tức nguyên mẫu đúng còn danh mục sai.
- Rủi ro chính xác cần chặn: "pháp luật 1." bị nhận thành loại "Luật" + số trần (thấy trong mẫu). Cần ranh giới từ chặt hơn và đặt tên loại sau dấu câu hoặc đầu câu.

---

## 4. Phương án huấn luyện (nếu làm)

### 4.1 Định dạng nhiệm vụ

| Phương án | Đầu ra | Ưu | Nhược |
|---|---|---|---|
| A. Trích khoảng + chuẩn hoá (khuyên dùng nếu làm) | `{"refs":[{"text":"NĐ 68/2026","number":"68/2026/NĐ-CP"\|null,"partial":{"type":"Nghị định","no":"68","year":"2026"},"name":null,"start":…}]}`; code tra danh mục | mô hình nhỏ, nhãn kiểm chứng được bằng code; danh mục đổi không cần huấn luyện lại | mô hình vẫn phải "đoán" đuôi khi thiếu, vốn là việc của danh mục |
| B. A + mã danh mục (`legal_documents.id`) | id | một bước | phải thuộc lòng 47k id; danh mục đổi liên tục (feed vbpl); ảo giác id |
| C. Gán nhãn token (BIO) bằng mô hình mã hoá nhỏ (PhoBERT/ViSoBERT) + chuẩn hoá bằng code | khoảng | nhanh, CPU được, không tốn VRAM .240 | cần hạ tầng phục vụ mới; mô hình mã hoá không suy được dạng thiếu |
| D. Không huấn luyện: prompt Gemma nền hoặc Qwen chỉ cho phần dư | như A | 0 chi phí huấn luyện | Gemma nền dễ hỏng JSON (31/200 ca JSON bị cắt trong thử KG) |

Không nên để mô hình **sinh** số hiệu đầy đủ khi văn bản không ghi. Quy tắc "không tự đoán số hiệu" đã có trong prompt KG, và ảo giác số hiệu ở đây gây hại nhiều hơn là bỏ trống.

### 4.2 Sinh cặp huấn luyện

1. **Nhãn yếu.** 112.648 lượt trích khớp danh mục trong 51.869 chunk [đo]. Lấy câu chứa số hiệu làm đầu vào, khoá danh mục làm đích. Thêm tên không số đã khớp tiêu đề Luật (45.160 lượt [đo]) làm ví dụ `name` (chỉ trích, không gắn số).
2. **Tăng cường bằng luật** [ước tỉ lệ]: viết tắt loại (NĐ/TT/QĐ/NQ/TTLT), bỏ đuôi, bỏ năm, đổi "/"↔"-", thêm khoảng trắng, đổi hoa/thường, Đ↔D↔Ð, homoglyph Kirin, OCR (0/O, 1/l/I, 5/S, 8/B), lỗi gõ Telex trên loại văn bản ("nghij ddinhj"), bỏ dấu. Tỉ lệ khoảng 35 % sạch, 65 % có một biến đổi, giống cách làm spell v3/v4.
3. **Ví dụ âm**: ngày, số tiền, "Điều 5 khoản 2", số điện thoại, mã hồ sơ, "Mẫu số 01/GLP", số trong bảng. Kho có hàng nghìn ví dụ [đo §3.2]. Thêm số hiệu **tự trích** (văn bản tự nhắc đến mình), tuỳ nhiệm vụ có cần đánh dấu hay không.
4. **Cỡ dữ liệu** [ước]: 15–25k mẫu là đủ (v3.1/v3.2 sửa được hành vi với 4–5k mẫu, mỗi vòng khoảng 1 giờ; v3 28k mẫu khoảng 6 giờ trên A6000 .241). Không cần dùng hết 112k.
5. **Chia train/test theo văn bản** (`external_id`), không theo chunk. Đồng thời giữ riêng **theo văn bản được trích**: khoảng 8.367 khoá chỉ xuất hiện một lần nên tách được; tránh để mô hình chỉ thuộc lòng các số phổ biến (63/2025/QH15 có 623 lượt).

### 4.3 Bộ đánh giá

- Bộ ngoài: 285 `doc` có số và 49 `doc` chỉ tên từ bộ chuẩn KG; phải bỏ khỏi dữ liệu huấn luyện mọi chunk của các văn bản B đó.
- 46 cách viết ở §2.1 (`variants.jsonl`), mở rộng thành khoảng 300 cách bằng tay.
- 904 câu chat (dạng sạch, kiểm tra tỉ lệ dương tính giả).
- **Còn thiếu và cần làm trước:** khoảng 200 đoạn thật có nhiễu (OCR từ `legal_document_drafts` khi có, văn bản người dùng trong `document_workspace_texts`), gán nhãn tay.
- Đường cơ sở: (1) regex hiện tại; (2) bộ tất định mới + danh mục; (3) Gemma nền có prompt; (4) Qwen (chỉ khi .240 ổn định, chạy 1 luồng ban đêm). Chỉ số: P/R theo khoảng, độ chính xác của khoá chuẩn hoá, tỉ lệ gắn đúng id danh mục (sau code), tỉ lệ ảo giác số hiệu (khoá không có trong văn bản lẫn danh mục).

### 4.4 Một adapter hay hai

- **Tách riêng LoRA.** Adapter chính tả v4 đang phục vụ dưới định dạng JSON của `check_spelling`, với chỉ số ALL .896/.862 [số liệu ghi nhớ]. Huấn luyện thêm một nhiệm vụ có nguy cơ làm giảm chỉ số này, và mỗi lần đổi một bên lại phải chạy lại đánh giá cả hai bên.
- vLLM 0.31 ở :8001 đã phục vụ dạng nền + LoRA, nên thêm adapter thứ hai (`--lora-modules sohieu=…`, `--max-loras 2`) chỉ tốn vài chục đến vài trăm MB VRAM [ước]. Tuy vậy, thẻ chỉ còn khoảng 3,2 GB trống (45,9/49,1 GB) và có tiền lệ Marlin FP8 sập khi trống dưới 2–3 GB. Muốn đổi cấu hình phải khởi động lại `vllm-spell`, và vì nó chờ `qwenawq` nên **phải chờ hết đợt bảo trì Qwen**.
- Huấn luyện trên A6000 .241 (container `spellcheck-train`, `expandable_segments`). Đây là tài nguyên dùng chung với embed/rerank/OCR.

---

## 5. Đánh giá: fine-tune có đáng không?

| Mục tiêu | Phần do nhận dạng số hiệu gây ra [đo] | Bộ tất định giải được [đo/ước] | Phần còn lại cần gì |
|---|---|---|---|
| (a) Văn bản đích KG | lỗi hình dạng: 17/285 đích (6 %) | 285/285 [đo] | chọn trong nhiều số + thừa hưởng từ `heading_path` (53/129 ca) + gắn tên-không-số (20/129): việc này thuộc bước trích của KG (LLM có ngữ cảnh) cộng code gắn tên theo tiêu đề và ngày. **Không phải** mô hình số hiệu |
| (b) Gate chat | 17 câu cắt "QĐ-TTg", 1 câu "NĐ 86/2013", 7 câu "NĐ 30" | cả ba bằng regex [ước, dựa trên nguyên mẫu] | văn bản địa phương không có trong danh mục (29/290 câu có số): thiếu dữ liệu, không thiếu mô hình |
| (c) Câu hỏi người dùng | chưa thấy nhiễu thật trong 904 câu | — | cần dữ liệu thật trước khi kết luận |
| Kho vbpl | 96,0 % khớp | 96,8 % [đo] | 3,1 % văn bản thiếu trong danh mục |

**Ước lượng lợi ích của fine-tune trên phần dư** [ước]: sau bộ tất định, phần mà một mô hình có thể cứu được là các ca không đảo ngược được bằng luật: OCR chữ/số mơ hồ, lỗi gõ loại văn bản, suy đuôi cơ quan từ "của Bộ Công an". Trên dữ liệu vbpl và chat hiện có, phần này **dưới 1 %** số lượt trích. Trên văn bản OCR hoặc tài liệu người dùng, tỉ lệ chưa đo được. Chi phí: 1–2 ngày dựng dữ liệu, 2–6 giờ huấn luyện mỗi vòng, đổi cấu hình phục vụ trên thẻ đang sát giới hạn, và phải duy trì một bộ đánh giá nữa.

Kết luận: **làm bộ tất định trước.** Nó sửa được mọi lớp lỗi đã đo, chạy dưới 1 ms, giải thích được và kiểm thử được bằng Go test. Gemma, bản nền hoặc bản fine-tune, chỉ nên chạm vào phần không suy được bằng luật, và chỉ khi phần đó được đo trên dữ liệu thật.

---

## 6. Rủi ro

- **Dương tính giả của bộ tất định khi nới regex**: "pháp luật 1.", số trong bảng, "Mẫu số 01/GLP". Cần bộ thử âm trước khi gộp.
- **Dữ liệu danh mục sai**: 66.x lưu thành số khác; 750 khoá Ð; 151 số kết thúc bằng gạch; trùng VBHN. Bộ nhận dạng đúng vẫn có thể gắn sai nếu chưa sửa khoá (cần migration hoặc backfill `number_key`, và người dùng sẽ duyệt).
- **Danh mục thiếu văn bản địa phương**: cả fine-tune lẫn regex đều không bù được. Văn bản UBND/HĐND đi qua luồng yêu cầu văn bản.
- **Nhãn yếu thiên vị dạng sạch**: 96 % lượt trích trong vbpl đã sạch, nên mô hình học từ vbpl chủ yếu học sao chép. Nhiễu hoàn toàn do bộ sinh tạo ra, nên mô hình chỉ học lại các luật nhiễu do chính ta viết.
- **Hạ tầng .240**: Qwen sập 2 lần ngày 19/10 và đang bảo trì; `vllm-spell` phụ thuộc `qwenawq`; thẻ còn khoảng 3 GB trống.
- **Ảo giác số hiệu** nếu cho mô hình sinh số đầy đủ (phương án B).
- Số 46/87 và 71/87 do người giao việc cung cấp; báo cáo này dùng cách đếm riêng (129 cặp trong `hgold`), không tái lập hai con số đó.

---

## 7. Bước tiếp theo

1. **Gộp bộ nhận dạng** (Go, `internal/vietnamese_legal`): một hàm `FindCitations(text) []Citation{Span, Raw, Key, Kind: full|admin|party|standard|partial|name, DocType, No, Year}`. Thay `documentNumberRe`, `administrativeNumberRe`, `DocNumberPattern`, `legalDocNumberRe` (×2), `bareLegalRefRe`, `num_key`, `phraseSpacedNumberRe`. Thêm vào `CatalogNumberKey`: gộp Ð và Kirin, xử lý số chấm. Phải chạy impact analysis trước khi sửa (các hàm này có nhiều nơi gọi).
2. **Sửa danh mục**: backfill `number_key` cho 750 khoá Ð; sửa 27 dòng "66.x" (document_number lấy từ tiêu đề); báo cáo 151 số kết thúc bằng gạch. Cần người dùng đồng ý vì là ghi CSDL.
3. **Gắn tên-không-số**: mở rộng `lookupAmended` thành dịch vụ dùng chung (tiêu đề chuẩn hoá, tiền tố dài nhất, chọn phiên bản theo ngày ban hành của văn bản trích, ưu tiên quan hệ `amends`/`based_on`). Đo trên 49 `doc` chỉ tên của bộ chuẩn.
4. **KG**: cho bước trích của KG nhận `heading_path` và danh sách số hiệu đã chuẩn hoá trong đoạn làm **lựa chọn đóng**. LLM chỉ chọn chỉ số hoặc "khác", không chép số hiệu. Đo lại phần văn bản đích trên bộ giữ riêng.
5. **Dựng bộ thử nhiễu thật** (khoảng 200 đoạn OCR hoặc tài liệu người dùng). Nếu phần sót sau bước 1–4 còn trên 5 %, làm LoRA `sohieu` theo §4 (phương án A, chia theo văn bản, adapter riêng, triển khai sau khi .240 ổn định).
