# Gắn nhãn dữ liệu tinh chỉnh SenOCR-Vi bằng Screen AI (thử nghiệm)

Nhãn lấy từ service `screenai-ocr` (Chrome Screen AI qua OpenAI API, xem `docker/screenai-ocr/README.md`; mặc định `http://127.0.0.1:8004/v1`, đổi bằng `--server`). Thư mục này chỉ chứa công cụ tạo dữ liệu.

> ⚠️ Nhãn sinh từ phần mềm đóng của Google: cần ý kiến pháp lý trước khi dùng chúng cho mô hình thương mại.

```bash
pip install -r requirements.txt
```

## Tạo dữ liệu

```bash
python build_corpus_counts.py /home/orca/data/vbpl/html --out data/vbpl_counts.pkl.gz   # ~15 s
# scan thật: nhãn Screen AI, SenOCR làm "người đọc thứ hai", tần suất VBPL phân xử
python make_dataset.py --senocr-url http://127.0.0.1:8001/v1 --counts data/vbpl_counts.pkl.gz \
    --out data/scan1 /home/orca/data/vbpl/original_pdf/*.pdf
# PDF có lớp chữ: lớp chữ là đáp án, ảnh được làm nhiễu như bản scan
python make_dataset.py --born-digital --degrade 2 --senocr-url http://127.0.0.1:8001/v1 \
    --out data/bd1 <pdf có lớp chữ>...
```

Ảnh được render giống hệt docreader (pypdfium2, 200 dpi, cạnh dài ≤ 2000 px, JPEG 85). `data/<run>/dataset.jsonl` có mỗi dòng một mẫu, theo định dạng messages (`<image>OCR:` → nhãn). `meta.status` cho biết độ tin cậy của nhãn:

| status | nghĩa |
|---|---|
| `exact` | PDF có lớp chữ: từ trong nhãn đã thay bằng từ lớp chữ, ≤ 2 % từ không khớp vị trí |
| `auto` | scan: không có từ nào SenOCR thấy mà Screen AI bỏ sót (`--max-missing`) |
| `review` | cần người xem (`meta.arbitration.missing`, `conflicts`) |

`meta.senocr` lưu câu trả lời hiện tại của SenOCR và `wer_vs_label`. Trang có WER cao chính là mẫu khó, nên ưu tiên đưa vào huấn luyện.

Cách phân xử (`arbitrate`) khi hai engine đọc một từ khác nhau:
- Chỉ đổi theo SenOCR khi **cả hai phía** ngữ cảnh, P(từ|từ trước) và P(từ sau|từ), đều nghiêng về SenOCR, tính theo tần suất cụm 2 âm tiết trong kho VBPL.
- Trường hợp ngược lại thì giữ nhãn. Bằng chứng mâu thuẫn thì ghi vào `conflicts` nhưng vẫn giữ nhãn.
- Từ đúng mà chỉ khác thứ tự đọc thì bỏ qua.
- Phần SenOCR lặp vô tận bị cắt trước khi so.

## Số đo (2026-10-19)

32 trang Luật 105/2025/QH15, render rồi làm nhiễu như bản scan; đáp án là lớp chữ (15 355 từ):

| | lỗi thay thế từ | WER |
|---|---|---|
| SenOCR-Vi | 1 237 | 27–32 % (một số trang lặp vô tận) |
| Screen AI, nhãn thô | 79 | 0,73 % |
| Screen AI + phân xử | 39 | 0,47 % (42/44 lần sửa đúng; ở các ca mâu thuẫn, nhãn đúng 106/115) |
| `--born-digital` | 0 | 0,21 % (chỉ còn chênh thứ tự đọc) |

Trang đạt `auto`: 26/32 trang nhiễu nhân tạo, 17/40 trang scan thật (vbpl, Công báo).

Giới hạn:
- Trên scan thật, Screen AI vẫn đọc sai dấu (trổng, quân lý). Phân xử sửa được phần lớn, nhưng nhãn `auto` không phải đáp án tuyệt đối.
- Ảnh làm nhiễu nhân tạo khác scan thật (không có dấu đỏ, chữ ký, giấy ố), nên nên trộn cả hai nguồn.
- Thứ tự đọc bảng phức tạp (ô gộp) vẫn có thể lệch.
