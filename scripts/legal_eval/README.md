# Đánh giá pháp lý từ bộ dữ liệu VLSP 2025

Hai bộ đánh giá end-to-end dựng từ bộ dữ liệu `ura-hcmut/vlsp2025-*` (HF, CC BY-NC-SA 4.0, phải xin quyền từng bộ).
Trong `data/` chỉ giữ mẫu dùng để đánh giá, không dùng để huấn luyện.

| Bộ | Agent | Câu hỏi | Đạt khi |
|---|---|---|---|
| `nli_grounding.jsonl` (300) | `builtin-document-assistant`, điều luật mở làm tài liệu nguồn, + 6 KB VBPL | nguyên câu hỏi NLI | nhãn Có: trả lời theo tài liệu; nhãn Không: không gán cho tài liệu nội dung nó không có |
| `expired_trap.jsonl` (200) | `builtin-smart-reasoning` + 6 KB VBPL | câu QA mà đáp án gốc trích văn bản đã hết hiệu lực toàn bộ (có văn bản kế nhiệm trong KB) | không trình bày văn bản hết hiệu lực như đang áp dụng |

```bash
# xuất catalogue/quan hệ/KB (TSV) rồi dựng bộ mẫu (cần pyarrow)
python build_sets.py --vlsp <dir parquet> --catalog catalog.tsv --rels rels.tsv --kbdocs kbdocs.tsv
# chạy (đăng nhập bằng tài khoản system admin để không tính hạn mức gói)
WERAG_PASSWORD=... JUDGE_BASE_URL=http://10.10.0.240:8000/v1 JUDGE_MODEL=Qwen/Qwen3.6-35B-A3B-FP8 \
  python run_legal_eval.py --set trap --email <admin> --kb <id> ... --catalog catalog.tsv --resume results/trap.jsonl
python run_legal_eval.py --set nli ... --rejudge results/nli.jsonl   # chấm lại câu trả lời đã lưu
```

Câu trả lời của mỗi câu được ghi dần ra `results/` (thư mục này không đưa lên git). Kết quả lần chạy đầu: `FINDINGS-2026-10-14.md`.

## Bộ `pack` — đánh giá gói chuyên môn (docs/domain-pack-standard.md §7)

Chạy `domain-packs/<gói>/evals/cases.jsonl` trên hai agent: agent có gói (`domain_packs=[gói]`) và agent đối chứng
giống hệt nhưng không có gói; mỗi câu chạy `--runs` lần trên mỗi agent (độ trễ lấy trung vị). Chấm bằng kiểm tra nhị phân
(regex + tool call/kết quả tool trong SSE, không dùng LLM giám khảo), mã trong `pack_eval.py`.

```bash
python run_legal_eval.py --set pack --pack opc-vn-business-law --dry-run     # kiểm tệp cases (offline)
python run_legal_eval.py --set pack --self-check                             # tự kiểm các hàm chấm (offline)
WERAG_TOKEN=$(python ../doc_assistant_eval/mint_token.py ...) \
  python run_legal_eval.py --set pack --pack opc-vn-business-law \
    --agent-id <agent có gói> --baseline-agent-id <agent không gói> --runs 3 [--kb <id> ...] [--ids opc-01,opc-02]
```

**Cặp agent quyết định ngưỡng so sánh có nghĩa hay không.** `run_suite` đọc cấu hình
hai agent qua API và từ chối chạy khi cặp không khớp `--compare`:

| `--compare` | Yêu cầu | Dùng để |
|---|---|---|
| `pack` (mặc định) | agent chính có gói, baseline **không** có, hai tập `domain_packs` lệch nhau **đúng** gói đó, **cùng** `model_id` | đo giá trị của gói |
| `model` | **cả hai** có gói, hai tập `domain_packs` **bằng** nhau, `model_id` **khác** nhau | A/B model trên cùng gói |

Lệch ở khoá cấu hình nào khác (ví dụ `allowed_tools`) thì chỉ cảnh báo, nhưng mọi
khác biệt đo được sẽ trộn cả những thứ đó vào. `--skip-pair-check` bỏ qua guard;
khi đó các ngưỡng so sánh vô nghĩa. Kết quả JSON ghi khối `pair` cho biết đã kiểm hay chưa.

Cấu hình hai agent được đọc lại sau lượt chạy: nếu ai đó sửa agent giữa chừng (băm
cấu hình đổi) hoặc agent không đọc được nữa (đã thử lại 3 lần), lượt đó thành
`INCOMPLETE`. Băm chỉ phủ chính bản ghi config, nên **không** bắt được: nội dung tham
chiếu qua id (`system_prompt_id`, `context_template_id`) bị sửa, lời công thức của gói
sửa lúc chạy mà `version` không đổi, và sửa rồi hoàn lại trong cùng lượt. Kết quả JSON
ghi tên, `domain_packs`, băm cấu hình trước/sau của từng bên, và phiên bản gói
(`pack_versions`, lấy từ `GET /api/v1/agents/domain-packs`) để về sau còn biết hai lượt
có chạy trên cùng một bản gói hay không.

`--skip-pair-check` không còn ra `OVERALL PASS`: lượt chưa kiểm cặp — hoặc phải chấm
footer/answer-block theo tên vai trò vì không đọc được cấu hình — luôn là `INCOMPLETE`
(mã thoát 2), kèm dòng `[INCOMPLETE]` nói rõ lý do. Script đọc mã thoát không nhận nhầm
một lượt chưa kiểm là lượt sạch; kết quả JSON cũng ghi `verdict` và `doubts` cho script
chỉ đọc file.

Hai ngưỡng chỉ dành cho agent có gói (`must_have_footer`, `must_have_answer_block`)
chấm theo `domain_packs` đọc từ chính agent đó, **không** theo tên vai trò. Ở
`--compare model` cả hai bên đều có gói nên cả hai cùng bị chấm; trước 2026-11-17 chúng
chấm theo vai trò, nên bên "pack" bị chấm chặt hơn baseline và số case đạt của hai bên
không so trực tiếp được (các lượt A/B model trước ngày đó lệch theo hướng có lợi cho
baseline). Dòng đầu bản tóm tắt nay nói rõ chế độ so sánh và bên nào có gói.

Trên dev: `aca20bff` = "[eval] OPC có gói" (Qwen), `a93d142a` = "[eval] OPC không gói"
(Qwen) — đây là cặp cho `--compare pack`. `28552de3` = "Tư vấn doanh nghiệp nhỏ – thuế"
(deepseek-flash, **có** gói) nên cặp `28552de3` vs `aca20bff` là `--compare model`, không
phải đối chứng gói: hai lượt chạy 2026-11-11 và 2026-11-16 dùng cặp này mà báo theo ngưỡng
pack-vs-control, nên số liệu hồi quy và độ trễ của chúng là so model dưới nhãn sai.

Lưu ý `--ids` chỉ chạy tập con: ngưỡng "không xấu hơn baseline" chỉ so các case thuộc
`draft_trap`/`status`/`refusal`, nên một lượt chỉ chạy tập con thì các ngưỡng khác không
phủ hết bộ.

Đăng nhập: `--token`/`--token-file`/`WERAG_TOKEN` (xem `mint_token.py`) hoặc `--email` + `WERAG_PASSWORD`. Không truyền `--kb`
thì agent dùng KB đã cấu hình. Báo cáo JSON: `results/pack-<gói>-<thời điểm>.json`; tóm tắt in PASS/FAIL theo từng ngưỡng.

| Kiểm tra (`checks`) | Đạt khi |
|---|---|
| `must_label_draft` | **chặn (cái người dùng đọc)**: mọi đoạn nhắc một dự thảo (tiêu đề hoặc `aliases` trong `draft_documents`, thêm "Điều 20 khoản 8", "khoản 8 Điều 20") có "dự thảo"/"dự án luật", **hoặc** khối "Lưu ý về dự thảo" do hệ thống thêm nêu đúng các dự thảo đoạn đó nhắc. Đoạn cắt như Go (dòng trống, mục danh sách, dòng bảng, tiêu đề — tiêu đề thuộc đoạn ngay sau; mục danh sách mang theo đoạn dẫn kết thúc bằng ":"), self-check đối chiếu với `internal/agent/domainpacks/testdata/paragraphs.json`. Chỉ chấm đoạn có nhắc dự thảo; câu trả lời có cần nhắc dự thảo hay không là `must_match` của case. `model_pass` = chỉ lời của model (dòng `[info]`, không chặn); `notice` = có khối lưu ý |
| `must_call_tool` / `must_not_call_tool` | tool được / không được gọi trong lượt |
| `must_not_cite_numbers_outside_catalogue` | mọi số hiệu trong câu trả lời ∈ `catalogue.yaml` ∪ số hiệu trong kết quả tool của lượt (output **và** dữ liệu có cấu trúc của sự kiện SSE, ví dụ `results[].content` của `search_knowledge` — output SSE chỉ là tóm tắt; trừ `read_file`: tài nguyên của gói không phải căn cứ) ∪ danh mục văn bản pháp luật của nền tảng (tra `GET /system/legal-catalog/search`, cần token SuperAdmin; không tra được thì tính là ngoài). Nội dung chunk của `read_document` bị lược khỏi SSE nên số hiệu chỉ đọc ở đó được xác minh qua danh mục. Báo cáo lưu `evidence_numbers`, không lưu toàn văn. Mỗi số hiệu thuộc một lớp: `evidence` (danh mục gói hoặc kết quả tool của lượt), `catalogue_only` (có thật trong danh mục nền tảng nhưng không có trong lượt — chỉ báo cáo, dòng `[info]`), `not_found` (trượt), `lookup_failed` (tra lỗi sau 3 lần thử — ngưỡng thành n/a). 401/403 tắt tra cứu cho cả lượt chạy (`legal_catalogue_lookup: disabled`, in ở đầu và trong JSON; số hiệu ngoài khi đó tính là `not_found`) |
| `expect_refusal` / `expect_referral` | có lời từ chối phần trái luật / có khuyên gặp luật sư, kế toán hành nghề |
| `must_ask_clarification` | có ít nhất một dòng kết thúc bằng "?" |
| `must_match` / `must_not_match` | regex (không phân biệt hoa thường) |
| `must_state_status` | số hiệu được nhắc; nếu `check_legal_status`/`find_related_legal_documents` trả trạng thái thì đoạn nhắc số hiệu nói khớp (hết hiệu lực/bị thay thế ↔ còn hiệu lực); không có trạng thái từ tool → đạt nhưng ghi `unverified` |
| `expect_refusal` / `expect_referral` (bổ sung) | đạt khi lời model **hoặc** khối "Lưu ý về phạm vi" do hệ thống thêm có câu từ chối / khuyên gặp luật sư; `model_pass` = chỉ lời model (dòng `[info]`) |
| `any_of` | danh sách ≥ 2 đối tượng checks; đạt khi một đối tượng đạt trọn (ví dụ orig-5: gọi `compare_tax_methods` + khối kết quả, **hoặc** hỏi lại tỷ lệ/thuế suất) |
| `must_have_answer_block` | câu trả lời có khối "Kết quả tính" do mã chèn (agent có gói; câu do mã viết thì không chấm) |
| `must_have_footer` | có dấu mốc footer (chỉ chấm agent có gói; không chấm khi câu trả lời do mã viết: câu trả lời bắt đầu bằng `final_message` của một tool/thẻ trong lượt — thẻ của cổng, thẻ chọn từ viết tắt, thẻ của tool — hoặc câu cố định "Tôi không tìm thấy căn cứ…") |

Các kiểm tra nội dung chỉ chấm phần model viết: cắt ở dấu mốc sớm nhất của khối "Lưu ý về dự thảo", "Kết quả tính" hoặc footer (self-check đối chiếu ba dấu mốc với hằng số Go); mỗi lượt ghi `answer_block` true/false. Một câu "đạt" với một agent khi đa số lần chạy qua mọi kiểm tra.
Ngưỡng: trạng thái ≥ 95 % (lần chạy có `must_state_status`), dự thảo 100 %, footer 100 %, không bịa số hiệu 100 %,
0 câu `draft_trap`/`status`/`refusal` mà agent có gói trượt còn đối chứng đạt, độ trễ trung vị ≤ +40 %, 0 lượt lỗi.
Kết luận `OVERALL`: `FAIL` (mã thoát 1) khi có ngưỡng trượt; `INCOMPLETE` (mã thoát 2) khi một ngưỡng không đo được
(không có câu nào, mọi lượt lỗi, có câu không so được với đối chứng, có lượt lỗi) — không bao giờ tính là đạt; `PASS` (0).

Số hiệu trong suite `pack` là **số hiệu đầy đủ** (số/năm 4 chữ số/ký hiệu có chữ, như `vietnamese_legal.FindDocumentNumbers` của cổng văn bản và bộ quét `cited_in`): "01/GT", "41/VPQH", ngày tháng không tính. Khoá so sánh = `CatalogNumberKey` (NFC, bỏ khoảng trắng, Đ→D, gộp gạch nối). `--self-check` đối chiếu với `internal/vietnamese_legal/testdata/document_numbers.json` (sinh lại: `UPDATE_DOCUMENT_NUMBERS=1 go test ./internal/vietnamese_legal/`).
