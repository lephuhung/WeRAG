"""MCP server exposing the thể-thức checker.

Tools
-----
- ``list_document_types``      — rule sets available for --type.
- ``inspect_docx_layout``      — bóc tách văn bản: layout + components JSON.
- ``get_component_labeling_task`` — đề bài gán nhãn thành phần cho chính
  agent gọi tool (không cần cấu hình LLM riêng cho server).
- ``check_document_format``    — chấm thể thức theo NĐ30/2020/NĐ-CP; nhận
  ``labels`` (trả lời của agent cho đề bài trên) hoặc tự gọi LLM khi đã cấu
  hình DOCFORMAT_LLM_BASE_URL / DOCFORMAT_LLM_MODEL.

Inputs (exactly one per call): ``file_path`` (local, restricted to
``DOCFORMAT_ALLOWED_DIRS`` or cwd), ``file_url`` (http(s), size-capped),
``file_base64``, or ``knowledge_id`` (downloads the original file through the
WeKnora API at WEKNORA_BASE_URL with X-API-Key WEKNORA_API_KEY).

Transports: ``stdio`` (default) or ``streamable-http``. For http, a shared
secret is required via ``MCP_SERVER_AUTH_TOKEN`` (clients send
``Authorization: Bearer <token>`` or ``X-MCP-Auth-Token``), mirroring the
repo's mcp-server security convention.
"""

from __future__ import annotations

import argparse
import base64
import binascii
import json
import logging
import os
import sys
import urllib.request
from pathlib import Path
from typing import Optional

from .checker import check_document, inspect_document, labeling_task_for
from .rules import available_types

logger = logging.getLogger("docformat.mcp")

MAX_FETCH_BYTES = 50 * 1024 * 1024  # 50 MiB cap for url/knowledge downloads


def _allowed_dirs() -> list:
    raw = os.getenv("DOCFORMAT_ALLOWED_DIRS", "").strip()
    if raw:
        return [Path(p).resolve() for p in raw.split(",") if p.strip()]
    return [Path.cwd().resolve()]


def _read_local(path_str: str) -> bytes:
    path = Path(path_str).expanduser().resolve()
    roots = _allowed_dirs()
    if not any(str(path).startswith(str(r) + os.sep) or path == r for r in roots):
        raise ValueError(
            "file_path %r is outside the allowed directories (%s); "
            "set DOCFORMAT_ALLOWED_DIRS" % (path_str, ", ".join(map(str, roots))))
    return path.read_bytes()


def _fetch_url(url: str) -> bytes:
    if not url.lower().startswith(("http://", "https://")):
        raise ValueError("file_url must be http(s)")
    req = urllib.request.Request(url, headers={"User-Agent": "docformat-mcp/1.0"})
    with urllib.request.urlopen(req, timeout=60) as resp:
        data = resp.read(MAX_FETCH_BYTES + 1)
    if len(data) > MAX_FETCH_BYTES:
        raise ValueError("file_url exceeds the %d-byte fetch cap" % MAX_FETCH_BYTES)
    return data


def _fetch_knowledge(knowledge_id: str) -> bytes:
    base = os.getenv("WEKNORA_BASE_URL", "http://localhost:8080/api/v1").rstrip("/")
    key = os.getenv("WEKNORA_API_KEY", "")
    url = "%s/knowledge/%s/download" % (base, knowledge_id)
    req = urllib.request.Request(url, headers={"X-API-Key": key})
    with urllib.request.urlopen(req, timeout=60) as resp:
        data = resp.read(MAX_FETCH_BYTES + 1)
    if len(data) > MAX_FETCH_BYTES:
        raise ValueError("knowledge file exceeds the fetch cap")
    return data


def resolve_content(file_path=None, file_url=None, file_base64=None,
                    knowledge_id=None) -> tuple:
    """Return (content_bytes, source_label)."""
    given = [x is not None for x in (file_path, file_url, file_base64, knowledge_id)]
    if sum(given) != 1:
        raise ValueError(
            "pass exactly one of file_path, file_url, file_base64, knowledge_id")
    if file_path is not None:
        return _read_local(file_path), file_path
    if file_url is not None:
        return _fetch_url(file_url), file_url
    if file_base64 is not None:
        try:
            return base64.b64decode(file_base64, validate=True), "<base64>"
        except (binascii.Error, ValueError) as exc:
            raise ValueError("file_base64 is not valid base64: %s" % exc)
    return _fetch_knowledge(str(knowledge_id)), "knowledge:%s" % knowledge_id


def _ensure_docx(content: bytes, source: str):
    if not content[:2] == b"PK":
        raise ValueError(
            "%r is not a .docx (PK zip signature missing); only .docx is "
            "supported for thể-thức checks" % source)


# ---------------------------------------------------------------------------
# MCP wiring


def build_server():
    try:
        from mcp.server import MCPServer
    except ImportError:
        print(
            "The 'mcp' package is required for docformat.mcp_server. "
            "Install it with: pip install 'mcp>=2,<3'",
            file=sys.stderr,
        )
        raise

    mcp = MCPServer("docformat", version="0.1.0")

    @mcp.tool()
    def list_document_types() -> dict:
        """List document types with a dedicated NĐ30 rule set (plus 'base'
        used as fallback for any other type)."""
        return {"document_types": available_types(), "fallback": "base"}

    @mcp.tool()
    def inspect_docx_layout(
        file_path: Optional[str] = None,
        file_url: Optional[str] = None,
        file_base64: Optional[str] = None,
        knowledge_id: Optional[str] = None,
        include_runs: bool = False,
    ) -> dict:
        """Bóc tách văn bản .docx: danh sách paragraph với font, cỡ chữ, căn
        lề, thụt đầu dòng, zone (trái/phải/đầy đủ), bảng, margin trang — và
        gán nhãn thành phần (quốc hiệu, tiêu ngữ, cơ quan, số ký hiệu, địa
        danh-ngày tháng, trích yếu, nội dung, khối ký, nơi nhận). Đây là bước
        bắt buộc trước khi đánh giá thể thức."""
        content, source = resolve_content(file_path, file_url, file_base64,
                                          knowledge_id)
        _ensure_docx(content, source)
        out = inspect_document(content, include_runs=include_runs)
        out["source"] = source
        return out

    @mcp.tool()
    def get_component_labeling_task(
        file_path: Optional[str] = None,
        file_url: Optional[str] = None,
        file_base64: Optional[str] = None,
        knowledge_id: Optional[str] = None,
    ) -> dict:
        """Bước 1 (khuyến nghị) để chấm thể thức chính xác: trả về danh sách
        đơn vị văn bản (units: id, text, zone, căn lề, cỡ chữ, đậm...) cùng
        định nghĩa các thành phần thể thức theo NĐ30. Hãy đọc kỹ
        `instructions`, gán MỖI unit id vào đúng một thành phần theo vai trò
        của dòng, rồi gọi check_document_format với
        labels={"document_type": "...", "labels": {"<id>": "<thành phần>"}}."""
        content, source = resolve_content(file_path, file_url, file_base64,
                                          knowledge_id)
        _ensure_docx(content, source)
        out = labeling_task_for(content)
        out["source"] = source
        return out

    @mcp.tool()
    def check_document_format(
        file_path: Optional[str] = None,
        file_url: Optional[str] = None,
        file_base64: Optional[str] = None,
        knowledge_id: Optional[str] = None,
        document_type: str = "auto",
        labels: Optional[dict] = None,
        segmenter: str = "auto",
    ) -> dict:
        """Chấm thể thức một .docx theo NĐ30/2020/NĐ-CP. document_type:
        auto (tự phát hiện), một trong 29 loại văn bản hành chính của Điều 7
        NĐ30 (cong_van, quyet_dinh, ke_hoach, giay_moi, … — xem
        list_document_types), nghi_dinh, thong_tu hoặc base. labels: kết quả gán nhãn từ get_component_labeling_task
        (nên truyền — cỡ chữ/font được chấm theo thành phần, gán sai thành
        phần thì chấm sai). segmenter: auto | heuristic | llm | labels.
        Trả về checks[] với status pass/fail/warn/skip và evidence;
        segmentation cho biết cách gán nhãn đã dùng."""
        content, source = resolve_content(file_path, file_url, file_base64,
                                          knowledge_id)
        _ensure_docx(content, source)
        return check_document(content, document_type=document_type,
                              source_name=source, segmenter=segmenter,
                              labels=labels)

    return mcp


class _BearerAuth:
    """ASGI middleware: require the shared token on every request."""

    def __init__(self, app, token: str):
        self.app = app
        self.token = token

    async def __call__(self, scope, receive, send):
        if scope["type"] == "http":
            headers = {k.lower(): v for k, v in scope.get("headers", [])}
            auth = headers.get(b"authorization", b"").decode(errors="replace")
            provided = ""
            if auth.lower().startswith("bearer "):
                provided = auth[7:].strip()
            elif b"x-mcp-auth-token" in headers:
                provided = headers[b"x-mcp-auth-token"].decode(errors="replace").strip()
            if provided != self.token:
                resp = json.dumps({"error": "unauthorized"}).encode()
                await send({
                    "type": "http.response.start", "status": 401,
                    "headers": [[b"content-type", b"application/json"]],
                })
                await send({"type": "http.response.body", "body": resp})
                return
        await self.app(scope, receive, send)


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(prog="docformat-mcp")
    ap.add_argument("--transport", default="stdio",
                    choices=["stdio", "http"],
                    help="stdio for local clients; http = streamable HTTP")
    ap.add_argument("--host", default="127.0.0.1")
    ap.add_argument("--port", type=int, default=8090)
    args = ap.parse_args(argv)

    mcp = build_server()
    if args.transport == "stdio":
        import asyncio
        asyncio.run(mcp.run_stdio_async())
        return 0

    token = os.getenv("MCP_SERVER_AUTH_TOKEN", "").strip()
    if not token:
        print("MCP_SERVER_AUTH_TOKEN is required for http transport "
              "(clients must send Authorization: Bearer <token> or "
              "X-MCP-Auth-Token).", file=sys.stderr)
        return 1
    try:
        app = mcp.streamable_http_app(host=args.host, stateless_http=True)
    except AttributeError:
        print("installed 'mcp' version lacks streamable_http_app; "
              "upgrade to mcp>=2", file=sys.stderr)
        return 1
    import uvicorn
    uvicorn.run(_BearerAuth(app, token), host=args.host, port=args.port)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
