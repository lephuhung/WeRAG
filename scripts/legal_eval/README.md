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
