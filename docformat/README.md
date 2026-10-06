# docformat — kiểm tra thể thức văn bản (.docx)

> **Trong WeKnora, tính năng chạy bằng bản Go `internal/docformat`** (tool
> agent `check_document_format`, API `POST /api/v1/document-format/check`),
> dùng model chat của agent / model chat mặc định của workspace để gán thành
> phần. Thư mục này là **bản tham chiếu Python + bộ đo**: sửa luật/heuristic
> ở đây trước, kiểm bằng `evaluation/`, rồi port sang Go và xuất lại dữ liệu
> đối chiếu — `parity_test.go` bắt mọi lệch giữa hai bản:
>
> ```bash
> python3 evaluation/gen_corpus.py --out /tmp/synth --n 60
> python3 evaluation/export_parity.py ../internal/docformat/testdata/parity /tmp/synth
> cp docformat/rules_data/*.json ../internal/docformat/rules/
> go test ./internal/docformat/
> ```

Bóc tách văn bản hành chính `.docx` thành các thành phần (quốc hiệu, tiêu
ngữ, cơ quan chủ quản/ban hành, số ký hiệu, địa danh–ngày tháng, trích yếu,
nội dung, khối ký, nơi nhận, ...) cùng layout thật (font, cỡ chữ, đậm/nghiêng,
căn lề, thụt đầu dòng, khổ giấy, lề trang, vùng cột trái/phải) rồi chấm theo
bộ luật **Nghị định 30/2020/NĐ-CP, Phụ lục I** theo từng loại văn bản.

Core **stdlib thuần** (zipfile + ElementTree): không cần python-docx, chạy
được trong sandbox Python trần.

## Pipeline

```
.docx ──► layout.inspect_docx()   ──► segment.segment()      ──► rules.evaluate()
          word/document.xml          thành phần theo vị trí      JSON checks[]
          + styles.xml + theme       (zone trái/phải/full)
```

- `inspect_docx()` → `DocLayout`: mọi paragraph với effective font/size/
  bold/italic/alignment/indent/spacing (đã resolve style chain + docDefaults
  + theme fonts), `zone` (left/right/full/split cho layout 2 cột bằng bảng
  hoặc tab), sections (khổ giấy, lề).
- `segment()` (heuristic) hoặc `labeling.apply_labels()` (LLM) → components: `quoc_hieu`, `tieu_ngu`, `co_quan_chu_quan`,
  `co_quan_ban_hanh`, `so_ky_hieu`, `dia_danh_ngay_thang`, `do_mat`,
  `do_khan`, `trich_yeu`, `tham_quyen_ban_hanh`, `kinh_gui`, `can_cu`,
  `noi_dung`, `chuc_danh`,
  `nguoi_ky`, `signature`, `noi_nhan`, `phu_luc`.
- `evaluate()` → `checks[]` với `status` pass/fail/warn/skip, `expected`,
  `actual`, `evidence` (đoạn vi phạm). `severity`: error = vi phạm thể thức
  bắt buộc, warn = nên sửa.

## CLI

```bash
python -m docformat inspect file.docx           # bóc tách, JSON đầy đủ
python -m docformat inspect file.docx --runs    # kèm từng run
python -m docformat check file.docx             # báo cáo text
python -m docformat check file.docx --type quyet_dinh --format json
python -m docformat types                       # các loại văn bản có rule set
python -m docformat label-task file.docx        # đề bài gán nhãn cho LLM/agent
python -m docformat check file.docx --labels labels.json   # chấm theo nhãn
python -m docformat check file.docx --segmenter llm        # tự gọi LLM
```

## Gán thành phần bằng LLM

Cỡ chữ/font/căn lề được chấm theo thành phần, nên gán sai thành phần là chấm
sai. Heuristic vị trí (`segment.py`) hay vỡ với bố cục lạ, vì vậy có thể để
LLM gán nhãn; phần đo layout và chấm luật vẫn chạy bằng code.

- `labeling.py` dựng *units* (mỗi paragraph, hoặc mỗi nửa của dòng tách tab:
  `12L`/`12R`) kèm zone, căn lề, cỡ chữ, đậm/nghiêng, bảng và `hint` từ
  heuristic; đoạn giữa thân văn bản dài được lược (tự gán `noi_dung`).
- Trả lời của LLM: `{"document_type": "...", "labels": {"<id>": "<thành phần>"}}`.
  Unit thiếu nhãn hoặc nhãn không hợp lệ giữ nhãn heuristic; mọi chỗ LLM gán
  khác heuristic được liệt kê trong `segmentation.disagreements`.
- `segmenter`: `auto` (mặc định — dùng `labels` nếu có, rồi LLM nếu đã cấu
  hình, cuối cùng heuristic), `heuristic`, `llm`, `labels`. LLM lỗi thì rơi
  về heuristic, lý do ghi ở `segmentation.error`.

Ba cách có LLM:

1. **Qua WeKnora — khuyến nghị khi chạy cùng hệ thống.** docformat gọi
   `POST {WEKNORA_BASE_URL}/llm/chat`; WeKnora chạy **model chat mặc định của
   workspace** (đổi model mặc định trên giao diện là docformat dùng model mới,
   không phải sửa cấu hình), API key/endpoint của model không rời WeKnora,
   thinking tự tắt đúng kiểu từng provider:

   ```bash
   export WEKNORA_BASE_URL=http://<weknora>/api/v1
   export WEKNORA_API_KEY=<API key có quyền "chat">
   # export DOCFORMAT_LLM_MODEL_ID=<id>   # chỉ khi muốn ghim một model cụ thể
   ```

2. **Agent tự gán** (MCP/skill): đề bài gán nhãn đưa cho chính agent của
   WeKnora, agent trả nhãn qua `--labels` / tham số `labels` — dùng model của
   agent, không cần cấu hình gì.

3. **Endpoint OpenAI-compatible trực tiếp** (chạy docformat độc lập; nếu đặt
   thì được ưu tiên hơn WeKnora):

   ```bash
   export DOCFORMAT_LLM_BASE_URL=http://10.10.0.240:8000/v1
   export DOCFORMAT_LLM_MODEL=Qwen/Qwen3.6-35B-A3B-FP8
   export DOCFORMAT_LLM_EXTRA_BODY='{"chat_template_kwargs": {"enable_thinking": false}}'
   ```

   Với Qwen3 nên tắt thinking: trên bộ fixture, tắt thinking ~2.7s/văn bản,
   bật thinking ~30s/văn bản, độ chính xác như nhau.

`segmentation.model` / `segmentation.backend` trong báo cáo cho biết model nào
đã trả lời.

## MCP server

```bash
pip install -e '.[mcp]'
python -m docformat.mcp_server                      # stdio (Claude Desktop, ...)
MCP_SERVER_AUTH_TOKEN=secret python -m docformat.mcp_server \
    --transport http --host 0.0.0.0 --port 8090     # streamable HTTP, /mcp
```

Tools: `list_document_types`, `inspect_docx_layout`,
`get_component_labeling_task`, `check_document_format` (tham số `labels`,
`segmenter`). Luồng khuyến nghị cho agent: `get_component_labeling_task` →
agent gán nhãn → `check_document_format(labels=...)`.
Mỗi tool nhận **đúng một** nguồn file:

| Input          | Ý nghĩa                                                        |
|----------------|----------------------------------------------------------------|
| `file_path`    | file local; chặn trong `DOCFORMAT_ALLOWED_DIRS` (mặc định: cwd)|
| `file_url`     | http(s) URL (≤50MiB)                                           |
| `file_base64`  | nội dung file base64                                           |
| `knowledge_id` | tải file gốc qua `GET {WEKNORA_BASE_URL}/knowledge/{id}/download` với `X-API-Key: WEKNORA_API_KEY` |

### Đăng ký vào WeKnora

Chạy server ở transport `http`, rồi tạo **MCP Service** trong workspace
(transport `http-streamable`, URL `http://<host>:8090/mcp`, header
`Authorization: Bearer <token>`). Agent của workspace sẽ thấy ba tool trên
và có thể gọi `check_document_format(knowledge_id=...)` với tài liệu trong
knowledge base.

## Skill (luồng upload trong chat)

`skill/` chứa `doc-format-check` — tenant skill cài vào sandbox config; file
upload nằm tại `/workspace/input` nên agent chạy checker ngay trong sandbox,
không cần sửa code backend:

```bash
python3 scripts/build_skill_bundle.py   # → dist/doc-format-check.zip
```

Upload zip qua API/UI cài skill cho sandbox config; agent dùng
`shell_exec(skill_name="doc-format-check")` với lệnh trong `SKILL.md`.

## Bộ luật

`docformat/rules_data/*.json` — `_base.json` chứa quy tắc chung (A4, lề
20–25/30–35/15–20mm, Times New Roman, cỡ chữ, căn lề, thứ tự thành phần);
mỗi file loại văn bản ghi đè `required_components`/`optional_components` và
nối thêm `checks`. Thêm loại mới = thêm một JSON — không phải sửa code.

Loại hiện có: đủ **29 loại văn bản hành chính** của Điều 7 NĐ30 (ký hiệu theo
Phụ lục III) và `nghi_dinh`, `thong_tu`. Danh mục loại + ký hiệu nằm ở
`docformat/doctypes.py` — bản sao của `internal/vietnamese_legal/doctype.go`
phía Go, sửa thì sửa cả hai. Loại không có rule set riêng (`luat`, `bo_luat`,
`phap_lenh`, `hien_phap`, `thong_tu_lien_tich`) rơi về `base`.

| Loại | Ký hiệu | Loại | Ký hiệu | Loại | Ký hiệu |
|---|---|---|---|---|---|
| Nghị quyết | NQ | Đề án | ĐA | Bản ghi nhớ | GN |
| Quyết định | QĐ | Dự án | DA | Bản thỏa thuận | TTh |
| Chỉ thị | CT | Báo cáo | BC | Giấy ủy quyền | GUQ |
| Quy chế | QC | Biên bản | BB | Giấy mời | GM |
| Quy định | QyĐ | Tờ trình | TTr | Giấy giới thiệu | GGT |
| Thông cáo | TC | Hợp đồng | HĐ | Giấy nghỉ phép | NP |
| Thông báo | TB | Công văn | — | Phiếu gửi | PG |
| Hướng dẫn | HD | Công điện | CĐ | Phiếu chuyển | PC |
| Chương trình | CTr | Kế hoạch | KH | Phiếu báo | PB |
| Phương án | PA | | | Thư công | — |

## Giới hạn đã biết

- Chỉ `.docx`. `.doc`/PDF không mang layout OOXML.
- "Đúng thể thức" được chấm từ file, không phải bản in: con dấu, chữ ký
  số/tay, đánh số trang, khổ lẻ không kiểm chứng được từ XML.
- Heuristic phân vùng dựa trên bảng/tab; tài liệu dùng textbox lệch chuẩn
  hoặc khung mỹ thuật có thể cần rà soát tay.

## Test

```bash
python3 -m unittest discover -s tests
```
