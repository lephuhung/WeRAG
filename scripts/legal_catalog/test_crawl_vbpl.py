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
from contextlib import redirect_stdout

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("crawl_vbpl", os.path.join(HERE, "crawl_vbpl.py"))
crawl = importlib.util.module_from_spec(spec)
spec.loader.exec_module(crawl)

TEXT = "<p>" + "Điều 1. Nội dung. " * 20 + "</p>"


class FakeClient:
    """Answers the detail of each id from docs: {id: (status, content)}."""

    def __init__(self, docs):
        self.docs = docs

    def fetch(self, url, headers=None, **_):
        doc_id = url.rsplit("/", 1)[1]
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
