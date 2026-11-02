#!/usr/bin/env python3
"""Đo Gemma 4 trích quan hệ sửa đổi điều khoản từ văn bản VBPL.

  sample  lấy các đoạn có câu dẫn sửa đổi (regex) từ Postgres ra JSONL
  run     gửi từng đoạn cho Gemma (JSON schema), ghi kết quả
  score   so với bộ đọc vbpl (chunks.metadata.legal.amends) và in bảng

  python amend_eval.py sample --psql "psql -h 172.20.0.15 -U postgres -d WeKnora" --n 200 --out data/sample.jsonl
  python amend_eval.py run --in data/sample.jsonl --out results/gemma.jsonl
  python amend_eval.py score --in results/gemma.jsonl
"""
import argparse
import concurrent.futures as cf
import json
import os
import re
import subprocess
import sys
import time

import requests

# Câu dẫn có hiệu lực pháp lý (không phải câu nhắc tên "Luật … sửa đổi, bổ sung
# năm 2020" trong phần căn cứ): đầu dòng, sau số thứ tự, hoặc "được sửa đổi … như sau".
OPERATIVE_SQL = r"""(content ~* '(^|\n)\s*(#+\s*)?(\d+[a-zđ]?\.|[a-zđ]\)|Điều\s+\d+[a-zđ]?\.)?\s*(Sửa đổi|Bổ sung|Bãi bỏ|Thay thế|Thay cụm|Thay từ|Bỏ cụm|Bỏ từ|Đình chỉ|Hủy bỏ|Huỷ bỏ)'
 or content ~* '(được|đã được)\s+(sửa đổi|bổ sung|thay thế)[^.\n]{0,40}như sau')"""

SAMPLE_SQL = """
with c as (
  select c.id, c.content, c.metadata->'legal' legal, k.title, k.metadata->>'doc_number' doc_number,
         k.metadata->>'doc_type' doc_type, md5(c.content) h,
         jsonb_exists(c.metadata->'legal','amends') has_amends,
         c.content ~* 'bãi bỏ\\s+(các\\s+|một số\\s+)?(điểm|khoản|điều|chương|mục)\\s' is_repeal,
         c.content ~* 'bổ sung\\s+(các\\s+)?(điểm|khoản|điều)\\s+\\S+\\s*(vào\\s+)?(sau|trước)' is_insert
  from chunks c join knowledges k on k.id = c.knowledge_id
  where c.deleted_at is null and k.deleted_at is null and c.metadata->'legal'->>'source' = 'vbpl'
    and length(c.content) < 9000 and {op}
), d as (select distinct on (h) * from c order by h, id)
select json_build_object('id', id, 'content', content, 'legal', legal, 'title', title,
  'doc_number', doc_number, 'doc_type', doc_type, 'stratum', stratum)
from (
  (select *, 'phrase' stratum from d where has_amends and legal->'amends' @> '[{{"phrase": true}}]' order by md5(id || 'p') limit {np})
  union all (select *, 'quoted' from d where has_amends and not legal->'amends' @> '[{{"phrase": true}}]' order by md5(id || 'q') limit {nq})
  union all (select *, 'repeal' from d where not has_amends and is_repeal order by md5(id || 'r') limit {nr})
  union all (select *, 'insert' from d where not has_amends and is_insert order by md5(id || 'i') limit {ni})
  union all (select *, 'other' from d where not has_amends and not is_repeal and not is_insert order by md5(id || 'o') limit {no})
) s
"""

OPS = ["sua_doi", "bo_sung", "thay_cum_tu", "bai_bo", "bai_bo_cum_tu", "thay_the_van_ban",
       "bai_bo_van_ban", "dinh_chi", "khac"]

SCHEMA = {
    "type": "object",
    "properties": {
        "operations": {
            "type": "array",
            "items": {
                "type": "object",
                "properties": {
                    "op": {"type": "string", "enum": OPS},
                    "target_doc": {"type": "string"},
                    "targets": {
                        "type": "array",
                        "items": {
                            "type": "object",
                            "properties": {
                                "dieu": {"type": "string"},
                                "khoan": {"type": "string"},
                                "diem": {"type": "string"},
                            },
                            "required": ["dieu", "khoan", "diem"],
                        },
                    },
                    "from": {"type": "string"},
                    "to": {"type": "string"},
                    "new_label": {"type": "string"},
                    "after": {"type": "string"},
                },
                "required": ["op", "target_doc", "targets", "from", "to", "new_label", "after"],
            },
        }
    },
    "required": ["operations"],
}

SCHEMA_COMPACT = {
    "type": "object",
    "properties": {
        "ops": {
            "type": "array",
            "items": {
                "type": "object",
                "properties": {
                    "op": {"type": "string", "enum": OPS},
                    "doc": {"type": "string"},
                    "at": {"type": "array", "items": {"type": "string"}},
                    "from": {"type": "string"},
                    "to": {"type": "string"},
                    "new": {"type": "string"},
                    "after": {"type": "string"},
                },
                "required": ["op", "doc", "at"],
            },
        }
    },
    "required": ["ops"],
}

COMPACT_NOTE = """

ĐỊNH DẠNG GỌN: trả về {"ops": [...]}; mỗi phần tử có op, doc (= target_doc), at (= targets, mỗi điều khoản viết "Điều/khoản/điểm": "17/3/a", "17/3", "17"; phụ lục giữ nguyên chữ "Phụ lục I"); chỉ thêm from, to, new (= new_label), after khi có giá trị. Gộp các điều khoản cùng một thao tác và cùng cụm từ vào một phần tử."""

SYSTEM = """Bạn đọc một đoạn của văn bản quy phạm pháp luật Việt Nam (văn bản B) và liệt kê các THAO TÁC mà đoạn đó thực hiện lên điều khoản của văn bản khác (văn bản A).

Chỉ liệt kê thao tác có hiệu lực do chính đoạn này quy định ("Sửa đổi khoản 2 Điều 5 như sau", "Bãi bỏ điểm c khoản 1 Điều 7", "Thay cụm từ “X” bằng cụm từ “Y” tại Điều 3"). KHÔNG liệt kê: tên văn bản được nhắc ở phần căn cứ ("Luật … được sửa đổi, bổ sung năm 2020"), lời văn mới nằm trong ngoặc kép (đó là nội dung, không phải thao tác), quy định tự sửa chính văn bản B.

op:
- sua_doi: sửa đổi, hoặc "sửa đổi, bổ sung" một Điều/khoản/điểm đã có, kèm lời văn mới
- bo_sung: thêm Điều/khoản/điểm MỚI (ví dụ "bổ sung khoản 4a vào sau khoản 4", "bổ sung Điều 5a"); new_label = nhãn mới ("4a"), after = nhãn đứng trước nếu có
- thay_cum_tu: thay cụm từ from bằng to tại các điều khoản targets
- bai_bo_cum_tu: bỏ cụm từ from tại targets
- bai_bo: bãi bỏ cả Điều/khoản/điểm
- thay_the_van_ban / bai_bo_van_ban / dinh_chi: tác động cả văn bản A (targets rỗng)
- khac: thao tác khác lên văn bản A

target_doc: tên văn bản A đúng như văn bản viết, gồm số hiệu nếu có ("Nghị định số 68/2026/NĐ-CP", "Luật Giáo dục"). Nếu đoạn không nêu tên, lấy từ tiêu đề Điều/Mục cha được cho trong NGỮ CẢNH. Không tự đoán số hiệu.
targets: mỗi điều khoản là một phần tử; dieu/khoan/diem chỉ ghi số hoặc chữ ("5", "2", "a"), để "" khi không có. Viết gọn danh sách ("các khoản 1, 2 và 3 Điều 4") thành từng phần tử.
from/to/new_label/after: "" khi không dùng.
Không có thao tác nào thì trả về {"operations": []}."""


def psql_rows(psql, sql):
    out = subprocess.run(psql.split() + ["-At", "-c", sql], capture_output=True, text=True, check=True).stdout
    return [json.loads(line) for line in out.splitlines() if line.strip()]


HOLDOUT_SQL = """
with c as (
  select c.id, c.content, c.metadata->'legal' legal, k.title, k.metadata->>'doc_number' doc_number,
         k.metadata->>'doc_type' doc_type, md5(c.content) h,
         coalesce(c.metadata->'legal'->>'source', 'other') src
  from chunks c join knowledges k on k.id = c.knowledge_id
  where c.deleted_at is null and k.deleted_at is null and k.metadata->>'doc_number' is not null
    and {op} and not (c.id = any(%(exclude)s))
), d as (select distinct on (h) * from c order by h, id)
select json_build_object('id', id, 'content', content, 'legal', legal, 'title', title,
  'doc_number', doc_number, 'doc_type', doc_type, 'stratum', 'holdout-' || src,
  'chars', length(content))
from d order by md5(id || '{seed}') limit {n}
"""


def cmd_holdout(a):
    exclude = [json.loads(l)["id"] for l in open(a.exclude)] if a.exclude else []
    arr = "ARRAY[" + ",".join("'" + i.replace("'", "") + "'" for i in exclude) + "]::varchar[]" if exclude else "ARRAY[]::varchar[]"
    sql = HOLDOUT_SQL.replace("%(exclude)s", arr).format(op=OPERATIVE_SQL, seed=a.seed, n=a.n)
    rows = psql_rows(a.psql, sql)
    with open(a.out, "w") as f:
        for r in rows:
            f.write(json.dumps(r, ensure_ascii=False) + "\n")
    long = sum(r["chars"] >= 9000 for r in rows)
    by = {}
    for r in rows:
        by[r["stratum"]] = by.get(r["stratum"], 0) + 1
    print(len(rows), by, "long>=9000:", long)


def cmd_sample(a):
    n = a.n
    sql = SAMPLE_SQL.format(op=OPERATIVE_SQL, np=n * 25 // 100, nq=n * 25 // 100, nr=n * 20 // 100,
                            ni=n * 15 // 100, no=n * 15 // 100)
    rows = psql_rows(a.psql, sql)
    os.makedirs(os.path.dirname(a.out) or ".", exist_ok=True)
    with open(a.out, "w") as f:
        for r in rows:
            f.write(json.dumps(r, ensure_ascii=False) + "\n")
    by = {}
    for r in rows:
        by[r["stratum"]] = by.get(r["stratum"], 0) + 1
    print(len(rows), by)


QUOTE_KEEP = 100
AS_FOLLOWS_RE = re.compile(r"(như sau|sau đây)\s*:", re.I)


def condense(text):
    """Giữ nguyên các câu dẫn; lời văn mới trích trong ngoặc (khối "> …" hoặc
    phần sau "như sau:") chỉ giữ đầu câu, vì thao tác nằm ở câu dẫn."""
    out, in_quote = [], False
    for line in text.splitlines():
        st = line.lstrip()
        if st.startswith(">"):
            if not in_quote:
                body = st.lstrip("> ").strip()
                out.append("> " + (body[:QUOTE_KEEP] + " …" if len(body) > QUOTE_KEEP else body))
            in_quote = True
            continue
        if st:
            in_quote = False
        m = AS_FOLLOWS_RE.search(line)
        if m and len(line) - m.end() > QUOTE_KEEP:
            line = line[:m.end() + QUOTE_KEEP] + " …"
        out.append(line)
    return re.sub(r"\n{3,}", "\n\n", "\n".join(out))


CONDENSE = False


def user_prompt(r):
    heading = " > ".join((r.get("legal") or {}).get("heading_path") or [])
    body = condense(r["content"]) if CONDENSE else r["content"]
    return (f"NGỮ CẢNH\nVăn bản B: {r.get('doc_number') or ''} — {r.get('title') or ''}\n"
            f"Tiêu đề cha của đoạn: {heading or '(không có)'}\n\nĐOẠN\n{body}")


def expand_compact(ops):
    out = []
    for o in ops:
        ts = []
        for t in o.get("at") or []:
            parts = (t.split("/") + ["", "", ""])[:3]
            ts.append({"dieu": parts[0].strip(), "khoan": parts[1].strip(), "diem": parts[2].strip()})
        out.append({"op": o.get("op"), "target_doc": o.get("doc", ""), "targets": ts, "from": o.get("from", ""),
                    "to": o.get("to", ""), "new_label": o.get("new", ""), "after": o.get("after", "")})
    return out


class ServerDown(Exception):
    """vLLM trả 5xx hoặc từ chối kết nối: dừng cả lượt chạy (ngắt mạch)."""


JSON_BLOCK_RE = re.compile(r"\{.*\}", re.S)


def parse_json_reply(text):
    t = re.sub(r"^```(?:json)?\s*|\s*```$", "", text.strip())
    try:
        return json.loads(t)
    except json.JSONDecodeError:
        m = JSON_BLOCK_RE.search(t)
        if not m:
            raise
        return json.loads(m.group(0))


def call(a, r):
    system, schema = (SYSTEM + COMPACT_NOTE, SCHEMA_COMPACT) if a.compact else (SYSTEM, SCHEMA)
    body = {
        "model": a.model,
        "messages": [{"role": "system", "content": system}, {"role": "user", "content": user_prompt(r)}],
        "temperature": 0,
        # Qwen3: tắt thinking (Gemma bỏ qua khóa này)
        "chat_template_kwargs": {"enable_thinking": False},
        "max_tokens": a.max_tokens,
    }
    if a.no_schema:
        # Không dùng structured outputs phía server: vLLM 0.31 + Qwen3.6 sập CUDA
        # (illegal memory access) khi giải mã song song các yêu cầu json_schema.
        body["messages"][0]["content"] += ("\n\nTrả về DUY NHẤT một đối tượng JSON, không kèm lời giải thích, "
                                          "đúng schema sau:\n" + json.dumps(schema, ensure_ascii=False))
    else:
        body["response_format"] = {"type": "json_schema", "json_schema": {"name": "amend_ops", "schema": schema}}
    t = time.time()
    err = ""
    for attempt in range(2):
        try:
            resp = requests.post(a.base_url.rstrip("/") + "/chat/completions", json=body, timeout=300)
        except requests.ConnectionError as e:
            raise ServerDown(str(e))
        if resp.status_code >= 500:
            raise ServerDown(f"HTTP {resp.status_code}: {resp.text[:200]}")
        try:
            resp.raise_for_status()
            j = resp.json()
            choice = j["choices"][0]
            parsed = parse_json_reply(choice["message"]["content"] or "")
            ops = expand_compact(parsed["ops"]) if a.compact else parsed["operations"]
            return {"ops": ops, "secs": round(time.time() - t, 2), "usage": j.get("usage"),
                    "finish": choice.get("finish_reason")}
        except Exception as e:  # noqa: BLE001
            err = f"{type(e).__name__}: {e}"
            if "length" in str(locals().get("choice", {}).get("finish_reason", "")):
                break  # hết token: thử lại cũng vậy
            time.sleep(2)
    return {"error": err, "secs": round(time.time() - t, 2)}


def cmd_run(a):
    global CONDENSE
    CONDENSE = a.condense
    rows = [json.loads(l) for l in open(a.inp)]
    if a.limit:
        rows = rows[:a.limit]
    kept, done = [], set()
    if os.path.exists(a.out):
        for l in open(a.out):
            x = json.loads(l)
            if a.retry_failed and "error" in (x.get("gemma") or {}):
                continue
            kept.append(l)
            done.add(x["id"])
        if a.retry_failed:
            with open(a.out, "w") as f:
                f.writelines(kept)
    todo = [r for r in rows if r["id"] not in done]
    os.makedirs(os.path.dirname(a.out) or ".", exist_ok=True)
    print(f"todo={len(todo)} workers={a.workers} no_schema={a.no_schema}", file=sys.stderr)
    with open(a.out, "a") as f, cf.ThreadPoolExecutor(a.workers) as ex:
        futs = {ex.submit(call, a, r): r for r in todo}
        try:
            for i, fu in enumerate(cf.as_completed(futs), 1):
                r = futs[fu]
                res = fu.result()
                f.write(json.dumps({**r, "gemma": res}, ensure_ascii=False) + "\n")
                f.flush()
                if i % 10 == 0:
                    print(i, "/", len(todo), file=sys.stderr)
        except ServerDown as e:
            for fu in futs:
                fu.cancel()
            print(f"SERVER DOWN — dừng: {e}", file=sys.stderr)
            sys.exit(3)


def norm(s):
    return re.sub(r"\s+", " ", (s or "").strip().lower())


def parser_targets(r):
    out = set()
    for x in (r.get("legal") or {}).get("amends") or []:
        out.add((x.get("article", ""), x.get("khoan", ""), x.get("diem", "")))
    return out


def gemma_targets(ops):
    out = set()
    for o in ops:
        for t in o.get("targets") or []:
            out.add((t.get("dieu", "").strip(), t.get("khoan", "").strip(), t.get("diem", "").strip()))
        if o.get("op") == "bo_sung" and o.get("new_label"):
            # bổ sung: bộ đọc ghi nhãn mới làm đích
            for t in o.get("targets") or []:
                pass
    return out


def cmd_score(a):
    rows = [json.loads(l) for l in open(a.inp)]
    st = {}
    secs = []
    for r in rows:
        s = st.setdefault(r["stratum"], {"n": 0, "err": 0, "empty": 0, "tp": 0, "fp": 0, "fn": 0,
                                         "phrase_ok": 0, "phrase_n": 0, "ops": {}})
        s["n"] += 1
        g = r.get("gemma") or {}
        if "error" in g:
            s["err"] += 1
            continue
        secs.append(g["secs"])
        ops = g["ops"]
        if not ops:
            s["empty"] += 1
        for o in ops:
            s["ops"][o["op"]] = s["ops"].get(o["op"], 0) + 1
        if r["stratum"] in ("phrase", "quoted"):
            p, q = parser_targets(r), gemma_targets(ops)
            # bộ đọc lưu đích ở cấp Điều khi sửa cả Điều; so khớp lỏng theo Điều
            p_d = {x[0] for x in p}
            q_d = {x[0] for x in q} | {o.get("new_label") for o in ops if o.get("op") == "bo_sung" and not o.get("targets")}
            s["tp"] += len(p_d & q_d)
            s["fp"] += len(q_d - p_d)
            s["fn"] += len(p_d - q_d)
        if r["stratum"] == "phrase":
            want = {(norm(x.get("from")), norm(x.get("to"))) for x in r["legal"]["amends"] if x.get("phrase")}
            got = {(norm(o.get("from")), norm(o.get("to"))) for o in ops if o["op"] in ("thay_cum_tu", "bai_bo_cum_tu")}
            s["phrase_n"] += len(want)
            s["phrase_ok"] += len(want & got)
    for k, s in st.items():
        line = f"{k:7} n={s['n']:3} err={s['err']} empty={s['empty']:3} ops={s['ops']}"
        if s["tp"] + s["fn"]:
            prec = s["tp"] / max(1, s["tp"] + s["fp"])
            rec = s["tp"] / max(1, s["tp"] + s["fn"])
            line += f" Điều P={prec:.2f} R={rec:.2f}"
        if s["phrase_n"]:
            line += f" phrase {s['phrase_ok']}/{s['phrase_n']}"
        print(line)
    if secs:
        secs.sort()
        print(f"secs median={secs[len(secs)//2]} p90={secs[int(len(secs)*.9)]}")


NUM_RE = re.compile(r"\d+[a-z]?/\d{4}/[A-Za-zĐđ0-9\-]+|\d+/[A-Za-zĐđ]+[A-Za-zĐđ0-9\-]*")


def num_key(doc):
    """số hiệu chuẩn hoá ("81-2018-NĐ-CP" → "81/2018/ND-CP"); "" khi không có."""
    d = (doc or "").replace("–", "-")
    d = re.sub(r"(\d+)-(\d{4})-", r"\1/\2/", d)
    m = NUM_RE.search(d)
    return m.group(0).upper().replace("Đ", "D").replace("đ", "D") if m else ""


def tgt(t):
    t = t.lower().replace(" ", "")
    for w in ("điều", "khoản", "điểm"):
        t = t.replace(w, "")
    return "/".join(p for p in t.split("/") if p) if t else ""


ANNEX_RE = re.compile(r"phụ ?lục|mẫu|biểu|mục|chương|phần|cột|bảng|\?", re.I)


def split_annex(pairs):
    core = {p for p in pairs if not ANNEX_RE.search(p[1])}
    return core, pairs - core


def gold_pairs(ops):
    """(op, đích) cho mỗi thao tác; thao tác cả văn bản dùng đích "*"."""
    out = set()
    for o in ops:
        op = o.get("op")
        ats = o.get("at") or []
        if op == "bo_sung":
            out.add((op, "+".join([tgt(ats[0]) if ats else ""] + [tgt(o.get("new") or "")])))
        elif not ats:
            out.add((op, "*"))
        for a in ats if op != "bo_sung" else []:
            out.add((op, tgt(a)))
    return out


def strict_items(ops):
    """Multiset các (op, đích, số hiệu A, from, to) — đúng cả văn bản và cụm từ
    mới tính; bổ sung kèm cả nhãn đứng trước. Không tính đích phụ lục/mẫu biểu."""
    from collections import Counter
    out = Counter()
    for o in ops:
        op, doc = o.get("op"), num_key(o.get("doc"))
        ph = (norm(o.get("from")), norm(o.get("to"))) if op in ("thay_cum_tu", "bai_bo_cum_tu", "khac") else ("", "")
        if op == "bo_sung":
            ats = o.get("at") or []
            t = (tgt(ats[0]) if ats else "") + "+" + tgt(o.get("new") or "") + "@" + tgt(o.get("after") or "")
            if not ANNEX_RE.search(t):
                out[(op, t, doc) + ph] += 1
            continue
        ats = o.get("at") or []
        if not ats:
            out[(op, "*", doc) + ph] += 1
        for x in ats:
            if not ANNEX_RE.search(tgt(x)):
                out[(op, tgt(x), doc) + ph] += 1
    return out


def model_as_gold(ops):
    return [{"op": o.get("op"), "doc": o.get("target_doc", ""),
             "at": ["/".join(x for x in (t.get("dieu", ""), t.get("khoan", ""), t.get("diem", "")) if x)
                    for t in o.get("targets") or []],
             "from": o.get("from", ""), "to": o.get("to", ""), "new": o.get("new_label", ""), "after": o.get("after", "")}
            for o in ops]


def parser_as_gold(r):
    out = []
    for x in (r.get("legal") or {}).get("amends") or []:
        at = "/".join(v for v in (x.get("article", ""), x.get("khoan", ""), x.get("diem", "")) if v)
        op = ("bai_bo_cum_tu" if not x.get("to") else "thay_cum_tu") if x.get("phrase") else "sua_doi"
        out.append({"op": op, "doc": x.get("document", ""), "at": [at], "from": x.get("from", ""), "to": x.get("to", "")})
    return out


def loosen(pairs):
    # bỏ phân biệt sửa đổi/bổ sung và cụm từ thay/bỏ: chỉ hỏi "đúng đích chưa"
    m = {"bo_sung": "sua_doi", "bai_bo_cum_tu": "thay_cum_tu"}
    return {(m.get(op, op), t.split("+")[0] if "+" in t else t) for op, t in pairs}


def cmd_score_gold(a):
    gold = {}
    for path in a.gold:
        for l in open(path):
            g = json.loads(l)
            gold[g["id"]] = g["ops"]
    systems = {}
    sample = {}
    for spec in a.systems:
        name, path = spec.split("=", 1)
        for l in open(path):
            r = json.loads(l)
            sample[r["id"]] = r
            g = r.get("gemma") or {}
            systems.setdefault(name, {})[r["id"]] = None if "error" in g else model_as_gold(g["ops"])
    systems["parser"] = {i: parser_as_gold(r) for i, r in sample.items()}
    print(f"gold passages={len(gold)} ops={sum(len(v) for v in gold.values())}")
    hdr = (f"{'system':14} {'fail':>4} {'P':>5} {'R':>5} {'F1':>5} | {'P~':>5} {'R~':>5} | "
           f"{'sP':>5} {'sR':>5} | {'doc':>5} {'phrase':>7} {'emptyOK':>7}")
    strata = [None] + sorted({r["stratum"] for r in sample.values()})
    for strat in strata:
        print(f"\n== {strat or 'ALL'}")
        print(hdr)
        for name, res in systems.items():
            tp = fp = fn = ltp = lfp = lfn = fail = 0
            stp = sfp = sfn = 0
            doc_ok = doc_n = ph_ok = ph_n = em_ok = em_n = 0
            for i, gops in gold.items():
                if i not in sample or (strat and sample[i]["stratum"] != strat):
                    continue
                if a.only and systems[a.only].get(i) is None:
                    continue
                sops = res.get(i)
                if sops is None:
                    fail += 1
                    sops = []
                gp, sp = gold_pairs(gops), gold_pairs(sops)
                if a.annex:
                    gp, sp = split_annex(gp)[1], split_annex(sp)[1]
                else:
                    gp, sp = split_annex(gp)[0], split_annex(sp)[0]
                tp += len(gp & sp); fp += len(sp - gp); fn += len(gp - sp)
                sg, ss = strict_items(gops), strict_items(sops)
                stp += sum((sg & ss).values()); sfp += sum((ss - sg).values()); sfn += sum((sg - ss).values())
                lg, ls = loosen(gp), loosen(sp)
                ltp += len(lg & ls); lfp += len(ls - lg); lfn += len(lg - ls)
                gd = {num_key(o.get("doc")) for o in gops if num_key(o.get("doc"))}
                if gd:
                    doc_n += 1
                    doc_ok += bool(gd & {num_key(o.get("doc")) for o in sops})
                gph = {(norm(o.get("from")), norm(o.get("to"))) for o in gops if o["op"] in ("thay_cum_tu", "bai_bo_cum_tu")}
                sph = {(norm(o.get("from")), norm(o.get("to"))) for o in sops if o.get("op") in ("thay_cum_tu", "bai_bo_cum_tu")}
                ph_n += len(gph); ph_ok += len(gph & sph)
                if not gops:
                    em_n += 1
                    em_ok += not sops
            P = tp / max(1, tp + fp); R = tp / max(1, tp + fn); F = 2 * P * R / max(1e-9, P + R)
            lP = ltp / max(1, ltp + lfp); lR = ltp / max(1, ltp + lfn)
            sP = stp / max(1, stp + sfp); sR = stp / max(1, stp + sfn)
            print(f"{name:14} {fail:4} {P:5.2f} {R:5.2f} {F:5.2f} | {lP:5.2f} {lR:5.2f} | {sP:5.2f} {sR:5.2f} | "
                  f"{doc_ok:>2}/{doc_n:<2} {ph_ok:>3}/{ph_n:<3} {em_ok:>3}/{em_n:<3}")


def main():
    ap = argparse.ArgumentParser()
    sub = ap.add_subparsers(dest="cmd", required=True)
    s = sub.add_parser("sample")
    s.add_argument("--psql", required=True)
    s.add_argument("--n", type=int, default=200)
    s.add_argument("--out", required=True)
    r = sub.add_parser("run")
    r.add_argument("--in", dest="inp", required=True)
    r.add_argument("--out", required=True)
    r.add_argument("--base-url", default="http://10.10.0.240:8001/v1")
    r.add_argument("--model", default="gemma-4-e4b-it")
    r.add_argument("--workers", type=int, default=4)
    r.add_argument("--compact", action="store_true")
    r.add_argument("--max-tokens", type=int, default=4096)
    r.add_argument("--condense", action="store_true")
    r.add_argument("--no-schema", action="store_true", help="schema trong prompt, không dùng structured outputs")
    r.add_argument("--retry-failed", action="store_true")
    r.add_argument("--limit", type=int, default=0)
    c = sub.add_parser("score")
    c.add_argument("--in", dest="inp", required=True)
    h = sub.add_parser("holdout")
    h.add_argument("--psql", required=True)
    h.add_argument("--n", type=int, default=150)
    h.add_argument("--seed", default="holdout-2026-10-19")
    h.add_argument("--exclude", default="")
    h.add_argument("--out", required=True)
    g = sub.add_parser("score-gold")
    g.add_argument("--gold", nargs="+", required=True)
    g.add_argument("--systems", nargs="+", required=True, help="tên=tệp kết quả run")
    g.add_argument("--annex", action="store_true", help="chỉ chấm đích phụ lục/mẫu/biểu (mặc định: bỏ chúng)")
    g.add_argument("--only", default="", help="chỉ các id có kết quả hợp lệ của hệ thống này")
    a = ap.parse_args()
    {"sample": cmd_sample, "holdout": cmd_holdout, "run": cmd_run, "score": cmd_score,
     "score-gold": cmd_score_gold}[a.cmd](a)


if __name__ == "__main__":
    main()
