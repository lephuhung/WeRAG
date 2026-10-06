"""docformat — kiểm tra thể thức văn bản hành chính Việt Nam (.docx).

Pipeline: inspect_docx (layout) -> segment / LLM labels (components) ->
evaluate (rules).
Pure stdlib: no third-party dependency required for inspect/segment/check.
"""

from .layout import inspect_docx, DocLayout
from .segment import segment, Segmented
from .rules import load_rule_set, available_types, evaluate
from .checker import check_document, inspect_document, labeling_task_for

__all__ = [
    "inspect_docx",
    "DocLayout",
    "segment",
    "Segmented",
    "load_rule_set",
    "available_types",
    "evaluate",
    "check_document",
    "inspect_document",
    "labeling_task_for",
]
