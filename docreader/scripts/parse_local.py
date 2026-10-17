"""Script gỡ lỗi phân tích cục bộ: gọi thẳng Parser để phân tích tệp trên máy, không qua dịch vụ gRPC.

Cách dùng (chạy tại thư mục gốc kho mã WeKnora):
    PYTHONPATH=. docreader/.venv/bin/python docreader/scripts/parse_local.py <đường dẫn tệp> [--engine markitdown] [--out out.md]

Ví dụ:
    PYTHONPATH=. docreader/.venv/bin/python docreader/scripts/parse_local.py docreader/testdata/test.md
    PYTHONPATH=. docreader/.venv/bin/python docreader/scripts/parse_local.py ~/Desktop/demo.pdf --out /tmp/demo.md
"""

import argparse
import logging
import os
import sys
import time

from docreader.parser import Parser


def main() -> int:
    parser = argparse.ArgumentParser(description="Phân tích một tệp cục bộ và xuất markdown")
    parser.add_argument("path", help="đường dẫn tệp cần phân tích")
    parser.add_argument(
        "--engine",
        default="",
        help="tên engine phân tích (builtin / markitdown); bỏ trống để dùng engine mặc định",
    )
    parser.add_argument(
        "--type",
        default="",
        help="loại tệp (pdf/docx/md…); bỏ trống để suy ra từ phần mở rộng",
    )
    parser.add_argument(
        "--out",
        default="",
        help="ghi toàn bộ markdown vào tệp này và xuất ảnh vào thư mục images/ cùng cấp",
    )
    parser.add_argument(
        "--scanned",
        action="store_true",
        help="bỏ qua trích chữ bằng markitdown, render từng trang PDF thành ảnh (dùng cho bản scan, tránh pdfminer treo)",
    )
    parser.add_argument(
        "--log-level",
        default="INFO",
        help="mức log (DEBUG/INFO/WARNING/ERROR)",
    )
    args = parser.parse_args()

    logging.basicConfig(
        level=getattr(logging, args.log_level.upper(), logging.INFO),
        format="%(asctime)s %(levelname)s %(name)s: %(message)s",
        stream=sys.stderr,
    )

    if not os.path.isfile(args.path):
        print(f"Không tìm thấy tệp: {args.path}", file=sys.stderr)
        return 1

    file_name = os.path.basename(args.path)
    file_type = args.type or os.path.splitext(file_name)[1].lstrip(".")
    with open(args.path, "rb") as f:
        content = f.read()

    started = time.monotonic()
    if args.scanned:
        from docreader.parser.pdf_parser import PDFScannedParser

        doc = PDFScannedParser(file_name=file_name, file_type=file_type).parse_into_text(
            content
        )
    else:
        doc = Parser().parse_file(
            file_name=file_name,
            file_type=file_type,
            content=content,
            parser_engine=args.engine or None,
        )
    elapsed = time.monotonic() - started

    print("=" * 60, file=sys.stderr)
    print(f"file       : {file_name}", file=sys.stderr)
    print(f"type       : {file_type}", file=sys.stderr)
    print(f"engine     : {args.engine or 'builtin'}", file=sys.stderr)
    print(f"scanned    : {args.scanned}", file=sys.stderr)
    print(f"content_len: {len(doc.content)}", file=sys.stderr)
    print(f"images     : {len(doc.images)}", file=sys.stderr)
    print(f"metadata   : {doc.metadata}", file=sys.stderr)
    print(f"elapsed    : {elapsed:.2f}s", file=sys.stderr)
    print("=" * 60, file=sys.stderr)

    if args.out:
        import base64

        out_dir = os.path.dirname(os.path.abspath(args.out))
        os.makedirs(out_dir, exist_ok=True)
        with open(args.out, "w", encoding="utf-8") as f:
            f.write(doc.content)
        if doc.images:
            img_root = os.path.join(out_dir, "images")
            os.makedirs(img_root, exist_ok=True)
            for ref_path, b64data in doc.images.items():
                try:
                    raw = base64.b64decode(b64data)
                except Exception:
                    raw = b64data if isinstance(b64data, bytes) else b64data.encode()
                dest = os.path.join(out_dir, ref_path)
                os.makedirs(os.path.dirname(dest), exist_ok=True)
                with open(dest, "wb") as imgf:
                    imgf.write(raw)
        print(f"Đã ghi: {args.out} (ảnh xuất ra {out_dir}/images/)", file=sys.stderr)
    else:
        print(doc.content)

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
