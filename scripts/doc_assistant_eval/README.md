# Bộ test tuân thủ prompt & công cụ của trợ lý soạn thảo văn bản

Kiểm tra end-to-end (qua HTTP, như web UI) xem agent `builtin-document-assistant`
có tuân thủ các quy tắc prompt và gọi đúng công cụ trong những pipeline một
người dùng thật đi qua: hỏi nội dung, tóm tắt, văn bản dài, chính tả, thể
thức NĐ30, bôi đen viết lại/gợi ý/giải thích, bôi đen tra cứu pháp lý, chống
trả lời theo trí nhớ, vẽ sơ đồ/hình minh họa, nhiều văn bản (@, đối chiếu,
nguồn chỉ đọc), gợi ý hoàn thiện, chèn đoạn, yêu cầu ngoài phạm vi, tìm web,
và nhóm `partial`: nhớ tên văn bản không đầy đủ ("Nghị định 53", "Luật bí mật
nhà nước 2025", sai năm) và từ viết tắt nhiều nghĩa → danh sách lựa chọn bấm
được (`legal_candidates`, `abbreviation_choice`), bước 2 mô phỏng bấm chọn;
mã đơn vị (PV01) giữ nguyên thành tên riêng; nhóm `template` (văn bản mẫu NĐ30
từ chat: "bỏ qua", "tạo lại", cỡ chữ/lề, "bỏ qua" ngay → văn bản trắng) và
nhóm `handoff` (đang ở agent khác, thảo luận nội dung rồi đòi file Word →
thẻ chuyển sang Soạn thảo văn bản; bấm → văn bản .docx mang nội dung đã chốt).

```
scripts/doc_assistant_eval/
├── fixtures/build_fixtures.py   # sinh 5 file .docx có lỗi gieo sẵn + manifest.json
├── werag_client.py              # client API: session, upload, mở tab, SSE agent-chat
├── checks.py                    # bộ assertion (tool gọi, ops, thẻ, nội dung trả lời…)
├── scenarios.py                 # 56 kịch bản, nhóm theo pipeline
├── compare.py                   # lưu baseline / so sánh một lần chạy với baseline
├── baselines/                   # kết quả đã lưu (commit) để đối chiếu: <ngày>.json
├── build_template_docx.mjs      # dựng .docx bằng bộ dựng của frontend-next (check _saved_as_docx)
├── judge.py                     # giám khảo LLM (tùy chọn, OpenAI-compatible)
├── run_eval.py                  # runner → results/<ts>/report.md + results.json
└── mint_token.py                # cấp token cho tài khoản test (JWT_SECRET + auth_tokens)
```

## Chạy

```bash
cd scripts/doc_assistant_eval
python3 -m venv .venv && .venv/bin/pip install python-docx requests
.venv/bin/python fixtures/build_fixtures.py            # → fixtures/out/*.docx

# token: đăng nhập (POST /auth/login) hoặc cấp trực tiếp cho tài khoản test
JWT_SECRET=$(sudo -n docker exec WeKnora-app-dev sh -c 'echo $JWT_SECRET') \
  python3 mint_token.py --email claude_admin@test.local --tenant 10003 > token.txt

python3 run_eval.py --token-file token.txt --tenant 10003            # tất cả
python3 run_eval.py --token-file token.txt --tenant 10003 --groups spelling,rewrite
python3 run_eval.py --token-file token.txt --tenant 10003 --only format_check_and_fix --keep-sessions
python3 run_eval.py --list

# giám khảo LLM cho các tiêu chí mềm (tóm tắt trung thực, không bịa):
JUDGE_BASE_URL=http://10.10.0.240:8000/v1 JUDGE_MODEL=<model> python3 run_eval.py ...
python3 mint_token.py --revoke "$(cat token.txt)"                      # thu hồi khi xong
```

Mỗi kịch bản tạo session riêng, mở tài liệu (`target` = tab soạn thảo,
`source` = tải lên ở chat), gửi các lượt, chấm bằng check tất định rồi xóa
session (trừ `--keep-sessions`). `--parallel N` chạy N kịch bản song song.

Mặc định tab mở với `editor_kind=word_addin` (không cần ONLYOFFICE kết
nối; các `document_ops` trả về là thứ trình soạn thảo sẽ áp dụng). Dùng
`--editor onlyoffice` khi muốn đi qua đường ONLYOFFICE (cần documentserver
và việc snapshot/forcesave có thể chờ 20 s mỗi lượt).

## Điều kiện môi trường

- Tài khoản test là thành viên thường của tenant 10003: khi `usage.enforce`
  bật, tenant phải có gói sử dụng còn hạn (nếu không mọi lượt trả 429
  `no_subscription`). Mỗi lần chạy đủ bộ tốn ~45 lượt hỏi của gói.
- Kịch bản `abbr_choice` cần từ điển viết tắt có 2 nghĩa active cho `ATTT`
  (dev: "An toàn thông tin" và "An toàn thực phẩm").
- Backend dev (air) restart mỗi khi có file Go được lưu; runner tự chạy lại
  kịch bản bị đứt (`--retries`), nhưng nên tránh lưu Go trong lúc chạy.

## Đối chiếu với lần trước

`results/` không được commit. Sau một lần chạy đầy đủ, lưu bản tóm tắt
(trạng thái từng kịch bản/check, không có câu trả lời) vào `baselines/` và
commit cùng `FINDINGS-<ngày>.md`; lần sau so sánh:

```bash
python3 compare.py diff results/<thư mục>/<ts>/results.json baselines/2026-10-18.json   # exit 1 khi có kịch bản tệ đi
python3 compare.py save results/<thư mục>/<ts>/results.json --name <ngày> --note "editor, judge, …"
```

Cấu hình chuẩn của các baseline: `--editor onlyoffice`, giám khảo
`JUDGE_MODEL=Qwen/Qwen3.6-35B-A3B-FP8`, tenant 10003. Giám khảo không biết văn
bản mới hơn dữ liệu huấn luyện của nó (vd. 333/2026/NĐ-CP trong catalogue):
FAIL của giám khảo ở nhóm `partial` cần đọc lại trước khi coi là lỗi.

`abbr_proper_name` thêm một mã ngẫu nhiên PV60–PV99 làm tên riêng (active) vào
từ điển dùng chung; xóa sau khi chạy:

```sql
DELETE FROM abbreviations WHERE is_proper_name AND short_form ~ '^PV[6-9][0-9]$'
  AND description LIKE 'Tên riêng — người dùng chọn giữ nguyên%';
```

## Trạng thái

- **PASS / FAIL**: quy tắc được mã hóa trong code hoặc prompt (công cụ bị
  ép gọi, thẻ đề xuất, không sửa khi chưa đồng ý, thông điệp "không tìm thấy
  căn cứ", không mời bấm nút không tồn tại…).
- **WARN**: kỳ vọng mềm (chất lượng mô hình, hạ tầng ngoài: Qwen-Image, web).
- **SKIP**: giám khảo LLM chưa bật.

## Thêm kịch bản

Thêm một `Scenario` vào `scenarios.py`: danh sách tài liệu `(role, file)`,
các `Step(query, checks, selection=…, mentions=…, before=…, judge=…)`.
Helper: `sel(doc, text_fn, occurrence)` tạo `document_selection` (bôi đen),
`men(doc, …)` tạo `mentioned_items` (@), `wait_format_check(doc)` chờ kiểm
tra thể thức nền. Oracle cho chuỗi kết quả tool nằm trong
`internal/agent/tools/*.go` (xem docstring trong `checks.py`).
