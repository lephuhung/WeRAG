#!/usr/bin/env python3
"""Select the vbpl.vn documents that go into the WeRAG legal catalogue.

Reads the parquet files of the Hugging Face dataset
th1nhng0/vietnamese-legal-documents (metadata.parquet, relationships.parquet)
and writes the JSONL pair consumed by cmd/legal-catalog-import:

  documents.jsonl  one vbpl metadata row per kept document
  relations.jsonl  the raw portal relations whose both ends were kept

Kept documents (decided 2026-10-09):
  * central-government issuers only — local scope or a UBND / HĐND / tỉnh /
    thành phố issuer is dropped;
  * the document types of WeRAG's NĐ30 registry
    (internal/vietnamese_legal/doctype.go): the 29 văn bản hành chính of
    Điều 7 NĐ30/2020 plus Hiến pháp, Bộ luật, Luật, Pháp lệnh, Nghị định,
    Thông tư, Thông tư liên tịch;
  * issued on or after --min-year (default 2010), for every type.

Usage (pandas + pyarrow needed, e.g. via uv):
  uv run --no-project --with pandas --with pyarrow python -I \\
      scripts/legal_catalog/prepare_vbpl.py <dataset-dir> <out-dir>
"""

import argparse
import json
import os
import re

import pandas as pd

ND30_TYPES = {
    "Nghị quyết", "Quyết định", "Chỉ thị", "Quy chế", "Quy định", "Thông cáo",
    "Thông báo", "Hướng dẫn", "Chương trình", "Kế hoạch", "Phương án", "Đề án",
    "Dự án", "Báo cáo", "Biên bản", "Tờ trình", "Hợp đồng", "Công văn",
    "Công điện", "Bản ghi nhớ", "Bản thỏa thuận", "Giấy ủy quyền", "Giấy mời",
    "Giấy giới thiệu", "Giấy nghỉ phép", "Phiếu gửi", "Phiếu chuyển", "Phiếu báo",
    "Thư công",
}
QPPL_TYPES = {
    "Hiến pháp", "Bộ luật", "Luật", "Pháp lệnh", "Nghị định", "Thông tư",
    "Thông tư liên tịch",
}
LOCAL_ISSUER = re.compile(
    r"UBND|HĐND|Ủy ban nhân dân|Uỷ ban nhân dân|Hội đồng nhân dân|tỉnh|Tỉnh|thành phố|Thành phố|TP\.")

DOC_FIELDS = [
    "id", "title", "so_ky_hieu", "loai_van_ban", "co_quan_ban_hanh", "nguoi_ky",
    "chuc_danh", "linh_vuc", "ngay_ban_hanh", "ngay_co_hieu_luc",
    "ngay_het_hieu_luc", "tinh_trang_hieu_luc",
]


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("dataset_dir", help="directory holding metadata.parquet and relationships.parquet")
    ap.add_argument("out_dir")
    ap.add_argument("--min-year", type=int, default=2010)
    args = ap.parse_args()

    m = pd.read_parquet(os.path.join(args.dataset_dir, "metadata.parquet"))
    issuer = m["co_quan_ban_hanh"].fillna("")
    central = (m["pham_vi"].fillna("") != "Địa phương") & ~issuer.str.contains(LOCAL_ISSUER)
    typed = m["loai_van_ban"].isin(ND30_TYPES | QPPL_TYPES)
    year = pd.to_datetime(m["ngay_ban_hanh"], format="%d/%m/%Y", errors="coerce").dt.year
    recent = year >= args.min_year
    kept = m[central & typed & recent]

    os.makedirs(args.out_dir, exist_ok=True)
    with open(os.path.join(args.out_dir, "documents.jsonl"), "w", encoding="utf-8") as f:
        for row in kept[DOC_FIELDS].fillna("").to_dict("records"):
            f.write(json.dumps(row, ensure_ascii=False) + "\n")

    ids = set(kept["id"])
    r = pd.read_parquet(os.path.join(args.dataset_dir, "relationships.parquet"))
    r = r[r["doc_id"].isin(ids) & r["other_doc_id"].isin(ids)].drop_duplicates()
    with open(os.path.join(args.out_dir, "relations.jsonl"), "w", encoding="utf-8") as f:
        for row in r[["doc_id", "other_doc_id", "relationship"]].to_dict("records"):
            f.write(json.dumps(row, ensure_ascii=False) + "\n")

    print(f"documents: {len(kept)} of {len(m)} (central {int(central.sum())}, "
          f"registry types {int(typed.sum())}, issued >= {args.min_year} {int(recent.sum())})")
    print(f"relations with both ends kept: {len(r)}")
    print(kept["loai_van_ban"].value_counts().to_string())


if __name__ == "__main__":
    main()
