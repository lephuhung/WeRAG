"""OpenAI-compatible OCR server over Chrome Screen AI — same contract as the
SenOCR-Vi vLLM endpoint docreader calls (POST /v1/chat/completions with an
image_url data URI + "OCR:" text, answer in choices[0].message.content).

Experimental: for building SenOCR fine-tuning labels (scripts/screenai_ocr).
Runs as the compose service ``screenai-ocr`` (profile "screenai"); locally:

    python fetch_component.py && uvicorn server:app --host 127.0.0.1 --port 8004 --workers 2

Extra request fields (ignored by other OpenAI servers):
    "screenai_format": "senocr" (default) | "lines"
    "screenai_return_lines": true   -> response gains "screenai": {"lines": [...]}
"""

from __future__ import annotations

import base64
import binascii
import io
import os
import time
import uuid

from fastapi import FastAPI, Header, HTTPException
from fastapi.responses import JSONResponse
from PIL import Image, UnidentifiedImageError

from screenai import ScreenAIEngine, to_lines_text, to_senocr_text

HERE = os.path.dirname(os.path.abspath(__file__))
COMPONENT_DIR = os.environ.get("SCREENAI_COMPONENT_DIR", os.path.join(HERE, "component"))
SO_PATH = os.environ.get("SCREENAI_SO", os.path.join(COMPONENT_DIR, "latest", "libchromescreenai.so"))
MODEL_ID = os.environ.get("SCREENAI_MODEL_ID", "screenai-ocr")
API_KEY = os.environ.get("SCREENAI_API_KEY", "")
MAX_IMAGE_BYTES = 20 * 1024 * 1024

app = FastAPI(title="screenai-ocr")
_started = int(time.time())


def _engine() -> ScreenAIEngine:
    return ScreenAIEngine.get(SO_PATH)


@app.on_event("startup")
def _warm() -> None:
    _engine()


def _error(status: int, message: str) -> JSONResponse:
    return JSONResponse(
        status_code=status,
        content={"error": {"message": message, "type": "invalid_request_error", "code": status}},
    )


def _check_auth(authorization: str | None) -> None:
    if API_KEY and authorization != f"Bearer {API_KEY}":
        raise HTTPException(status_code=401, detail="invalid api key")


@app.get("/health")
def health() -> dict:
    eng = _engine()
    return {"status": "ok", "screenai_version": eng.version, "max_dimension": eng.max_dimension}


@app.get("/v1/models")
def models(authorization: str | None = Header(default=None)) -> dict:
    _check_auth(authorization)
    return {
        "object": "list",
        "data": [{"id": MODEL_ID, "object": "model", "created": _started, "owned_by": "screenai"}],
    }


def _extract_image(messages: list) -> Image.Image:
    for msg in reversed(messages or []):
        content = msg.get("content")
        if not isinstance(content, list):
            continue
        for part in content:
            if part.get("type") != "image_url":
                continue
            url = (part.get("image_url") or {}).get("url", "")
            if not url.startswith("data:") or ";base64," not in url:
                raise ValueError("only base64 data: URIs are accepted for image_url")
            b64 = url.split(";base64,", 1)[1]
            if len(b64) * 3 // 4 > MAX_IMAGE_BYTES:
                raise ValueError("image too large")
            try:
                raw = base64.b64decode(b64, validate=False)
                img = Image.open(io.BytesIO(raw))
                img.load()
            except (binascii.Error, UnidentifiedImageError, OSError) as e:
                raise ValueError(f"cannot decode image: {e}") from e
            return img
    raise ValueError("no image_url part in messages")


@app.post("/v1/chat/completions")
def chat_completions(body: dict, authorization: str | None = Header(default=None)):
    _check_auth(authorization)
    if body.get("stream"):
        return _error(400, "stream is not supported")
    try:
        img = _extract_image(body.get("messages"))
    except ValueError as e:
        return _error(400, str(e))

    fmt = body.get("screenai_format", "senocr")
    if fmt not in ("senocr", "lines"):
        return _error(400, "screenai_format must be 'senocr' or 'lines'")

    t0 = time.time()
    lines = _engine().ocr(img)
    text = to_senocr_text(lines) if fmt == "senocr" else to_lines_text(lines)
    completion_tokens = max(1, len(text) // 4) if text else 0

    resp = {
        "id": f"chatcmpl-{uuid.uuid4().hex}",
        "object": "chat.completion",
        "created": int(time.time()),
        "model": body.get("model") or MODEL_ID,
        "choices": [
            {
                "index": 0,
                "message": {"role": "assistant", "content": text},
                "finish_reason": "stop",
            }
        ],
        # Rough figures: Screen AI has no tokens; kept so clients that read
        # usage (docreader's OCR billing) do not break.
        "usage": {
            "prompt_tokens": 0,
            "completion_tokens": completion_tokens,
            "total_tokens": completion_tokens,
        },
        "screenai": {"elapsed_ms": int((time.time() - t0) * 1000), "image_size": list(img.size)},
    }
    if body.get("screenai_return_lines"):
        resp["screenai"]["lines"] = [l.as_dict() for l in lines]
    return resp
