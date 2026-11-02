# screenai-ocr — Chrome Screen AI OCR qua OpenAI API (thử nghiệm)

Engine OCR "Screen AI" của Chrome (`libchromescreenai.so`, Linux x64) chạy CPU, đặt sau `/v1/chat/completions` với cùng hợp đồng như SenOCR-Vi (`vllm-ocr`). Dùng để gắn nhãn dữ liệu tinh chỉnh SenOCR (`scripts/screenai_ocr`), hoặc làm endpoint OCR thứ hai.

> ⚠️ `libchromescreenai.so` là phần mềm đóng của Google, chỉ được phân phối để Chrome dùng. Image **không chứa** thư viện này: lần khởi động đầu, `entrypoint.sh` tải nó từ máy chủ cập nhật của Google vào volume `screenai_component` (kiểm sha256). Không đẩy volume đó lên registry hay đóng vào image phân phối. Cần ý kiến pháp lý trước khi dùng cho mục đích thương mại.

Phần lõi chuyển từ [ayismas/chrome-ocr](https://github.com/ayismas/chrome-ocr) (MIT, DLL Windows) sang bản Linux. Image tự dựng stub `threadlogger::EnableThreadedLogging`, vì Chrome thường tự cung cấp hàm này.

## Service

Compose: service `screenai-ocr`, container `WeKnora-screenai-ocr`, profile `screenai` (đã có trong `COMPOSE_PROFILES`).

```bash
docker compose -p spec-airag-refactor --profile screenai up -d --build --no-deps screenai-ocr
curl -s 127.0.0.1:8004/health      # {"status":"ok","screenai_version":"153.3",...}
```

| Biến (.env) | Mặc định | |
|---|---|---|
| `SCREENAI_PORT` | 8004 | cổng host, gắn vào `INTERNAL_BIND_ADDR` (127.0.0.1) |
| `SCREENAI_WORKERS` | 2 | số tiến trình; mỗi tiến trình ~300 MB và xử lý tuần tự từng ảnh |
| `SCREENAI_CPUS` / `SCREENAI_MEM_LIMIT` | 4 / 3g | |
| `SCREENAI_API_KEY` | trống | đặt thì bắt buộc `Authorization: Bearer …` |
| `SCREENAI_MODEL_ID` | screenai-ocr | id trả ở `/v1/models` |

Trong mạng `WeKnora-network`, gọi `http://screenai-ocr:8004/v1` với model `screenai-ocr`. Đo ngày 2026-10-19: 2 worker xử lý 8 trang A4 song song trong 2,4 s (~0,3 s/trang), RAM ~600 MB.

Một số chỗ dùng được:
- docreader: `DOCREADER_VN_OCR_URL=http://screenai-ocr:8004/v1`, `DOCREADER_VN_OCR_MODEL=screenai-ocr`.
- Endpoint của Tổ chức (`paddleocr_vl_endpoint`, đi qua app Go): cần thêm `screenai-ocr` vào `SSRF_WHITELIST` trước.

Cập nhật component: xoá volume `screenai_component` rồi khởi động lại; hoặc chạy `docker exec WeKnora-screenai-ocr python /app/fetch_component.py --no-stub --force` rồi restart.

## API

- `GET /health`, `GET /v1/models`.
- `POST /v1/chat/completions`:
  - Ảnh gửi dưới dạng `image_url` là `data:` base64 (URL http không được nhận, tối đa 20 MB); phần text như `"OCR:"`.
  - Câu trả lời nằm ở `choices[0].message.content`.
  - Không hỗ trợ `stream`.
  - `usage` chỉ là ước lượng; `temperature`, `max_tokens`, `repetition_penalty` bị bỏ qua.
- Hai trường thêm:
  - `screenai_format`: `senocr` (mặc định) hoặc `lines`.
  - `screenai_return_lines: true`: trả thêm `screenai.lines` (bbox, độ tin cậy dòng và từng từ).
- Định dạng `senocr` bắt chước SenOCR-Vi:
  - mỗi đoạn văn một dòng;
  - tiêu đề căn giữa và quốc hiệu giữ dòng riêng;
  - mỗi hàng bảng một dòng;
  - không dùng Markdown;
  - xoá ký tự `|` đọc nhầm từ đường kẻ bảng;
  - đổi chữ Kirin trông giống Latin về chữ Latin.

## Chạy ngoài Docker

```bash
pip install fastapi uvicorn pillow numpy requests
python fetch_component.py            # tải vào ./component/ và dựng stub (cần g++)
uvicorn server:app --host 127.0.0.1 --port 8004 --workers 2
```
