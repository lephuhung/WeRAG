#!/usr/bin/env python3
"""Run the document-assistant compliance suite against a live WeRAG backend.

    python3 run_eval.py --base-url http://127.0.0.1:18080 --token-file token.txt --tenant 10003
    python3 run_eval.py --groups spelling,rewrite          # subset by pipeline
    python3 run_eval.py --only spell_document,rewrite_formal
    python3 run_eval.py --list

Writes results/<timestamp>/report.md, results.json (every event of every
turn) and prints a summary table. Sessions are deleted afterwards unless
--keep-sessions is given.
"""
from __future__ import annotations

import argparse
import concurrent.futures as cf
import datetime as dt
import json
import os
import re
import sys
import threading
import time
import traceback
from typing import Any, Dict, List

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)

import checks as C  # noqa: E402
import scenarios as SC  # noqa: E402
from judge import Judge  # noqa: E402
from werag_client import ApiError, WeragClient  # noqa: E402

STATUS_ORDER = {"FAIL": 0, "WARN": 1, "SKIP": 2, "PASS": 3}
_print_lock = threading.Lock()


def aggregate(statuses: List[str]) -> str:
    """FAIL > WARN > PASS; SKIP only when nothing else was evaluated."""
    real = [s for s in statuses if s != "SKIP"]
    if not real:
        return "SKIP" if statuses else "PASS"
    return min(real, key=lambda s: STATUS_ORDER[s])


def log(msg: str) -> None:
    with _print_lock:
        print(msg, flush=True)


def ensure_fixtures(fixtures_dir: str) -> Dict[str, Any]:
    manifest = os.path.join(fixtures_dir, "manifest.json")
    if not os.path.exists(manifest):
        raise SystemExit(f"fixtures missing: run fixtures/build_fixtures.py first ({manifest})")
    with open(manifest, encoding="utf-8") as f:
        return json.load(f)


PARAGRAPHS_CHARS_PER_DOC = 3000
_FILLER_RE = re.compile(r"Kết quả này được ghi nhận tại cuộc họp giao ban tháng \d+ năm \d{4}\.$")
JUDGE_FACTS_CHARS = 8000


def docx_paragraphs(path: str, cap: int = PARAGRAPHS_CHARS_PER_DOC) -> List[str]:
    """Non-empty paragraphs of a fixture in body order, table cells included
    (quốc hiệu, cơ quan ban hành, chữ ký live in layout tables)."""
    try:
        from docx import Document  # python-docx, already used by build_fixtures.py
        from docx.table import Table
        from docx.text.paragraph import Paragraph
    except ImportError:
        return []
    try:
        doc = Document(path)
    except Exception:  # noqa: BLE001
        return []
    texts: List[str] = []

    def walk(parent, el):
        tag = el.tag.rsplit("}", 1)[-1]
        if tag == "p":
            texts.append(Paragraph(el, parent).text)
        elif tag == "tbl":
            for row in Table(el, parent).rows:
                for cell in row.cells:
                    for child in cell._tc.iterchildren():
                        walk(cell, child)

    for el in doc.element.body.iterchildren():
        walk(doc, el)
    out: List[str] = []
    used = 0
    seen_prev = None
    fillers = 0
    for t in texts:
        t = t.strip()
        if not t or t == seen_prev:  # merged cells repeat their text
            continue
        seen_prev = t
        if _FILLER_RE.search(t):  # bao_cao_dai padding (build_fixtures.py FILLER), no facts
            fillers += 1
            continue
        line = f"[{len(out)}] {t}"
        if used + len(line) > cap:
            out.append("… (danh sách đã cắt bớt cho giám khảo: các đoạn phía sau không hiển thị)")
            break
        out.append(line)
        used += len(line)
    else:
        if fillers:
            out.append(f"(bỏ qua {fillers} đoạn đệm dạng 'Kết quả này được ghi nhận tại cuộc họp giao ban…', không chứa số liệu)")
    return out


def judge_context(sc: SC.Scenario, manifest: Dict[str, Any], note: str, fixtures_dir: str = "") -> str:
    """Known facts of the fixtures in play, so the judge can tell real figures from invented ones."""
    facts = {}
    for _, fname in sc.documents:
        m = dict(manifest.get(fname) or {})
        m.pop("filler_paragraphs", None)
        if fixtures_dir:
            paras = docx_paragraphs(os.path.join(fixtures_dir, fname))
            if paras:
                m["paragraphs"] = paras
        facts[fname] = m
    blob = json.dumps(facts, ensure_ascii=False)
    if len(blob) > JUDGE_FACTS_CHARS:
        blob = blob[:JUDGE_FACTS_CHARS] + "…"
    return (note + "\n" if note else "") + "DỮ KIỆN CÓ THẬT TRONG CÁC TÀI LIỆU CỦA PHIÊN (từ bộ sinh fixture; số liệu xuất hiện ở đây là của văn bản, không phải bịa; \"paragraphs\" là danh sách đoạn của tài liệu): " + blob


def run_scenario(sc: SC.Scenario, client: WeragClient, manifest: Dict[str, Any], fixtures_dir: str,
                 editor_kind: str, judge: Judge, keep: bool) -> Dict[str, Any]:
    t0 = time.time()
    out: Dict[str, Any] = {"id": sc.id, "group": sc.group, "title": sc.title, "requires": sc.requires,
                           "steps": [], "status": "PASS", "session_id": None, "setup_error": None}
    ctx: Dict[str, Any] = {"client": client, "manifest": manifest, "docs": {}, "turns": [],
                           "fixtures_dir": fixtures_dir, "editor_kind": editor_kind}
    sid = None
    try:
        sid = client.create_session(f"[eval] {sc.id}")
        out["session_id"] = sid
        ctx["session_id"] = sid
        for role, fname in sc.documents:
            path = os.path.join(fixtures_dir, fname)
            if role == "target":
                view = client.add_target(sid, path, editor_kind=editor_kind)
            else:
                view = client.add_source(sid, path)
            ctx["docs"][fname] = view
            log(f"  [{sc.id}] {role} {fname} → {view.get('handle')} ({view.get('id', '')[:8]})")
        for i, step in enumerate(sc.steps):
            if step.before:
                log(f"  [{sc.id}] step {i + 1}: waiting (before hook)…")
                step.before(ctx)
            selection = step.selection(ctx) if step.selection else None
            mentions = step.mentions(ctx) if step.mentions else None
            query = step.query(ctx) if callable(step.query) else step.query
            if not query:
                log(f"  [{sc.id}] step {i + 1}: skipped (no query from the previous turn)")
                out["steps"].append({"query": "", "note": step.note, "selection": None, "mentions": None,
                                     "status": "SKIP", "checks": [{"status": "SKIP", "label": "query derived from previous turn", "detail": "nothing to send"}],
                                     "turn": {"query": "", "answer": "", "error": None, "duration_s": 0, "tools": [], "display_types": []}, "events": []})
                continue
            log(f"  [{sc.id}] step {i + 1}: {query[:70]}")
            turn = client.chat(sid, query, selection=selection, mentions=mentions, web_search=step.web_search,
                               extra=step.extra(ctx) if callable(step.extra) else step.extra)
            ctx["turns"].append(turn)
            results: List[C.Result] = []
            for chk in step.checks:
                try:
                    results.append(chk(turn, ctx))
                except Exception as e:  # noqa: BLE001
                    results.append(C.Result(C.FAIL, f"check crashed: {getattr(chk, '__name__', chk)}", f"{e!r}"))
            if step.judge:
                results.append(judge.grade(step.judge, turn, extra_context=judge_context(sc, manifest, step.note, fixtures_dir)))
            worst = aggregate([r.status for r in results])
            out["steps"].append({
                "query": query, "note": step.note, "selection": selection, "mentions": mentions,
                "status": worst, "checks": [r.as_dict() for r in results], "turn": turn.summary(),
                "events": turn.events,
            })
            log(f"  [{sc.id}] step {i + 1}: {worst} in {turn.duration_s:.0f}s; tools {turn.tool_names()}")
    except (ApiError, RuntimeError, TimeoutError) as e:
        out["setup_error"] = f"{e}"
        log(f"  [{sc.id}] SETUP ERROR: {e}")
    except Exception as e:  # noqa: BLE001
        out["setup_error"] = f"{e!r}\n{traceback.format_exc()}"
        log(f"  [{sc.id}] CRASH: {e!r}")
    finally:
        if sid and not keep:
            try:
                client.delete_session(sid)
            except Exception as e:  # noqa: BLE001
                log(f"  [{sc.id}] cleanup failed: {e}")
    statuses = [s["status"] for s in out["steps"]]
    if out["setup_error"]:
        out["status"] = "FAIL"
    elif statuses:
        out["status"] = aggregate(statuses)
    out["duration_s"] = round(time.time() - t0, 1)
    return out


def write_report(results: List[Dict[str, Any]], path: str, meta: Dict[str, Any]) -> str:
    lines = [f"# Document assistant compliance run — {meta['started']}", ""]
    lines.append(f"Backend: `{meta['base_url']}` · tenant {meta['tenant']} · editor_kind `{meta['editor_kind']}` · judge: {meta['judge']}")
    lines.append("")
    counts = {k: sum(1 for r in results if r["status"] == k) for k in ("PASS", "WARN", "FAIL", "SKIP")}
    lines.append(f"**Scenarios:** {len(results)} — PASS {counts['PASS']} · WARN {counts['WARN']} · FAIL {counts['FAIL']}")
    step_counts = {k: 0 for k in ("PASS", "WARN", "FAIL", "SKIP")}
    for r in results:
        for s in r["steps"]:
            for c in s["checks"]:
                step_counts[c["status"]] = step_counts.get(c["status"], 0) + 1
    lines.append(f"**Checks:** PASS {step_counts['PASS']} · WARN {step_counts['WARN']} · FAIL {step_counts['FAIL']} · SKIP {step_counts['SKIP']}")
    lines.append("")
    lines.append("| # | Pipeline | Scenario | Status | Time | Tools called | Failed / warned checks |")
    lines.append("|---|---|---|---|---|---|---|")
    for i, r in enumerate(results, 1):
        tools = []
        bad = []
        for s in r["steps"]:
            tools.append(" → ".join(t["name"] for t in s["turn"]["tools"]) or "–")
            for c in s["checks"]:
                if c["status"] in ("FAIL", "WARN"):
                    bad.append(f"{c['status']}: {c['label']}")
        if r["setup_error"]:
            bad.append("setup: " + r["setup_error"].splitlines()[0][:120])
        lines.append(f"| {i} | {r['group']} | {r['id']} — {r['title']} | **{r['status']}** | {r['duration_s']:.0f}s | {' ; '.join(tools)} | {'<br>'.join(bad) or '–'} |")
    lines.append("")
    lines.append("## Chi tiết")
    for r in results:
        lines.append("")
        lines.append(f"### {r['id']} — {r['title']} [{r['status']}]")
        if r["requires"]:
            lines.append(f"_Yêu cầu hạ tầng: {r['requires']}_")
        if r["setup_error"]:
            lines.append(f"Setup error: `{r['setup_error'].splitlines()[0][:300]}`")
        for j, s in enumerate(r["steps"], 1):
            t = s["turn"]
            lines.append("")
            lines.append(f"**Lượt {j}** ({s['status']}, {t['duration_s']}s): `{s['query']}`")
            if s["selection"]:
                lines.append(f"- bôi đen: `{s['selection'].get('text', '')[:120]}`" + (f" (lần {s['selection']['paragraph_occurrence']})" if s['selection'].get('paragraph_occurrence') else ""))
            if s["mentions"]:
                lines.append(f"- @: {', '.join(m['name'] for m in s['mentions'])}")
            if s["note"]:
                lines.append(f"- ghi chú: {s['note']}")
            for tc in t["tools"]:
                args = json.dumps(tc["arguments"], ensure_ascii=False)
                lines.append(f"- tool `{tc['name']}` success={tc['success']} ops={tc['ops'][:6]} args=`{args[:160]}`" + (f" error=`{tc['error']}`" if tc.get("error") else ""))
            for c in s["checks"]:
                mark = {"PASS": "✅", "WARN": "⚠️", "FAIL": "❌", "SKIP": "⏭️"}[c["status"]]
                lines.append(f"- {mark} {c['label']}" + (f" — {c['detail'][:300]}" if c["detail"] else ""))
            ans = (t["answer"] or "").strip().replace("\n", " ")
            lines.append(f"- trả lời: {ans[:500]}{'…' if len(ans) > 500 else ''}")
            if t["error"]:
                lines.append(f"- lỗi lượt: `{t['error'][:200]}`")
    text = "\n".join(lines) + "\n"
    with open(path, "w", encoding="utf-8") as f:
        f.write(text)
    return text


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--base-url", default=os.environ.get("WERAG_BASE_URL", "http://127.0.0.1:18080"))
    ap.add_argument("--token", default=os.environ.get("WERAG_TOKEN", ""))
    ap.add_argument("--token-file", default="")
    ap.add_argument("--tenant", type=int, default=int(os.environ.get("WERAG_TENANT", "0") or 0))
    ap.add_argument("--fixtures", default=os.path.join(HERE, "fixtures", "out"))
    ap.add_argument("--editor", default="word_addin", choices=["word_addin", "onlyoffice"],
                    help="editor_kind used to open targets; word_addin needs no editor server")
    ap.add_argument("--only", default="", help="comma-separated scenario ids")
    ap.add_argument("--groups", default="", help="comma-separated pipelines: " + ",".join(SC.GROUPS))
    ap.add_argument("--skip", default="", help="comma-separated scenario ids to skip")
    ap.add_argument("--parallel", type=int, default=1, help="scenarios run concurrently (each has its own session)")
    ap.add_argument("--out", default=os.path.join(HERE, "results"))
    ap.add_argument("--keep-sessions", action="store_true")
    ap.add_argument("--list", action="store_true")
    ap.add_argument("--turn-timeout", type=float, default=900)
    ap.add_argument("--retries", type=int, default=2, help="re-run a scenario whose turn died on a transport error (backend restart)")
    args = ap.parse_args()

    ids = [x for x in args.only.split(",") if x]
    groups = [x for x in args.groups.split(",") if x]
    skip = {x for x in args.skip.split(",") if x}
    selected = [s for s in SC.select(ids, groups) if s.id not in skip]
    if args.list:
        for s in selected:
            print(f"{s.group:9s} {s.id:32s} {len(s.steps)} step(s)  {s.title}")
        return 0
    if not selected:
        raise SystemExit("no scenario selected")

    token = args.token
    if args.token_file:
        with open(args.token_file, encoding="utf-8") as f:
            token = f.read().strip()
    if not token:
        raise SystemExit("token missing (--token / --token-file / WERAG_TOKEN); see mint_token.py")

    manifest = ensure_fixtures(args.fixtures)
    client = WeragClient(args.base_url, token, tenant_id=args.tenant or None, timeout=args.turn_timeout)
    me = client.me()  # fail fast on a bad token
    tenant = (me.get("data") or {}).get("tenant", {}).get("id") if isinstance(me, dict) else None
    judge = Judge()
    started = dt.datetime.now().strftime("%Y-%m-%d %H:%M:%S")
    run_dir = os.path.join(args.out, dt.datetime.now().strftime("%Y%m%d-%H%M%S"))
    os.makedirs(run_dir, exist_ok=True)
    log(f"run dir {run_dir}; {len(selected)} scenarios; tenant {tenant}; judge {'on' if judge.enabled else 'off'}")

    results: List[Dict[str, Any]] = []

    def transport_failed(r: Dict[str, Any]) -> bool:
        if r.get("setup_error") and "Connection" in r["setup_error"]:
            return True
        return any((s["turn"].get("error") or "").startswith("transport:") for s in r["steps"])

    def work(sc: SC.Scenario) -> Dict[str, Any]:
        log(f"▶ {sc.id} ({sc.group}) — {sc.title}")
        r = run_scenario(sc, client, manifest, args.fixtures, args.editor, judge, args.keep_sessions)
        # a dev backend restarts on hot reload and cuts the SSE stream:
        # that is not the agent's doing, so the scenario runs again
        for attempt in range(args.retries):
            if not transport_failed(r):
                break
            log(f"  [{sc.id}] transport failure; retrying in 30s ({attempt + 1}/{args.retries})")
            time.sleep(30)
            r = run_scenario(sc, client, manifest, args.fixtures, args.editor, judge, args.keep_sessions)
            r["retried"] = attempt + 1
        return r

    if args.parallel > 1:
        with cf.ThreadPoolExecutor(max_workers=args.parallel) as ex:
            results = list(ex.map(work, selected))
    else:
        for sc in selected:
            results.append(work(sc))
            # write partial results so a long run can be inspected while it goes
            with open(os.path.join(run_dir, "results.json"), "w", encoding="utf-8") as f:
                json.dump(results, f, ensure_ascii=False, indent=1)

    meta = {"started": started, "base_url": args.base_url, "tenant": tenant, "editor_kind": args.editor,
            "judge": f"{judge.model}@{judge.base_url}" if judge.enabled else "off"}
    with open(os.path.join(run_dir, "results.json"), "w", encoding="utf-8") as f:
        json.dump(results, f, ensure_ascii=False, indent=1)
    report = write_report(results, os.path.join(run_dir, "report.md"), meta)
    print(report.split("## Chi tiết")[0])
    print(f"report: {os.path.join(run_dir, 'report.md')}")
    return 0 if all(r["status"] != "FAIL" for r in results) else 1


if __name__ == "__main__":
    sys.exit(main())
