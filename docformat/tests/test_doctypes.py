import os
import re
import sys
import unittest
from pathlib import Path

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from docformat import doctypes  # noqa: E402
from docformat.labeling import DOC_TYPES
from docformat.rules import available_types, load_rule_set
from docformat.segment import _type_from_symbol, detect_type

GO_REGISTRY = Path(__file__).resolve().parents[2] / "internal/vietnamese_legal/doctype.go"


class DocTypesTest(unittest.TestCase):
    def test_29_nd30_types_each_with_a_rule_set(self):
        self.assertEqual(len(doctypes.ND30_TYPES), 29)
        rule_sets = set(available_types())
        for d in doctypes.ND30_TYPES:
            self.assertIn(d.slug, rule_sets)
            load_rule_set(d.slug)
        self.assertEqual(DOC_TYPES[:-1], list(doctypes.SLUGS))

    def test_heading_detects_every_type(self):
        for d in doctypes.DOC_TYPES:
            got = detect_type(d.name.upper() + "\nVề việc triển khai nhiệm vụ", "")
            self.assertEqual(got, d.slug, d.name)

    def test_symbols(self):
        cases = {
            "Số: 45/KH-UBND": "ke_hoach", "Số: 3/HĐ-UBND": "hop_dong",
            "Số: 3/HD-SYT": "huong_dan", "Số: 9/ĐA-UBND": "de_an",
            "Số: 9/DA-BQL": "du_an", "Số: 1/QyĐ-UBND": "quy_dinh",
            "Số: 8/TTr-UBND": "to_trinh", "Số: 2/TTh-UBND": "ban_thoa_thuan",
            "Số: 12/QD-UBND": "quyet_dinh",  # Đ lost to the font
            "Số: 53/2022/NĐ-CP": "nghi_dinh", "Số: 7/CĐ-TTg": "cong_dien",
            "Số: 1234/UBND-VP": "cong_van",
            "Số: 24/2018/QH14": None, "Số: 05/VBHN-BCT": None,
        }
        for so, want in cases.items():
            self.assertEqual(_type_from_symbol(so), want, so)

    def test_mirrors_go_registry(self):
        if not GO_REGISTRY.exists():
            self.skipTest("Go source not available")
        go = re.findall(r'\{"(\w+)", "([^"]+)", DocTypeGroup(\w+), (?:nil|\[\]string\{([^}]*)\})',
                        GO_REGISTRY.read_text(encoding="utf-8"))
        go_types = [(slug, name, {"HanhChinh": "hanh_chinh", "QPPL": "qppl"}[grp],
                     tuple(s.strip().strip('"') for s in syms.split(",") if s.strip()))
                    for slug, name, grp, syms in go]
        py_types = [(d.slug, d.name, d.group, d.symbols) for d in doctypes.DOC_TYPES]
        self.assertEqual(py_types, go_types)


if __name__ == "__main__":
    unittest.main()
