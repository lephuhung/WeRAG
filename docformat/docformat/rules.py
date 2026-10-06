"""Rule engine for "thể thức văn bản" checks.

Rule sets are JSON files under ``rules_data/``: a shared ``_base.json``
(Nghị định 30/2020/NĐ-CP, Phụ lục I — khổ giấy, lề trang, font, cỡ chữ, căn
lề chung) plus one file per document type that inherits/overrides it.

Check kinds
-----------
- ``present`` / ``absent``: the component must (not) be found.
- ``prop``: evaluate a paragraph property over the component's paragraphs.
  ``mode`` = ``all`` (every paragraph satisfies), ``majority`` (>=70% of
  paragraphs satisfy — for body text where headings legitimately differ) or
  ``any``.
- ``page_setup``: evaluate a Section property (page size, margins).
- ``order``: components must appear in the rule-set ``order`` list.

Operators: ``eq``, ``ne``, ``in``, ``range`` ([min,max], inclusive),
``regex``, ``ge``, ``le``.
"""

from __future__ import annotations

import json
import re
from importlib import resources
from typing import Optional

from .layout import DocLayout, Para
from .segment import Segmented

_RULES_PACKAGE = "docformat.rules_data"


def _norm_font(name: Optional[str]) -> Optional[str]:
    if not name:
        return None
    return re.sub(r"\s+", " ", name).strip().lower()


def available_types() -> list:
    """Rule-set names shipped in rules_data (excluding the _base set)."""
    out = []
    try:
        for f in resources.files(_RULES_PACKAGE).iterdir():
            if f.name.endswith(".json") and not f.name.startswith("_"):
                out.append(f.name[:-5])
    except (FileNotFoundError, TypeError):
        pass
    return sorted(out)


def load_rule_set(doc_type: str) -> dict:
    """Load a rule set merged over _base.json."""
    base = _load_one("_base")
    if doc_type in ("base", "_base", "generic"):
        return base
    specific = _load_one(doc_type)
    merged = dict(base)
    merged.update({k: v for k, v in specific.items() if k != "checks"})
    # a type-specific check with the same id replaces the base one
    own = {c.get("id"): c for c in specific.get("checks", [])}
    merged["checks"] = [own.pop(c.get("id"), c) for c in base.get("checks", [])] \
        + [c for c in specific.get("checks", []) if c.get("id") in own]
    merged["type"] = specific.get("type", doc_type)
    merged["label"] = specific.get("label", doc_type)
    return merged


def _load_one(name: str) -> dict:
    try:
        with resources.files(_RULES_PACKAGE).joinpath(name + ".json").open(
                "r", encoding="utf-8") as fh:
            return json.load(fh)
    except (FileNotFoundError, json.JSONDecodeError) as exc:
        raise ValueError("unknown document type rule set %r (%s)" % (name, exc))


# ---------------------------------------------------------------------------
# Evaluation


def _prop_value(para: Para, prop: str):
    return getattr(para, prop, None)


def _compare(op: str, actual, expected) -> bool:
    if op == "eq":
        if isinstance(actual, str) and isinstance(expected, str):
            return _norm_font(actual) == _norm_font(expected)
        return actual == expected
    if op == "ne":
        return not _compare("eq", actual, expected)
    if op == "in":
        if isinstance(actual, str):
            return any(_norm_font(actual) == _norm_font(e) for e in expected)
        return actual in expected
    if op == "range":
        if actual is None:
            return False
        lo, hi = expected
        return lo <= actual <= hi
    if op == "ge":
        return actual is not None and actual >= expected
    if op == "le":
        return actual is not None and actual <= expected
    if op == "regex":
        return actual is not None and re.search(expected, str(actual)) is not None
    raise ValueError("unknown op %r" % op)


def _component_paras(layout: DocLayout, seg: Segmented, comp_key: str) -> list:
    out = []
    for idx in seg.comp(comp_key).get("paras", []):
        if 0 <= idx < len(layout.paragraphs):
            out.append(layout.paragraphs[idx])
    return out


def _eval_prop_check(layout: DocLayout, seg: Segmented, check: dict) -> dict:
    comp_key = check["component"]
    prop = check["prop"]
    op = check["op"]
    expected = check.get("value")
    mode = check.get("mode", "all")
    comp = seg.comp(comp_key)
    paras = _component_paras(layout, seg, comp_key)
    if not paras:
        return {"status": "skip", "actual": None,
                "note": "component %r not found" % comp_key, "evidence": []}

    # zone lives on the segmented element, not the paragraph (split paras
    # contribute to two zones at once).
    zones = comp.get("zones", [])

    def actual_of(i: int, p: Para):
        z = zones[i] if i < len(zones) else p.zone
        if prop == "zone":
            return z
        if prop == "text_upper":
            return p.text_is_upper
        if p.zone == "split" and z in ("left", "right"):
            side = p.left_props if z == "left" else p.right_props
            if prop in side:
                return side[prop]
            if prop == "text":
                return p.left_text if z == "left" else p.right_text
        return getattr(p, prop, None)

    def sat(i: int, p: Para) -> bool:
        actual = actual_of(i, p)
        if prop == "font_name":
            if actual is None:
                return False
            if op == "in":
                return any(_norm_font(actual) == _norm_font(e) for e in expected)
        return _compare(op, actual, expected)

    text_paras = [p for p in paras if p.text.strip()]
    flags = [(i, p, sat(i, p)) for i, p in enumerate(text_paras)]
    if not flags:
        return {"status": "skip", "actual": None,
                "note": "component %r has no text paragraphs" % comp_key,
                "evidence": []}
    n_ok = sum(1 for _, _, ok in flags if ok)
    if mode == "any":
        passed = n_ok >= 1
    elif mode == "majority":
        passed = n_ok / len(flags) >= 0.7
    else:
        passed = n_ok == len(flags)
    bad = [
        {
            "para": p.index,
            "text": p.text.strip()[:80],
            "actual": actual_of(i, p),
        }
        for i, p, ok in flags if not ok
    ][:5]
    return {
        "status": "pass" if passed else ("warn" if check.get("severity") == "warn" else "fail"),
        "actual": {"satisfied": n_ok, "of": len(flags)},
        "evidence": bad,
    }


def _eval_page_setup(layout: DocLayout, check: dict) -> dict:
    prop = check["prop"]
    op = check["op"]
    expected = check.get("value")
    secs = [s for s in layout.sections] or []
    if not secs:
        return {"status": "skip", "actual": None, "evidence": [],
                "note": "no section info"}
    actuals = [getattr(s, prop, None) for s in secs]
    ok = all(a is not None and _compare(op, a, expected) for a in actuals)
    return {
        "status": "pass" if ok else ("warn" if check.get("severity") == "warn" else "fail"),
        "actual": actuals,
        "evidence": [],
    }


def _eval_present(seg: Segmented, check: dict) -> dict:
    found = seg.comp(check["component"])["found"]
    want = check["op"] == "present"
    ok = found == want
    return {
        "status": "pass" if ok else ("warn" if check.get("severity") == "warn" else "fail"),
        "actual": "found" if found else "missing",
        "evidence": [],
    }


def _cross_column_pair(a: dict, b: dict, seg: Segmented) -> bool:
    """True when the two components sit in different zones of the SAME
    table — XML emits a table's left column before its right column, so
    element positions cannot order them."""
    za = set(a.get("zones") or ([a.get("zone")] if a.get("zone") else []))
    zb = set(b.get("zones") or ([b.get("zone")] if b.get("zone") else []))
    if za == zb:
        return False
    ta = {seg.para_table.get(i) for i in a.get("paras", [])} - {None}
    tb = {seg.para_table.get(i) for i in b.get("paras", [])} - {None}
    return bool(ta & tb)


def _eval_order(seg: Segmented, order: list) -> dict:
    """First-element positions of found components must follow `order`
    (equal positions are tolerated: a tab-split line hosts two columns;
    cross-column pairs inside one table are unordered)."""
    placed = []
    for name in order:
        comp = seg.comp(name)
        pos = comp.get("pos")
        if comp["found"] and pos is not None:
            placed.append((name, pos, comp))
    violations = []
    for i in range(1, len(placed)):
        prev_name, prev_pos, prev_comp = placed[i - 1]
        name, pos, comp = placed[i]
        if pos < prev_pos and not _cross_column_pair(comp, prev_comp, seg):
            violations.append({"component": name, "pos": pos,
                               "should_follow": prev_name,
                               "should_follow_pos": prev_pos})
    return {
        "status": "pass" if not violations else "fail",
        "actual": [(n, p) for n, p, _ in placed],
        "evidence": violations,
    }


def evaluate(layout: DocLayout, seg: Segmented, rule_set: dict) -> list:
    results = []
    for comp, sev in (
        [(c, "error") for c in rule_set.get("required_components", [])]
        + [(c, "warn") for c in rule_set.get("optional_components", [])]
    ):
        r = _eval_present(seg, {"component": comp, "op": "present",
                                "severity": sev})
        # a missing OPTIONAL component is not a finding — it only needs to
        # be right when present, which the prop checks cover separately
        if sev == "warn" and r["status"] == "warn":
            r = {"status": "skip", "actual": "missing",
                 "note": "thành phần không bắt buộc, không có trong văn bản",
                 "evidence": []}
        results.append({
            "id": "component.%s.present" % comp,
            "desc": ("Văn bản phải có thành phần %r" if sev == "error"
                     else "Văn bản có thành phần %r (không bắt buộc)") % comp,
            "severity": sev,
            "component": comp,
            **r,
        })
    for check in rule_set.get("checks", []):
        kind = check.get("kind", "prop")
        entry = {
            "id": check.get("id", "?"),
            "desc": check.get("desc", ""),
            "severity": check.get("severity", "error"),
            "component": check.get("component"),
        }
        try:
            if kind in ("present", "absent"):
                r = _eval_present(seg, check)
            elif kind == "page_setup":
                r = _eval_page_setup(layout, check)
            elif kind == "prop":
                r = _eval_prop_check(layout, seg, check)
            else:
                r = {"status": "skip", "note": "unknown check kind %r" % kind,
                     "actual": None, "evidence": []}
        except Exception as exc:  # a malformed rule must not kill the report
            r = {"status": "skip", "note": str(exc), "actual": None, "evidence": []}
        entry.update(r)
        results.append(entry)

    order = rule_set.get("order")
    if order:
        r = _eval_order(seg, order)
        results.append({
            "id": "component.order",
            "desc": "Thứ tự các thành phần của văn bản",
            "severity": "error",
            "component": None,
            **r,
        })
    return results
