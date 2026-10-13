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

SYSTEM = (
    "Bạn là giám khảo chấm câu trả lời của một trợ lý soạn thảo văn bản hành chính. "
    "Chỉ dựa vào tiêu chí được giao, bản ghi lượt hội thoại (câu hỏi, các công cụ đã gọi và kết quả, câu trả lời). "
    "Trả lời DUY NHẤT một JSON: {\"pass\": true|false, \"score\": 0-10, \"reason\": \"...\"}."
)


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
                          "result": t.output[:1200] or json.dumps({k: v for k, v in t.data.items() if k in ("proposal", "variants", "found", "count", "error")}, ensure_ascii=False)[:1200]})
        user = (
            f"TIÊU CHÍ: {criteria}\n\n"
            + (f"BỐI CẢNH: {extra_context}\n\n" if extra_context else "")
            + f"CÂU HỎI: {turn.query}\n\n"
            + f"CÔNG CỤ ĐÃ GỌI: {json.dumps(tools, ensure_ascii=False)[:6000]}\n\n"
            + f"CÂU TRẢ LỜI: {turn.text[:6000]}"
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
        m = re.search(r"\{.*\}", text, re.S)
        if not m:
            return Result(WARN, label, f"unparseable judge reply: {text[:200]}")
        try:
            verdict = json.loads(m.group(0))
        except ValueError:
            return Result(WARN, label, f"bad JSON from judge: {text[:200]}")
        ok = bool(verdict.get("pass"))
        return Result(PASS if ok else FAIL, label, f"score={verdict.get('score')} — {verdict.get('reason', '')}")
