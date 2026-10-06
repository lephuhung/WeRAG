# Đo độ chính xác gán thành phần

Cỡ chữ/font/căn lề được chấm theo thành phần, nên độ chính xác gán thành phần
quyết định độ đúng của báo cáo thể thức. Thư mục này đo nó trên bộ văn bản có
đáp án (gold).

| Script | Việc |
|---|---|
| `gen_corpus.py` | Sinh văn bản tổng hợp kèm đáp án chính xác (9 loại × 5 kiểu header × 5 kiểu khối ký, dự thảo, độ khẩn, bảng số liệu, phụ lục, lỗi định dạng cố ý) |
| `draft_labels.py` | Gán nhãn nháp cho văn bản thật bằng LLM để người rà soát |
| `run_eval.py` | So heuristic / LLM với đáp án: độ chính xác từng unit, P/R/F1 từng thành phần, loại văn bản, % kết quả chấm trùng với chấm theo đáp án |

```bash
export DOCFORMAT_LLM_BASE_URL=http://10.10.0.240:8000/v1
export DOCFORMAT_LLM_MODEL=Qwen/Qwen3.6-35B-A3B-FP8
export DOCFORMAT_LLM_EXTRA_BODY='{"chat_template_kwargs": {"enable_thinking": false}}'

python3 evaluation/gen_corpus.py --n 60                      # → samples/synth/
python3 evaluation/run_eval.py evaluation/samples/synth --json report.json
```

## Văn bản thật

1. Chép `.docx` vào `samples/real/` (thư mục `samples/` không được commit —
   văn bản thật có thể chứa thông tin nội bộ).
2. `python3 evaluation/draft_labels.py evaluation/samples/real` → mỗi file có
   `X.labels.json` dạng danh sách `{"id", "label", "text"}`.
3. Rà từng file: sửa `label` sai, sửa `document_type`, đặt `"draft": false`.
4. `python3 evaluation/run_eval.py evaluation/samples/real`.

## Kết quả trên bộ tổng hợp (60 văn bản, 1859 unit, 2026-10-05)

| | heuristic | Qwen3.6-35B-A3B (tắt thinking) |
|---|---|---|
| Unit cấu trúc đúng (ngoài nội dung) | 93.0% | 99.6% |
| Văn bản đúng hoàn toàn | 56.7% | 95.0% |
| Loại văn bản đúng | 100% | 100% |
| Kết quả chấm trùng chấm theo đáp án | 96.1% | 99.8% |
| Thời gian | ~0 | ~80s / 60 văn bản (8 luồng) |

Heuristic hỏng chủ yếu ở khối ký không có dấu hiệu vị trí (căn giữa toàn
trang) và biên bản 2 người ký; LLM còn nhầm cơ quan chủ quản/ban hành ở header
tab 3 dòng. Bộ tổng hợp do chính bộ sinh tạo ra — con số trên văn bản thật sẽ
thấp hơn; dùng nó để phát hiện hồi quy, không để kết luận độ chính xác thật.
