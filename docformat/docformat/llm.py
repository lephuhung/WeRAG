"""Minimal chat client (stdlib only) for component labeling.

Two backends, picked from the environment by :meth:`LLMConfig.from_env`:

1. **weknora** (preferred inside a WeKnora deployment) — the call goes through
   WeKnora's ``POST /llm/chat``, which runs the *workspace default chat
   model* (or ``DOCFORMAT_LLM_MODEL_ID``).  Switching the model in WeKnora
   switches docformat; credentials never leave WeKnora::

    WEKNORA_BASE_URL         e.g. http://weknora:8080/api/v1
    WEKNORA_API_KEY          API key with the "chat" capability
    DOCFORMAT_LLM_MODEL_ID   optional, a WeKnora chat model id (default:
                             the workspace default chat model)

2. **openai** — any OpenAI-compatible ``/chat/completions`` endpoint (vLLM,
   Ollama ``/v1``, LiteLLM, OpenAI...), for running docformat on its own.
   Used when DOCFORMAT_LLM_BASE_URL is set (it wins over WeKnora)::

    DOCFORMAT_LLM_BASE_URL   e.g. http://localhost:11434/v1
    DOCFORMAT_LLM_API_KEY    optional bearer token
    DOCFORMAT_LLM_MODEL      e.g. qwen2.5:14b-instruct
    DOCFORMAT_LLM_TIMEOUT    seconds (default 120)
    DOCFORMAT_LLM_EXTRA_BODY JSON merged into the request body, e.g. for
                             Qwen3 on vLLM (thinking is ~10x slower and not
                             needed for labeling):
                             '{"chat_template_kwargs": {"enable_thinking": false}}'
"""

from __future__ import annotations

import json
import os
import urllib.error
import urllib.request
from dataclasses import dataclass
from typing import Optional

from .labeling import parse_reply, task_messages


class LLMError(RuntimeError):
    transient = False


@dataclass
class LLMConfig:
    base_url: str
    model: str
    api_key: Optional[str] = None
    timeout: float = 120.0
    extra_body: Optional[dict] = None
    backend: str = "openai"  # openai | weknora

    @classmethod
    def from_env(cls) -> Optional["LLMConfig"]:
        base = os.environ.get("DOCFORMAT_LLM_BASE_URL", "").strip()
        model = os.environ.get("DOCFORMAT_LLM_MODEL", "").strip()
        timeout = float(os.environ.get("DOCFORMAT_LLM_TIMEOUT", "120"))
        if not base:
            wk_key = os.environ.get("WEKNORA_API_KEY", "").strip()
            if not wk_key:
                return None
            wk_base = os.environ.get("WEKNORA_BASE_URL",
                                     "http://localhost:8080/api/v1")
            return cls(base_url=wk_base.strip().rstrip("/"),
                       model=os.environ.get("DOCFORMAT_LLM_MODEL_ID",
                                            "").strip() or "default",
                       api_key=wk_key, timeout=timeout, backend="weknora")
        if not model:
            return None
        extra = os.environ.get("DOCFORMAT_LLM_EXTRA_BODY", "").strip()
        try:
            extra_body = json.loads(extra) if extra else None
        except json.JSONDecodeError as exc:
            raise LLMError("DOCFORMAT_LLM_EXTRA_BODY is not valid JSON: %s"
                           % exc) from exc
        return cls(base_url=base.rstrip("/"), model=model,
                   api_key=os.environ.get("DOCFORMAT_LLM_API_KEY") or None,
                   timeout=timeout, extra_body=extra_body)


def _chat(cfg: LLMConfig, messages: list, json_mode: bool) -> tuple:
    """One retry on timeouts / connection errors / 5xx — a busy vLLM queue
    occasionally drops or stalls a request."""
    try:
        return _chat_once(cfg, messages, json_mode)
    except LLMError as exc:
        if not getattr(exc, "transient", False):
            raise
        return _chat_once(cfg, messages, json_mode)


def _post(url: str, body: dict, headers: dict, timeout: float) -> dict:
    req = urllib.request.Request(
        url, data=json.dumps(body).encode("utf-8"),
        headers={"Content-Type": "application/json", **headers}, method="POST")
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return json.loads(resp.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode("utf-8", "replace")[:300]
        err = LLMError("LLM HTTP %s: %s" % (exc.code, detail))
        err.transient = exc.code >= 500
        raise err from exc
    except (urllib.error.URLError, TimeoutError, OSError) as exc:
        err = LLMError("LLM unreachable: %s" % exc)
        err.transient = True
        raise err from exc


def _chat_once(cfg: LLMConfig, messages: list, json_mode: bool) -> tuple:
    """(content, model name actually used)."""
    if cfg.backend == "weknora":
        body = {"messages": messages, "temperature": 0, "thinking": False,
                "json": json_mode, "max_tokens": 8192}
        if cfg.model and cfg.model != "default":
            body["model_id"] = cfg.model
        data = _post(cfg.base_url + "/llm/chat", body,
                     {"X-API-Key": cfg.api_key or ""}, cfg.timeout)
        try:
            d = data["data"]
            return d["content"], d.get("model_name") or d.get("model_id")
        except (KeyError, TypeError) as exc:
            raise LLMError("unexpected WeKnora response: %s"
                           % str(data)[:300]) from exc

    body = {"model": cfg.model, "messages": messages, "temperature": 0}
    if cfg.extra_body:
        body.update(cfg.extra_body)
    if json_mode:
        body["response_format"] = {"type": "json_object"}
    data = _post(cfg.base_url + "/chat/completions", body,
                 {"Authorization": "Bearer " + cfg.api_key}
                 if cfg.api_key else {}, cfg.timeout)
    try:
        return data["choices"][0]["message"]["content"], cfg.model
    except (KeyError, IndexError, TypeError) as exc:
        raise LLMError("unexpected LLM response: %s" % str(data)[:300]) from exc


def label_with_llm(task: dict, cfg: LLMConfig) -> dict:
    """Send the labeling task; one corrective retry on unparseable output.
    Some OpenAI-compatible servers reject ``response_format`` — retried
    without it.  The reply carries ``_model``: the model that answered (for
    the weknora backend, whatever the workspace default currently is)."""
    messages = task_messages(task)
    try:
        text, used = _chat(cfg, messages, json_mode=True)
    except LLMError as exc:
        if cfg.backend == "weknora" or (
                "response_format" not in str(exc) and "HTTP 400" not in str(exc)):
            raise
        text, used = _chat(cfg, messages, json_mode=False)
    try:
        reply = parse_reply(text)
    except ValueError:
        messages = messages + [
            {"role": "assistant", "content": text},
            {"role": "user", "content": "Trả lời lại CHỈ bằng một JSON object "
             "hợp lệ theo đúng định dạng đã yêu cầu."}]
        text, used = _chat(cfg, messages, json_mode=False)
        reply = parse_reply(text)
    reply["_model"] = used
    return reply
