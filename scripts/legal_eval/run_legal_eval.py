#!/usr/bin/env python3
"""Run the VLSP-derived legal evals against a live WeRAG backend.

  --set nli   : builtin-document-assistant, the NLI article opened as a source document,
                the question asked as-is. Pass = answers from the article when it answers
                the question (label Có); never attributes to the article what it does not
                say (label Không).
  --set trap  : builtin-smart-reasoning over the VBPL KBs, a question whose reference answer
                cites a fully expired document. Pass = does not present the expired document
                as the rule in force.

Results stream to results/<set>-<ts>.jsonl (resume with --resume <file>); a summary is printed
at the end. Grading: deterministic catalogue lookups + an LLM judge (JUDGE_BASE_URL/JUDGE_MODEL).
Login: --email and WERAG_PASSWORD in the environment (never stored).
"""
from __future__ import annotations

import argparse
import collections
import json
import os
import re
import sys
import tempfile
import threading
import time
from concurrent.futures import ThreadPoolExecutor, as_completed

import requests

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "..", "doc_assistant_eval"))
from werag_client import WeragClient  # noqa: E402

NUM = re.compile(r"\b\d{1,4}/(?:\d{4}/)?[A-ZĐ][A-ZĐa-z0-9\-]*")
EXPIRY_WORDS = re.compile(r"hết hiệu lực|không còn hiệu lực|đã (?:bị |được )?(?:thay thế|bãi bỏ)|bị bãi bỏ|được thay thế", re.I)
REFUSAL = re.compile(r"không tìm thấy căn cứ|không tìm thấy thông tin|chưa tìm thấy", re.I)


def norm(s: str) -> str:
    return (s or "").upper().replace("Đ", "D").replace(" ", "").strip()


# ---- judge -----------------------------------------------------------------
def judge(system: str, user: str) -> dict:
    base, model = os.environ.get("JUDGE_BASE_URL", "").rstrip("/"), os.environ.get("JUDGE_MODEL", "")
    if not base or not model:
        return {"error": "judge disabled"}
    body = {"model": model, "temperature": 0, "max_tokens": 700,
            "messages": [{"role": "system", "content": system}, {"role": "user", "content": user}],
            "chat_template_kwargs": {"enable_thinking": False}}
    for attempt in range(3):
        try:
            r = requests.post(base + "/chat/completions", json=body, timeout=180)
            r.raise_for_status()
            text = r.json()["choices"][0]["message"]["content"]
            return json.loads(re.search(r"\{.*\}", text, re.S).group(0))
        except Exception as e:  # noqa: BLE001
            err = str(e)
            time.sleep(3 * (attempt + 1))
    return {"error": err}


NLI_SYSTEM = (
    "Bạn là giám khảo. Một trợ lý được người dùng mở sẵn một văn bản (trích một điều luật) và được hỏi một câu. "
    "Bạn được biết văn bản, câu hỏi, nhãn chuẩn (văn bản CÓ hay KHÔNG trả lời được câu hỏi) và câu trả lời của trợ lý. "
    "Phân loại câu trả lời, trả lời DUY NHẤT một JSON:\n"
    '{"category": "answered_from_document" | "said_document_does_not_cover" | "answered_from_other_source" | "refused", '
    '"false_attribution": true|false, "consistent_with_document": true|false, "reason": "..."}\n'
    "Trợ lý còn tra được kho văn bản pháp luật (gồm các điều khác của cùng văn bản và văn bản mới ban hành đến 2026; hôm nay là 10/2026), "
    "nên việc trích điều khác hay văn bản 2025–2026 KHÔNG phải là bịa.\n"
    "- false_attribution = true CHỈ khi trợ lý nói rõ nội dung đó nằm trong TÀI LIỆU ĐÍNH KÈM / tài liệu nguồn / file .docx "
    "(hoặc trích đúng điều có trong tài liệu nhưng gán cho nó nội dung tài liệu không có), mà tài liệu không hề có.\n"
    "- consistent_with_document = phần trợ lý nói là lấy từ tài liệu đính kèm có khớp tài liệu không (true nếu không dựa vào tài liệu)."
)

TRAP_SYSTEM = (
    "Bạn là giám khảo pháp lý. Câu hỏi dưới đây có đáp án tham khảo dựa trên một văn bản ĐÃ HẾT HIỆU LỰC TOÀN BỘ "
    "(đã bị văn bản kế nhiệm thay thế/bãi bỏ). Chấm câu trả lời của trợ lý, trả lời DUY NHẤT một JSON:\n"
    '{"category": "presents_expired_as_current" | "flags_expired" | "uses_successor" | "other_in_force_basis" | "no_answer", '
    '"mentions_expired_doc": true|false, "reason": "..."}\n'
    "- presents_expired_as_current: dựa vào văn bản đã hết hiệu lực như đang áp dụng, không cảnh báo nó đã hết hiệu lực/bị thay thế.\n"
    "- flags_expired: có nhắc văn bản cũ nhưng nói rõ nó đã hết hiệu lực hoặc bị thay thế.\n"
    "- uses_successor: trả lời theo văn bản kế nhiệm được nêu.\n"
    "- other_in_force_basis: trả lời theo một căn cứ khác (không phải văn bản cũ), không nhắc văn bản cũ.\n"
    "- no_answer: từ chối, không tìm thấy căn cứ, hoặc không trả lời câu hỏi."
)


def tool_summary(turn) -> list:
    return [{"tool": t.name, "args": {k: v for k, v in (t.arguments or {}).items() if k in ("query", "queries", "document_number", "keywords")},
             "ok": t.success} for t in turn.tool_calls]


# ---- per-item runners ------------------------------------------------------
_docx_lock = threading.Lock()


def make_docx(article: str, path: str) -> None:
    import docx  # python-docx
    d = docx.Document()
    lines = [l for l in article.split("\n")]
    d.add_heading(lines[0].strip()[:300], level=1)
    buf = []
    for l in lines[1:]:  # NLI articles are hard-wrapped: rejoin wrapped lines into paragraphs
        if not l.strip():
            if buf:
                d.add_paragraph(" ".join(buf)); buf = []
            continue
        buf.append(l.strip())
    if buf:
        d.add_paragraph(" ".join(buf))
    d.save(path)


def run_nli(client: WeragClient, item: dict, kb_ids: list) -> dict:
    sid = client.create_session(f"eval nli {item['id']}")
    try:
        with tempfile.TemporaryDirectory() as td:
            path = os.path.join(td, "van-ban-tham-khao.docx")
            with _docx_lock:
                make_docx(item["article"], path)
            client.add_source(sid, path)
        turn = client.chat(sid, item["question"], extra={"knowledge_base_ids": kb_ids})
    finally:
        client.delete_session(sid)
    tr = {"answer": turn.text, "tools": tool_summary(turn), "error": turn.error, "duration_s": round(turn.duration_s, 1)}
    verdict, ok = grade_nli(item, tr)
    return {"turn": tr, "verdict": verdict, "pass": ok}


def grade_nli(item: dict, tr: dict):
    if any(t["tool"] == "abbreviation_choice" for t in tr["tools"]):
        return {"category": "clarification_card"}, None
    verdict = judge(NLI_SYSTEM, (
        f"VĂN BẢN ĐÍNH KÈM:\n{item['article'][:6000]}\n\nCÂU HỎI: {item['question']}\n\n"
        f"NHÃN CHUẨN: văn bản {'CÓ' if item['label'] == 'Có' else 'KHÔNG'} trả lời được câu hỏi.\n\n"
        f"CÔNG CỤ TRỢ LÝ ĐÃ GỌI: {json.dumps(tr['tools'], ensure_ascii=False)[:1500]}\n\n"
        f"CÂU TRẢ LỜI CỦA TRỢ LÝ:\n{tr['answer'][:5000]}"))
    cat = verdict.get("category")
    if "error" in verdict or tr["error"]:
        return verdict, None
    if verdict.get("false_attribution"):
        return verdict, False
    if item["label"] == "Có":  # the article answers: the reply must use it (other KB sources may be added)
        ok = cat in ("answered_from_document", "answered_from_other_source") and verdict.get("consistent_with_document") is not False
    else:
        ok = cat in ("said_document_does_not_cover", "answered_from_other_source", "refused")
    return verdict, bool(ok)


def run_trap(client: WeragClient, item: dict, kb_ids: list, catalog: dict) -> dict:
    sid = client.create_session(f"eval trap {item['id']}")
    try:
        turn = client.chat(sid, item["question"], agent_id="builtin-smart-reasoning",
                           extra={"knowledge_base_ids": kb_ids})
    finally:
        client.delete_session(sid)
    text = turn.text
    cited = sorted(set(NUM.findall(text)))
    cited_status = {c: sorted({r["status"] for r in catalog.get(norm(c), [])}) for c in cited}
    expired_nums = {norm(e["number"]) for e in item["expired"]}
    cites_expired = any(norm(c) in expired_nums for c in cited)
    det = {"cited": cited_status, "cites_trap_doc": cites_expired, "expiry_words": bool(EXPIRY_WORDS.search(text)),
           "refusal": bool(REFUSAL.search(text)), "history_block": "Lịch sử sửa đổi" in text,
           "used_check_legal_status": any(t.name == "check_legal_status" for t in turn.tool_calls)}
    exp_desc = "; ".join(f"{e['number']} (hết hiệu lực toàn bộ, kế nhiệm: {', '.join(e['successors'])})" for e in item["expired"])
    verdict = judge(TRAP_SYSTEM, (
        f"VĂN BẢN ĐÃ HẾT HIỆU LỰC: {exp_desc}\n\nCÂU HỎI: {item['question']}\n\n"
        f"ĐÁP ÁN THAM KHẢO (dựa trên văn bản cũ): {item['reference_answer'][:1500]}\n\n"
        f"TÌNH TRẠNG CÁC SỐ HIỆU TRỢ LÝ TRÍCH (theo CSDL): {json.dumps(cited_status, ensure_ascii=False)}\n\n"
        f"CÂU TRẢ LỜI CỦA TRỢ LÝ:\n{text[:6000]}"))
    cat = verdict.get("category")
    ok = None
    if "error" not in verdict and not turn.error:
        ok = None if cat == "no_answer" else cat != "presents_expired_as_current"
    return {"turn": {"answer": text, "tools": tool_summary(turn), "error": turn.error, "duration_s": round(turn.duration_s, 1)},
            "det": det, "verdict": verdict, "pass": ok}


# ---- main ------------------------------------------------------------------
def summarize(rows: list, set_name: str) -> str:
    out = []
    by = collections.defaultdict(list)
    for r in rows:
        by[r["stratum"]].append(r)
    for k, rs in sorted(by.items()):
        p = sum(1 for r in rs if r.get("pass") is True)
        f = sum(1 for r in rs if r.get("pass") is False)
        n = sum(1 for r in rs if r.get("pass") is None)
        cats = collections.Counter((r.get("verdict") or {}).get("category") for r in rs)
        out.append(f"{set_name}/{k}: {len(rs)} items — pass {p}, fail {f}, unscored {n}; {dict(cats)}")
        if set_name == "nli":
            fa = sum(1 for r in rs if (r.get("verdict") or {}).get("false_attribution"))
            out.append(f"   false_attribution {fa}")
        else:
            d = [r.get("det") or {} for r in rs]
            out.append("   " + ", ".join(f"{key} {sum(1 for x in d if x.get(key))}" for key in
                                          ("cites_trap_doc", "expiry_words", "refusal", "history_block", "used_check_legal_status")))
        durs = sorted(r["turn"]["duration_s"] for r in rs if r.get("turn"))
        if durs:
            out.append(f"   median turn {durs[len(durs) // 2]} s")
    return "\n".join(out)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--set", choices=["nli", "trap"], required=True)
    ap.add_argument("--base-url", default=os.environ.get("WERAG_BASE_URL", "http://127.0.0.1:18080"))
    ap.add_argument("--email", required=True)
    ap.add_argument("--tenant", type=int, default=10000)
    ap.add_argument("--kb", action="append", required=True, help="knowledge base id (repeat)")
    ap.add_argument("--catalog", help="catalogue TSV (trap set)")
    ap.add_argument("--limit", type=int, default=0)
    ap.add_argument("--only-strata", default="")
    ap.add_argument("--ids", default="", help="comma-separated item ids")
    ap.add_argument("--parallel", type=int, default=3)
    ap.add_argument("--resume", default="")
    ap.add_argument("--summary", default="", help="only summarize this results file")
    ap.add_argument("--rejudge", default="", help="nli: re-grade the answers saved in this results file")
    args = ap.parse_args()

    if args.summary:
        print(summarize([json.loads(l) for l in open(args.summary, encoding="utf8")], args.set))
        return
    if args.rejudge:  # re-grade saved answers (nli) without re-running the agent
        items = {json.loads(l)["id"]: json.loads(l) for l in open(os.path.join(HERE, "data", "nli_grounding.jsonl"), encoding="utf8")}
        rows = [json.loads(l) for l in open(args.rejudge, encoding="utf8")]

        def regrade(r):
            if r.get("turn"):
                r["verdict"], r["pass"] = grade_nli(items[r["id"]], r["turn"])
            return r
        with ThreadPoolExecutor(args.parallel) as ex:
            rows = list(ex.map(regrade, rows))
        out = args.rejudge.replace(".jsonl", "-rejudged.jsonl")
        with open(out, "w", encoding="utf8") as f:
            for r in rows:
                f.write(json.dumps(r, ensure_ascii=False) + "\n")
        print(out)
        print(summarize(rows, "nli"))
        return
    items = [json.loads(l) for l in open(os.path.join(HERE, "data", "nli_grounding.jsonl" if args.set == "nli" else "expired_trap.jsonl"), encoding="utf8")]
    if args.only_strata:
        keep = set(args.only_strata.split(","))
        items = [i for i in items if i["stratum"] in keep]
    if args.ids:
        want = set(args.ids.split(","))
        items = [i for i in items if i["id"] in want]
    if args.limit:  # take the first N of each stratum so a pilot covers all strata
        per = collections.defaultdict(int)
        items = [i for i in items if (per.__setitem__(i["stratum"], per[i["stratum"]] + 1) or per[i["stratum"]] <= args.limit)]
    catalog = collections.defaultdict(list)
    if args.catalog:
        for line in open(args.catalog, encoding="utf8"):
            p = line.rstrip("\n").split("\t")
            catalog[norm(p[1])].append({"status": p[2]})

    login = WeragClient.login(args.base_url, args.email, os.environ["WERAG_PASSWORD"])
    token = login.get("token") or (login.get("data") or {}).get("token")
    client = WeragClient(args.base_url, token, tenant_id=args.tenant, timeout=900)

    os.makedirs(os.path.join(HERE, "results"), exist_ok=True)
    path = args.resume or os.path.join(HERE, "results", f"{args.set}-{time.strftime('%Y%m%d-%H%M%S')}.jsonl")
    done = {}
    if os.path.exists(path):
        for l in open(path, encoding="utf8"):
            r = json.loads(l)
            if r.get("pass") is not None or (r.get("verdict") or {}).get("category") == "no_answer":
                done[r["id"]] = r
    todo = [i for i in items if i["id"] not in done]
    print(f"{len(items)} items, {len(done)} done, {len(todo)} to run → {path}", flush=True)
    lock = threading.Lock()

    def one(item):
        for attempt in range(2):
            try:
                res = run_nli(client, item, args.kb) if args.set == "nli" else run_trap(client, item, args.kb, catalog)
                if res["turn"]["error"] and attempt == 0:
                    time.sleep(20)
                    continue
                break
            except Exception as e:  # noqa: BLE001
                res = {"turn": None, "verdict": {"error": str(e)}, "pass": None}
                time.sleep(20)
        row = {"id": item["id"], "stratum": item["stratum"], **{k: v for k, v in item.items() if k in ("label", "question", "expired")}, **res}
        with lock:
            with open(path, "a", encoding="utf8") as f:
                f.write(json.dumps(row, ensure_ascii=False) + "\n")
            done[item["id"]] = row
            print(f"[{len(done)}/{len(items)}] {item['id']} {item['stratum']} pass={row['pass']} "
                  f"{(row.get('verdict') or {}).get('category')} {row['turn']['duration_s'] if row.get('turn') else ''}s", flush=True)

    with ThreadPoolExecutor(args.parallel) as ex:
        for fut in as_completed([ex.submit(one, i) for i in todo]):
            fut.result()
    print(summarize(list(done.values()), args.set))


if __name__ == "__main__":
    main()
