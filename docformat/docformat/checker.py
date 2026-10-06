"""Orchestration: docx bytes -> layout -> segments -> rule evaluation."""

from __future__ import annotations

from typing import Optional

from . import labeling
from .layout import DocLayout, inspect_docx
from .rules import available_types, evaluate, load_rule_set
from .segment import segment

SEGMENTERS = ("auto", "heuristic", "llm", "labels")


def _segment(layout: DocLayout, segmenter: str = "auto",
             labels: Optional[dict] = None, llm_config=None) -> tuple:
    """Pick the component labeler.

    - ``labels``: apply a labeling reply produced elsewhere (e.g. by the
      calling agent from :func:`labeling_task_for`).
    - ``llm``: call the configured OpenAI-compatible model.
    - ``heuristic``: positional rules only.
    - ``auto``: labels if given, else LLM when configured, else heuristic.

    LLM failures fall back to the heuristic; the reason is reported in
    ``segmentation.error`` instead of failing the whole check."""
    heur = segment(layout)
    mode = (segmenter or "auto").strip().lower()
    if mode not in SEGMENTERS:
        raise ValueError("segmenter must be one of %s" % ", ".join(SEGMENTERS))
    if labels is not None and mode in ("auto", "labels"):
        task, units, elided = labeling.prepare(layout, heur)
        seg, diag = labeling.apply_labels(layout, labels, heur, units, elided)
        return seg, {"method": "labels", **diag}
    if mode == "labels":
        raise ValueError("segmenter='labels' requires labels")
    if mode == "heuristic":
        return heur, {"method": "heuristic"}

    from .llm import LLMConfig, LLMError, label_with_llm
    try:
        cfg = llm_config or LLMConfig.from_env()
    except LLMError as exc:
        return heur, {"method": "heuristic", "error": "LLM: %s" % exc}
    if cfg is None:
        info = {"method": "heuristic"}
        if mode == "llm":
            info["error"] = ("LLM chưa cấu hình (WEKNORA_API_KEY, hoặc "
                             "DOCFORMAT_LLM_BASE_URL + DOCFORMAT_LLM_MODEL) "
                             "— dùng heuristic")
        return heur, info
    task, units, elided = labeling.prepare(layout, heur)
    try:
        reply = label_with_llm(task, cfg)
        seg, diag = labeling.apply_labels(layout, reply, heur, units, elided)
    except (LLMError, ValueError) as exc:
        return heur, {"method": "heuristic", "error": "LLM: %s" % exc}
    return seg, {"method": "llm", "model": reply.get("_model") or cfg.model,
                 "backend": cfg.backend,
                 "notes": reply.get("notes"), **diag}


def labeling_task_for(content: bytes) -> dict:
    """The labeling task for a document — hand it to any LLM/agent, then pass
    its JSON reply back as ``labels`` to :func:`check_document`."""
    layout = inspect_docx(content)
    if layout.errors and not layout.paragraphs:
        return {"ok": False, "error": "; ".join(layout.errors)}
    task, _, _ = labeling.prepare(layout, segment(layout))
    return {"ok": True, **task}


def inspect_document(content: bytes, *, include_runs: bool = False,
                     segmenter: str = "heuristic",
                     labels: Optional[dict] = None, llm_config=None) -> dict:
    """Return layout + component segmentation without rule evaluation."""
    layout = inspect_docx(content)
    seg, seg_info = _segment(layout, segmenter, labels, llm_config)
    paras = []
    for p in layout.paragraphs:
        d = {
            "index": p.index,
            "text": p.text,
            "zone": p.zone,
            "source": p.source,
            "alignment": p.alignment,
            "font_name": p.font_name,
            "size_pt": p.size_pt,
            "bold": p.bold,
            "italic": p.italic,
            "underline": p.underline,
            "text_is_upper": p.text_is_upper,
            "indent_left_mm": p.indent_left_mm,
            "indent_first_line_mm": p.indent_first_line_mm,
            "space_before_pt": p.space_before_pt,
            "space_after_pt": p.space_after_pt,
            "line_spacing_pt": p.line_spacing_pt,
            "line_spacing_multiple": p.line_spacing_multiple,
            "style_name": p.style_name,
            "in_table": p.in_table,
            "table_index": p.table_index,
            "row": p.row,
            "col": p.col,
            "section": p.section,
        }
        if p.zone == "split":
            d["left_text"] = p.left_text
            d["right_text"] = p.right_text
        if include_runs:
            d["runs"] = p.runs
        label = seg.para_labels.get(p.index)
        if label:
            d["component"] = label
        paras.append(d)
    return {
        "sections": [
            {k: v for k, v in vars(s).items() if v is not None}
            for s in layout.sections
        ],
        "n_tables": layout.n_tables,
        "has_headers": layout.has_headers,
        "has_footers": layout.has_footers,
        "errors": layout.errors,
        "detected_type": seg.detected_type,
        "segmentation": seg_info,
        "components": seg.to_dict()["components"],
        "paragraphs": paras,
    }


def check_document(content: bytes, document_type: str = "auto",
                   source_name: str = "", *, segmenter: str = "auto",
                   labels: Optional[dict] = None, llm_config=None) -> dict:
    """Full check: extract layout, label components, evaluate the rule set
    for ``document_type`` ("auto" uses the detected type).  See
    :func:`_segment` for ``segmenter`` / ``labels``.
    """
    layout = inspect_docx(content)
    if layout.errors and not layout.paragraphs:
        return {
            "source": source_name,
            "ok": False,
            "error": "; ".join(layout.errors),
        }
    try:
        seg, seg_info = _segment(layout, segmenter, labels, llm_config)
    except ValueError as exc:
        return {"source": source_name, "ok": False, "error": str(exc)}

    requested = (document_type or "auto").strip().lower()
    used = seg.detected_type if requested in ("auto", "", "detect") else requested
    known = set(available_types()) | {"base"}
    used_rules = used if used in known else "base"
    rule_set = load_rule_set(used_rules)

    checks = evaluate(layout, seg, rule_set)
    n_fail = sum(1 for c in checks if c["status"] == "fail")
    n_warn = sum(1 for c in checks if c["status"] == "warn")
    n_pass = sum(1 for c in checks if c["status"] == "pass")
    n_skip = sum(1 for c in checks if c["status"] == "skip")

    return {
        "source": source_name,
        "ok": True,
        "document_type": {
            "requested": requested,
            "detected": seg.detected_type,
            "used": used,
            "rule_set": rule_set.get("label", used_rules),
        },
        "segmentation": seg_info,
        "summary": {
            "pass": n_pass,
            "fail": n_fail,
            "warn": n_warn,
            "skip": n_skip,
        },
        "sections": [
            {k: v for k, v in vars(s).items() if v is not None}
            for s in layout.sections
        ],
        "components": seg.to_dict()["components"],
        "checks": checks,
    }
