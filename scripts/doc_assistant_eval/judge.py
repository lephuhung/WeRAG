"""Optional LLM judge for soft criteria (summary faithfulness, honesty about
failures). Uses any OpenAI-compatible chat endpoint.

Environment: JUDGE_BASE_URL (e.g. http://10.10.0.240:8000/v1), JUDGE_MODEL,
JUDGE_API_KEY (optional). When JUDGE_BASE_URL is unset every judge check is
reported as SKIP.
"""
from __future__ import annotations

import json
import os
import re
from typing import Any, Dict, Optional

import requests

from checks import FAIL, PASS, SKIP, WARN, Result
from werag_client import Turn

TOOL_EVIDENCE_CHARS = 2500
TOOLS_BLOCK_CHARS = 20000
ANSWER_CHARS = 12000
ANSWER_CUT_MARKER = "[… câu trả lời còn tiếp, bị cắt cho giám khảo]"

SYSTEM = (
    "Bạn là giám khảo chấm câu trả lời của một trợ lý soạn thảo văn bản hành chính. "
    "Chỉ dựa vào tiêu chí được giao, bản ghi lượt hội thoại (câu hỏi, các công cụ đã gọi và kết quả, câu trả lời). "
    "Kết quả công cụ là trích đoạn thật (tối đa " + str(TOOL_EVIDENCE_CHARS) + " ký tự mỗi công cụ); các ghi chú "
    "'details omitted' / 'content omitted from history' chỉ là ghi chú lịch sử của hệ thống, KHÔNG phải bằng chứng "
    "thiếu căn cứ — trợ lý đã thấy toàn văn kết quả trong lượt. "
    "Danh sách đoạn (\"paragraphs\") trong BỐI CẢNH là toàn văn tài liệu, trừ khi nó kết thúc bằng ghi chú đã cắt bớt — "
    "khi đó phần sau không hiển thị cho giám khảo và chỉ coi là bịa khi mâu thuẫn với dữ kiện đã có; "
    "nếu không bị cắt, chỉ coi là bịa khi nội dung không có trong đó lẫn trong kết quả công cụ. "
    "Nếu câu trả lời kết thúc bằng '" + ANSWER_CUT_MARKER + "' thì đó là giám khảo cắt bớt cho gọn, không phải câu trả lời bị ngắt — đừng trừ điểm vì thế. "
    "Trả lời DUY NHẤT một JSON: {\"pass\": true|false, \"score\": 0-10, \"reason\": \"...\"} (reason tối đa 4 câu)."
)


_SEARCH_TOOLS = ("search_knowledge", "web_search")
_DOC_READ_TOOLS = ("search_document_section", "read_document", "find_in_documents", "read_document_outline")


def _knowledge_refs(turn: Turn) -> list:
    """Chunks the backend streamed as `references` events in this turn."""
    out = []
    for ev in turn.events:
        if (ev.get("response_type") or ev.get("type")) == "references":
            out.extend(ev.get("knowledge_references") or [])
    return out


def tool_evidence(t, turn: Turn) -> Any:
    """What the tool actually returned, not the one-line history summary.

    The `output` of the search/read tools is "…(details omitted from history)";
    the real evidence travels in the tool_result data (`results`, `documents[].hits`)
    or in the turn's `references` events.
    """
    d = t.data
    if t.name in _SEARCH_TOOLS:
        rows = d.get("results") or []
        if rows:
            return [{"title": r.get("knowledge_title") or r.get("title"), "chunk": r.get("chunk_index"),
                     "text": (r.get("content") or "")[:400]} for r in rows[:8]]
    if t.name in _DOC_READ_TOOLS:
        content = d.get("output") or ""
        if not content or "omitted from history" in content:
            parts = []
            for doc in d.get("documents") or []:
                for h in doc.get("hits") or []:
                    parts.append(f"{doc.get('handle') or doc.get('file_name', '')} [{h.get('index')}] {h.get('text', '')}")
            kid = d.get("knowledge_id")
            if kid:
                for r in _knowledge_refs(turn):
                    if r.get("knowledge_id") == kid:
                        parts.append(f"[chunk {r.get('chunk_index')}] {r.get('content') or ''}")
            if parts:
                content = "\n".join(parts)
            elif kid or d.get("knowledge_title"):
                sec = d.get("section")
                content = (f"Đã đọc {d.get('fetched_chunks', '?')}/{d.get('total_chunks', '?')} đoạn của "
                           f"«{d.get('knowledge_title', '')}»" + (f", mục {json.dumps(sec, ensure_ascii=False)}" if sec else "")
                           + " (nội dung đã đọc không có trong luồng sự kiện; trợ lý đã thấy toàn văn — "
                           "không coi là thiếu căn cứ).")
        return (content or t.output)[:TOOL_EVIDENCE_CHARS]
    if t.output:
        return t.output[:1200]
    return json.dumps({k: v for k, v in d.items() if k in ("proposal", "variants", "found", "count", "error")},
                      ensure_ascii=False)[:1200]


def parse_verdict(text: str) -> Optional[Dict[str, Any]]:
    """The judge's JSON; a reply cut by max_tokens mid-"reason" still yields pass/score."""
    m = re.search(r"\{.*\}", text, re.S)
    if m:
        try:
            return json.loads(m.group(0))
        except ValueError:
            pass
    p = re.search(r'"pass"\s*:\s*(true|false)', text)
    if not p:
        return None
    sc = re.search(r'"score"\s*:\s*(\d+)', text)
    rs = re.search(r'"reason"\s*:\s*"(.*)', text, re.S)
    return {"pass": p.group(1) == "true", "score": int(sc.group(1)) if sc else None,
            "reason": (rs.group(1) if rs else "") + " [lý do bị cắt]"}


class Judge:
    def __init__(self, base_url: Optional[str] = None, model: Optional[str] = None, api_key: Optional[str] = None,
                 timeout: float = 180):
        self.base_url = (base_url or os.environ.get("JUDGE_BASE_URL", "")).rstrip("/")
        self.model = model or os.environ.get("JUDGE_MODEL", "")
        self.api_key = api_key or os.environ.get("JUDGE_API_KEY", "")
        self.timeout = timeout

    @property
    def enabled(self) -> bool:
        return bool(self.base_url and self.model)

    def grade(self, criteria: str, turn: Turn, extra_context: str = "") -> Result:
        label = "LLM judge: " + criteria[:70]
        if not self.enabled:
            return Result(SKIP, label, "judge disabled (set JUDGE_BASE_URL and JUDGE_MODEL)")
        tools = []
        for t in turn.tool_calls:
            tools.append({"tool": t.name, "arguments": t.arguments, "success": t.success,
                          "result": tool_evidence(t, turn)})
        answer = turn.text
        if len(answer) > ANSWER_CHARS:
            answer = answer[:ANSWER_CHARS] + "\n" + ANSWER_CUT_MARKER
        user = (
            f"TIÊU CHÍ: {criteria}\n\n"
            + (f"BỐI CẢNH: {extra_context}\n\n" if extra_context else "")
            + f"CÂU HỎI: {turn.query}\n\n"
            + f"CÔNG CỤ ĐÃ GỌI: {json.dumps(tools, ensure_ascii=False)[:TOOLS_BLOCK_CHARS]}\n\n"
            + f"CÂU TRẢ LỜI: {answer}"
        )
        body: Dict[str, Any] = {
            "model": self.model,
            "messages": [{"role": "system", "content": SYSTEM}, {"role": "user", "content": user}],
            "temperature": 0,
            "max_tokens": 600,
            "chat_template_kwargs": {"enable_thinking": False},
        }
        headers = {"Content-Type": "application/json"}
        if self.api_key:
            headers["Authorization"] = f"Bearer {self.api_key}"
        try:
            r = requests.post(self.base_url + "/chat/completions", json=body, headers=headers, timeout=self.timeout)
            r.raise_for_status()
            text = r.json()["choices"][0]["message"]["content"]
        except Exception as e:  # noqa: BLE001
            return Result(WARN, label, f"judge call failed: {e}")
        verdict = parse_verdict(text)
        if verdict is None:
            return Result(WARN, label, f"unparseable judge reply: {text[:200]}")
        ok = bool(verdict.get("pass"))
        return Result(PASS if ok else FAIL, label, f"score={verdict.get('score')} — {verdict.get('reason', '')}")
