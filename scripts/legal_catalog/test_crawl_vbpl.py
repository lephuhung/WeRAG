"""Tests of crawl_vbpl.crawl_documents' full-text removal rules (no network):

    python3 -I scripts/legal_catalog/test_crawl_vbpl.py
"""
import argparse
import importlib.util
import io
import json
import os
import sqlite3
import sys
import tempfile
import unittest
import urllib.error
from contextlib import redirect_stdout

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("crawl_vbpl", os.path.join(HERE, "crawl_vbpl.py"))
crawl = importlib.util.module_from_spec(spec)
spec.loader.exec_module(crawl)

TEXT = "<p>" + "Điều 1. Nội dung. " * 20 + "</p>"


class FakeClient:
    """Answers the detail of each id from docs: {id: (status, content)};
    an id whose value is an exception raises it."""

    def __init__(self, docs):
        self.docs = docs

    def fetch(self, url, headers=None, **_):
        doc_id = url.rsplit("/", 1)[1]
        if isinstance(self.docs[doc_id], Exception):
            raise self.docs[doc_id]
        status, content = self.docs[doc_id]
        return json.dumps({"success": True, "data": {
            "id": doc_id, "title": doc_id, "effStatus": {"name": status},
            "documentContent": {"content": content}}})

    def action(self, action_id, args, **_):
        return "diagram"


class CrawlRemovalTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.mkdtemp()
        self.html = os.path.join(self.dir, "html")
        os.makedirs(self.html)
        self.db = sqlite3.connect(os.path.join(self.dir, "state.sqlite"))
        self.db.executescript(crawl.SCHEMA)
        self.orig_rsc = crawl.rsc_object
        crawl.rsc_object = lambda text, key: {}

    def tearDown(self):
        crawl.rsc_object = self.orig_rsc

    def args(self, **kw):
        a = argparse.Namespace(out_dir=self.dir, refetch_all=False, limit=0,
                               max_expired_removals=None, allow_mass_expiry=False)
        for k, v in kw.items():
            setattr(a, k, v)
        return a

    def held(self, *ids):
        """Documents with a stored text, listed as changed since their fetch."""
        with self.db:
            for doc_id in ids:
                self.db.execute("INSERT INTO docs (id, list_sig, list_item, kept, fetched_sig) VALUES (?, 'new', '{}', 1, 'old')",
                                (doc_id,))
                with open(os.path.join(self.html, f"{doc_id}.html"), "w", encoding="utf-8") as f:
                    f.write(TEXT)

    def has_html(self, doc_id):
        return os.path.exists(os.path.join(self.html, f"{doc_id}.html"))

    def sig(self, doc_id):
        return self.db.execute("SELECT fetched_sig FROM docs WHERE id=?", (doc_id,)).fetchone()[0]

    def run_crawl(self, docs, **kw):
        with redirect_stdout(io.StringIO()) as out:
            res = crawl.crawl_documents(FakeClient(docs), self.db, "act", self.args(**kw))
        return res, out.getvalue()

    def test_empty_text_of_a_document_in_force_keeps_the_stored_one(self):
        self.held("a", "b")
        res, log = self.run_crawl({"a": ("Còn hiệu lực", ""), "b": ("Còn hiệu lực", TEXT + "x")})
        self.assertTrue(self.has_html("a"), "the stored text is kept")
        self.assertEqual(res["empty_content_kept"], 1)
        self.assertIn("a: empty full text, the stored one is kept", log)
        self.assertIsNone(self.sig("a"), "fetched again next run")
        self.assertEqual(self.sig("b"), "new")
        self.assertEqual(res["expired_html_removed"], 0)

    def test_a_fully_expired_document_loses_its_text(self):
        self.held("a", "b")
        res, _ = self.run_crawl({"a": (crawl.EXPIRED_STATUS, TEXT), "b": ("Hết hiệu lực một phần", "")})
        self.assertFalse(self.has_html("a"))
        self.assertEqual(self.sig("a"), "new")
        self.assertTrue(self.has_html("b"), "partial expiry keeps the text")
        self.assertEqual(res["expired_html_removed"], 1)
        self.assertNotIn("mass_expiry_held", res)

    def test_mass_expiry_removes_nothing(self):
        self.held("a", "b", "c")
        docs = {i: (crawl.EXPIRED_STATUS, "") for i in ("a", "b", "c")}
        res, log = self.run_crawl(docs, max_expired_removals=2)
        self.assertEqual(res["mass_expiry_held"], 3)
        for i in ("a", "b", "c"):
            self.assertTrue(self.has_html(i), i)
            self.assertEqual(self.sig(i), "old", "the old row stays; fetched again next run")
        self.assertIn("HELD: 3 documents", log)
        # the override lets them through
        res, _ = self.run_crawl(docs, max_expired_removals=2, allow_mass_expiry=True)
        self.assertEqual(res["expired_html_removed"], 3)
        self.assertFalse(any(self.has_html(i) for i in ("a", "b", "c")))

    def test_default_limit(self):
        self.assertEqual(crawl.mass_expiry_limit(self.args(), self.html), 200)
        for i in range(12000):
            open(os.path.join(self.html, f"{i}.html"), "w").close()
        self.assertEqual(crawl.mass_expiry_limit(self.args(), self.html), 240)
        self.assertEqual(crawl.mass_expiry_limit(self.args(max_expired_removals=5), self.html), 5)


OLD_SCHEMA = """
CREATE TABLE docs (id TEXT PRIMARY KEY, list_sig TEXT NOT NULL, list_item TEXT NOT NULL, kept INTEGER NOT NULL,
    fetched_sig TEXT, detail TEXT, diagram TEXT, fetched_at TEXT, error TEXT);
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT);
INSERT INTO docs (id, list_sig, list_item, kept) VALUES ('a', 's', '{}', 1);
"""


def list_item(doc_id):
    return json.dumps({"id": doc_id, "title": doc_id, "docNum": f"{doc_id}/2026/NĐ-CP",
                       "docType": {"name": "Nghị định"}, "effStatus": {"name": "Còn hiệu lực"}})


class FetchFailedTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.mkdtemp()
        os.makedirs(os.path.join(self.dir, "html"))
        self.db = crawl.open_state(sqlite3.connect(os.path.join(self.dir, "state.sqlite")))
        self.orig_rsc = crawl.rsc_object
        crawl.rsc_object = lambda text, key: {}

    def tearDown(self):
        crawl.rsc_object = self.orig_rsc

    def args(self, **kw):
        a = argparse.Namespace(out_dir=self.dir, refetch_all=False, limit=0, max_expired_removals=None,
                               allow_mass_expiry=False, all_central=True, min_year=2010, no_stubs=False,
                               province=[])
        for k, v in kw.items():
            setattr(a, k, v)
        return a

    def add(self, doc_id, kept=1, detail=None):
        with self.db:
            self.db.execute("INSERT INTO docs (id, list_sig, list_item, kept, detail, diagram) VALUES (?, 's', ?, ?, ?, '{}')",
                            (doc_id, list_item(doc_id), kept, detail))

    def runs(self, doc_id):
        return self.db.execute("SELECT fail_runs FROM docs WHERE id=?", (doc_id,)).fetchone()[0]

    def crawl(self, docs):
        with redirect_stdout(io.StringIO()):
            return crawl.crawl_documents(FakeClient(docs), self.db, "act", self.args())

    def export(self, **kw):
        with redirect_stdout(io.StringIO()):
            res = crawl.export(self.db, self.args(**kw))
        with open(os.path.join(self.dir, "documents.jsonl"), encoding="utf-8") as f:
            rows = {r["id"]: r for r in map(json.loads, f)}
        return res, rows

    def test_an_old_state_file_upgrades_in_place(self):
        path = os.path.join(self.dir, "old.sqlite")
        old = sqlite3.connect(path)
        old.executescript(OLD_SCHEMA)
        old.close()
        db = crawl.open_state(sqlite3.connect(path))
        self.assertEqual(db.execute("SELECT id, fail_runs FROM docs").fetchall(), [("a", 0)])
        crawl.open_state(db)  # idempotent
        cols = [r[1] for r in db.execute("PRAGMA table_info(docs)")]
        self.assertEqual(cols.count("fail_runs"), 1)

    def test_fail_runs_count_up_and_reset_on_a_fetch(self):
        self.add("a")
        broken = {"a": urllib.error.HTTPError("u", 500, "boom", {}, None)}
        for n in (1, 2, 3):
            res = self.crawl(broken)
            self.assertEqual(res["fetch_failed"], 1)
            self.assertEqual(self.runs("a"), n)
        self.assertIn("500", self.db.execute("SELECT error FROM docs WHERE id='a'").fetchone()[0])
        self.crawl({"a": ("Còn hiệu lực", TEXT)})
        self.assertEqual(self.runs("a"), 0)
        self.assertIsNone(self.db.execute("SELECT error FROM docs WHERE id='a'").fetchone()[0])

    def test_the_deferred_expiry_resets_fail_runs(self):
        self.add("a", detail="{}")
        with open(os.path.join(self.dir, "html", "a.html"), "w", encoding="utf-8") as f:
            f.write(TEXT)
        with self.db:
            self.db.execute("UPDATE docs SET fail_runs=4 WHERE id='a'")
        res = self.crawl({"a": (crawl.EXPIRED_STATUS, "")})
        self.assertEqual(res["expired_html_removed"], 1)
        self.assertEqual(self.runs("a"), 0)

    def test_export_flags_a_kept_document_failing_two_runs_without_detail(self):
        detail = json.dumps({"id": "held", "title": "held"})
        self.add("once")       # one bad night
        self.add("twice")      # two: flagged
        self.add("held", detail=detail)  # fetched earlier: exported from its detail
        self.add("out", kept=0)          # outside the crawl scope
        with self.db:
            self.db.execute("UPDATE docs SET fail_runs=1 WHERE id='once'")
            self.db.execute("UPDATE docs SET fail_runs=2 WHERE id IN ('twice', 'held', 'out')")
        res, rows = self.export()
        self.assertTrue(rows["twice"]["fetch_failed"])
        self.assertTrue(rows["twice"]["metadata_only"])
        self.assertFalse(rows["twice"]["has_html"])
        for doc_id in ("once", "out"):
            self.assertNotIn("fetch_failed", rows[doc_id], doc_id)
            self.assertTrue(rows[doc_id]["metadata_only"], doc_id)
        self.assertNotIn("fetch_failed", rows["held"])
        self.assertNotIn("metadata_only", rows["held"])
        self.assertEqual(res["exported_fetch_failed"], 1)
        self.assertEqual(res["exported_metadata_only"], 3)
        # --no-stubs still exports the flagged kept document
        res, rows = self.export(no_stubs=True)
        self.assertEqual(sorted(rows), ["held", "twice"])
        self.assertEqual(res["exported_fetch_failed"], 1)


class MainExitTest(unittest.TestCase):
    def test_held_run_exits_3_after_writing_the_export(self):
        d = tempfile.mkdtemp()
        summary = os.path.join(d, "summary.json")
        orig = (crawl.crawl_documents, crawl.Client, crawl.discover_actions, crawl.crawl_list)
        crawl.crawl_documents = lambda *a: {"to_fetch": 3, "mass_expiry_held": 3}
        crawl.Client = lambda *a: argparse.Namespace(requests=0)
        crawl.discover_actions = lambda c: ("l", "d")
        crawl.crawl_list = lambda *a: None
        argv = sys.argv
        sys.argv = ["crawl_vbpl.py", d, "--summary", summary]
        try:
            with redirect_stdout(io.StringIO()):
                rc = crawl.main()
        finally:
            sys.argv = argv
            crawl.crawl_documents, crawl.Client, crawl.discover_actions, crawl.crawl_list = orig
        self.assertEqual(rc, crawl.EXIT_HELD)
        self.assertTrue(os.path.exists(os.path.join(d, "documents.jsonl")), "the export is written")
        with open(summary) as f:
            self.assertEqual(json.load(f)["mass_expiry_held"], 3)


if __name__ == "__main__":
    unittest.main()
