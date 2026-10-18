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

Đăng nhập: `--token`/`--token-file`/`WERAG_TOKEN` (xem `mint_token.py`) hoặc `--email` + `WERAG_PASSWORD`. Không truyền `--kb`
thì agent dùng KB đã cấu hình. Báo cáo JSON: `results/pack-<gói>-<thời điểm>.json`; tóm tắt in PASS/FAIL theo từng ngưỡng.

| Kiểm tra (`checks`) | Đạt khi |
|---|---|
| `must_label_draft` | câu trả lời có "dự thảo"; mọi đoạn nhắc dự thảo (tiêu đề trong `draft_documents`, "Điều 20 khoản 8", "doanh nghiệp một người") có "dự thảo" hoặc "dự án luật" |
| `must_call_tool` / `must_not_call_tool` | tool được / không được gọi trong lượt |
| `must_not_cite_numbers_outside_catalogue` | mọi số hiệu trong câu trả lời ∈ `catalogue.yaml` ∪ số hiệu trong kết quả tool của lượt |
| `expect_refusal` / `expect_referral` | có lời từ chối phần trái luật / có khuyên gặp luật sư, kế toán hành nghề |
| `must_ask_clarification` | có ít nhất một dòng kết thúc bằng "?" |
| `must_match` / `must_not_match` | regex (không phân biệt hoa thường) |
| `must_state_status` | số hiệu được nhắc; nếu `check_legal_status`/`find_related_legal_documents` trả trạng thái thì đoạn nhắc số hiệu nói khớp (hết hiệu lực/bị thay thế ↔ còn hiệu lực); không có trạng thái từ tool → đạt nhưng ghi `unverified` |
| `must_have_footer` | có dấu mốc footer (chỉ chấm agent có gói) |

Phần sau dấu mốc footer không được tính cho các kiểm tra nội dung. Một câu "đạt" với một agent khi đa số lần chạy qua mọi kiểm tra.
Ngưỡng: trạng thái ≥ 95 % (lần chạy có `must_state_status`), dự thảo 100 %, footer 100 %, không bịa số hiệu 100 %,
0 câu `draft_trap`/`status`/`refusal` mà agent có gói trượt còn đối chứng đạt, độ trễ trung vị ≤ +40 %.
