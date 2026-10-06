"""Minimal upload web app for trying the checker in a browser.

    python -m docformat.webapp --host 0.0.0.0 --port 8090

Routes:
    GET  /           upload form (chọn .docx + loại văn bản)
    POST /check      multipart upload → HTML report
    POST /api/check  multipart upload → JSON report
    GET  /api/types  rule-set list (JSON)
"""

from __future__ import annotations

import argparse
import html
import json
import os
import time

UPLOAD_DIR = os.environ.get("DOCFORMAT_UPLOAD_DIR")

from starlette.applications import Starlette
from starlette.concurrency import run_in_threadpool
from starlette.requests import Request
from starlette.responses import HTMLResponse, JSONResponse
from starlette.routing import Route

from .checker import check_document
from .rules import available_types

MAX_UPLOAD = 30 * 1024 * 1024

_PAGE = """<!doctype html>
<html lang="vi"><head><meta charset="utf-8">
<title>Kiểm tra thể thức văn bản (NĐ30/2020)</title>
<style>
 body{{font-family:system-ui,sans-serif;max-width:960px;margin:2rem auto;padding:0 1rem;color:#222}}
 .card{{border:1px solid #ddd;border-radius:10px;padding:1.2rem;margin-bottom:1rem}}
 .pass{{border-left:4px solid #1a7f37}}
 .fail{{border-left:4px solid #cf222e}}
 .warn{{border-left:4px solid #9a6700}}
 .skip{{border-left:4px solid #999}}
 .badge{{display:inline-block;min-width:70px;text-align:center;border-radius:6px;
        font-weight:600;font-size:.8rem;padding:2px 8px;margin-right:8px;color:#fff}}
 .b-pass{{background:#1a7f37}} .b-fail{{background:#cf222e}}
 .b-warn{{background:#9a6700}} .b-skip{{background:#777}}
 .ev{{font-family:ui-monospace,monospace;font-size:.85rem;background:#f6f8fa;
      padding:.4rem .6rem;border-radius:6px;margin:.2rem 0;white-space:pre-wrap}}
 small{{color:#666}}
 select,input[type=file]{{margin:.3rem 0}}
 button{{padding:.6rem 1.4rem;font-size:1rem;border-radius:8px;border:0;
         background:#0969da;color:#fff;cursor:pointer}}
</style></head><body>
<h1>Kiểm tra thể thức văn bản</h1>
<p><small>Theo Nghị định 30/2020/NĐ-CP, Phụ lục I — chỉ hỗ trợ .docx.
File xử lý trong bộ nhớ, không lưu lại.</small></p>
<div class="card">
 <form method="post" action="/check" enctype="multipart/form-data">
  <div><label>File .docx: <input type="file" name="file" accept=".docx" required></label></div>
  <div><label>Loại văn bản:
   <select name="document_type">
    <option value="auto">Tự phát hiện</option>
    {type_options}
   </select></label></div>
  <div><label>Gán thành phần:
   <select name="segmenter">
    <option value="auto">{seg_default}</option>
    <option value="llm">LLM</option>
    <option value="heuristic">Heuristic (không dùng LLM)</option>
   </select></label></div>
  <div><button type="submit">Kiểm tra</button></div>
 </form>
</div>
{result}
</body></html>"""


def _badge(status: str) -> str:
    label = {"pass": "ĐẠT", "fail": "SAI", "warn": "CẢNH BÁO",
             "skip": "BỎ QUA"}.get(status, status)
    return '<span class="badge b-%s">%s</span>' % (html.escape(status),
                                                  html.escape(label))


def _esc(v) -> str:
    return html.escape(json.dumps(v, ensure_ascii=False)
                       if isinstance(v, (dict, list)) else str(v))


def _render_report(rep: dict) -> str:
    if not rep.get("ok"):
        return ('<div class="card fail"><h2>Không xử lý được file</h2><p>%s</p></div>'
                % _esc(rep.get("error", "?")))
    dt = rep.get("document_type", {})
    s = rep.get("summary", {})
    sg = rep.get("segmentation", {})
    parts = [
        '<div class="card"><h2>%s</h2>'
        '<p>Loại văn bản — phát hiện: <b>%s</b> · bộ luật dùng: <b>%s</b> (%s)</p>'
        '<p>Gán thành phần: <b>%s</b>%s · %.1fs</p>'
        '<p><b>%d</b> đạt · <b>%d</b> sai · <b>%d</b> cảnh báo · <b>%d</b> bỏ qua</p></div>'
        % (_esc(rep.get("source", "")), _esc(dt.get("detected")),
           _esc(dt.get("used")), _esc(dt.get("rule_set", "")),
           _esc(sg.get("method")),
           _esc(" (%s)" % sg["model"] if sg.get("model") else "")
           + _esc(" — %s" % sg["error"] if sg.get("error") else ""),
           rep.get("elapsed_s", 0.0),
           s.get("pass", 0), s.get("fail", 0), s.get("warn", 0), s.get("skip", 0))
    ]
    dis = sg.get("disagreements") or []
    if dis or sg.get("missing") or sg.get("invalid"):
        rows = "".join(
            "<tr><td>%s</td><td>%s</td><td><b>%s</b></td><td>%s</td></tr>" % (
                _esc(d["id"]), _esc(d["text"]), _esc(d["llm"]),
                _esc(d["heuristic"])) for d in dis)
        parts.append(
            '<div class="card"><details><summary>%d dòng LLM gán khác '
            'heuristic%s</summary><table cellspacing="4"><tr><th>unit</th>'
            '<th>dòng</th><th>LLM</th><th>heuristic</th></tr>%s</table>'
            '</details></div>'
            % (len(dis), _esc(" · thiếu nhãn: %s" % sg["missing"])
               if sg.get("missing") else "", rows))
    order = {"fail": 0, "warn": 1, "skip": 2, "pass": 3}
    checks = sorted(rep.get("checks", []), key=lambda c: order.get(c["status"], 4))
    for c in checks:
        body = [
            '<div class="card %s"><div>%s<b>%s</b> — %s</div>'
            % (c["status"], _badge(c["status"]), _esc(c["id"]), _esc(c.get("desc", "")))
        ]
        if c.get("note"):
            body.append('<div class="ev">%s</div>' % _esc(c["note"]))
        for ev in c.get("evidence", [])[:5]:
            if isinstance(ev, dict) and "text" in ev:
                body.append('<div class="ev">đoạn %s: %s — thực tế: %s</div>'
                            % (_esc(ev.get("para")), _esc(ev.get("text")),
                               _esc(ev.get("actual"))))
            else:
                body.append('<div class="ev">%s</div>' % _esc(ev))
        if isinstance(c.get("actual"), dict) and "satisfied" in c["actual"]:
            body.append('<small>%s</small>' % _esc(c["actual"]))
        body.append("</div>")
        parts.append("".join(body))
    comps = rep.get("components", {})
    rows = "".join(
        "<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>" % (
            _esc(k),
            "✓" if v.get("found") else "—",
            _esc(v.get("zone") or ""),
            _esc((v.get("text") or "")[:90]),
        ) for k, v in comps.items())
    parts.append('<div class="card"><h3>Thành phần bóc tách được</h3>'
                 '<table border="0" cellspacing="4">%s</table></div>' % rows)
    return "".join(parts)


async def _read_upload(request: Request):
    form = await request.form(max_part_size=MAX_UPLOAD)
    upload = form.get("file")
    if upload is None or not getattr(upload, "filename", ""):
        raise ValueError("chưa chọn file")
    if not upload.filename.lower().endswith(".docx"):
        raise ValueError("chỉ hỗ trợ .docx — nhận được %r" % upload.filename)
    doc_type = str(form.get("document_type") or "auto")
    segmenter = str(form.get("segmenter") or "auto")
    if segmenter not in ("auto", "llm", "heuristic"):
        raise ValueError("segmenter không hợp lệ: %r" % segmenter)
    content = await upload.read()
    if UPLOAD_DIR:
        os.makedirs(UPLOAD_DIR, exist_ok=True)
        safe = "".join(c if c.isalnum() or c in "._-" else "_"
                       for c in upload.filename)
        path = os.path.join(UPLOAD_DIR, "%d_%s" % (time.time(), safe))
        with open(path, "wb") as fh:
            fh.write(content)
    return content, upload.filename, doc_type, segmenter


async def _check(content, name, doc_type, segmenter) -> dict:
    """check_document blocks for seconds when it calls the LLM — run it off
    the event loop so concurrent uploads are not serialized behind it."""
    t0 = time.time()
    rep = await run_in_threadpool(check_document, content,
                                  document_type=doc_type, source_name=name,
                                  segmenter=segmenter)
    rep["elapsed_s"] = round(time.time() - t0, 2)
    return rep


def _page(result: str) -> str:
    opts = "".join('<option value="%s">%s</option>' % (t, t)
                   for t in available_types())
    from .llm import LLMConfig
    try:
        cfg = LLMConfig.from_env()
    except Exception:
        cfg = None
    seg_default = ("Tự động (LLM: %s)" % (
        "model chat mặc định của WeKnora" if cfg.backend == "weknora"
        and cfg.model == "default" else cfg.model)) if cfg else \
        "Tự động (chưa cấu hình LLM → heuristic)"
    return _PAGE.format(type_options=opts, result=result,
                        seg_default=html.escape(seg_default))


async def index(request: Request):
    return HTMLResponse(_page(""))


async def check_form(request: Request):
    try:
        content, name, doc_type, segmenter = await _read_upload(request)
        rep = await _check(content, name, doc_type, segmenter)
    except ValueError as exc:
        rep = {"ok": False, "error": str(exc)}
    return HTMLResponse(_page(_render_report(rep)))


async def check_api(request: Request):
    try:
        content, name, doc_type, segmenter = await _read_upload(request)
        rep = await _check(content, name, doc_type, segmenter)
    except ValueError as exc:
        return JSONResponse({"ok": False, "error": str(exc)}, status_code=400)
    return JSONResponse(rep)


async def types_api(request: Request):
    return JSONResponse({"document_types": available_types(), "fallback": "base"})


def create_app() -> Starlette:
    return Starlette(routes=[
        Route("/", index, methods=["GET"]),
        Route("/check", check_form, methods=["POST"]),
        Route("/api/check", check_api, methods=["POST"]),
        Route("/api/types", types_api, methods=["GET"]),
    ])


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(prog="docformat-web")
    ap.add_argument("--host", default="127.0.0.1")
    ap.add_argument("--port", type=int, default=8090)
    args = ap.parse_args(argv)
    import uvicorn
    uvicorn.run(create_app(), host=args.host, port=args.port)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
