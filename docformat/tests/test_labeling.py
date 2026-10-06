"""LLM/agent component labeling: task building, applying labels, fallbacks,
and the OpenAI-compatible client against a local fake endpoint."""

import json
import os
import sys
import threading
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer

sys.path.insert(0, os.path.dirname(__file__))
sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from test_check import build_docx, doc, p  # noqa: E402

from docformat import check_document, labeling_task_for  # noqa: E402
from docformat.llm import LLMConfig  # noqa: E402


def flat_centered_doc(signer_size=14):
    """Every line centered and full-width: no table, no tabs, no indent —
    the positional heuristic cannot tell the signature block from body."""
    body = (
        p("SỞ Y TẾ", align="center", bold=True, size=13)
        + p("CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", align="center", bold=True,
            size=13)
        + p("Độc lập - Tự do - Hạnh phúc", align="center", bold=True, size=13)
        + p("Số: 12/TB-SYT", align="center", size=13)
        + p("Huế, ngày 05 tháng 10 năm 2026", align="center", italic=True)
        + p("THÔNG BÁO", align="center", bold=True)
        + p("Lịch tiếp công dân tháng 10/2026", align="center", bold=True)
        + p("Sở Y tế thông báo lịch tiếp công dân như sau: ...", align="both")
        + p("GIÁM ĐỐC", align="center", bold=True)
        + p("Phạm Văn Em", align="center", bold=True, size=signer_size)
        + p("Nơi nhận:", bold=True, italic=True, size=12)
        + p("- Lưu: VT.", size=11)
    )
    return build_docx(doc(body))


def labels_for(task, mapping, default="noi_dung", doc_type="thong_bao"):
    """Build a reply labelling units by exact text."""
    out = {}
    for u in task["units"]:
        out[u["id"]] = mapping.get(u["text"], default)
    return {"document_type": doc_type, "labels": out}


GOOD = {
    "SỞ Y TẾ": "co_quan_ban_hanh",
    "CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM": "quoc_hieu",
    "Độc lập - Tự do - Hạnh phúc": "tieu_ngu",
    "Số: 12/TB-SYT": "so_ky_hieu",
    "Huế, ngày 05 tháng 10 năm 2026": "dia_danh_ngay_thang",
    "THÔNG BÁO": "trich_yeu",
    "Lịch tiếp công dân tháng 10/2026": "trich_yeu",
    "GIÁM ĐỐC": "chuc_danh",
    "Phạm Văn Em": "nguoi_ky",
    "Nơi nhận:": "noi_nhan",
    "- Lưu: VT.": "noi_nhan",
}


class TestLabelsApplied(unittest.TestCase):
    def setUp(self):
        self.content = flat_centered_doc()
        self.task = labeling_task_for(self.content)

    def test_task_shape(self):
        self.assertTrue(self.task["ok"])
        self.assertIn("nguoi_ky", self.task["components"])
        ids = [u["id"] for u in self.task["units"]]
        self.assertEqual(len(ids), len(set(ids)))
        self.assertTrue(any("hint" in u for u in self.task["units"]))

    def test_heuristic_misses_signer(self):
        rep = check_document(self.content, segmenter="heuristic")
        self.assertNotIn("Phạm Văn Em",
                         rep["components"].get("nguoi_ky", {}).get("text", ""))

    def test_labels_fix_components(self):
        rep = check_document(self.content,
                             labels=labels_for(self.task, GOOD))
        c = rep["components"]
        self.assertEqual(rep["segmentation"]["method"], "labels")
        self.assertEqual(c["nguoi_ky"]["text"], "Phạm Văn Em")
        self.assertEqual(c["chuc_danh"]["text"], "GIÁM ĐỐC")
        self.assertEqual(c["co_quan_ban_hanh"]["text"], "SỞ Y TẾ")
        self.assertNotIn("Phạm Văn Em", c["noi_dung"]["text"])
        self.assertEqual(rep["document_type"]["detected"], "thong_bao")

    def test_size_check_follows_labels(self):
        """Signer at 11pt: only caught once the line is labelled nguoi_ky."""
        content = flat_centered_doc(signer_size=11)
        task = labeling_task_for(content)
        rep = check_document(content, labels=labels_for(task, GOOD))
        size = [c for c in rep["checks"] if c["id"] == "nguoi_ky.size"][0]
        self.assertNotEqual(size["status"], "pass")
        self.assertEqual(size["evidence"][0]["text"], "Phạm Văn Em")

    def test_missing_and_invalid_labels_fall_back(self):
        reply = labels_for(self.task, GOOD)
        ids = list(reply["labels"])
        del reply["labels"][ids[0]]
        reply["labels"][ids[1]] = "khong_ton_tai"
        reply["labels"]["999"] = "noi_dung"
        rep = check_document(self.content, labels=reply)
        sg = rep["segmentation"]
        self.assertEqual(sg["missing"], [ids[0]])
        self.assertEqual(sg["invalid"][0]["id"], ids[1])
        self.assertEqual(sg["unknown_ids"], ["999"])
        self.assertTrue(rep["ok"])

    def test_reply_as_fenced_string(self):
        reply = "```json\n%s\n```" % json.dumps(labels_for(self.task, GOOD))
        rep = check_document(self.content, labels=reply)
        self.assertEqual(rep["components"]["nguoi_ky"]["text"], "Phạm Văn Em")

    def test_khac_excluded(self):
        m = dict(GOOD, **{"- Lưu: VT.": "khac"})
        rep = check_document(self.content, labels=labels_for(self.task, m))
        self.assertNotIn("Lưu", rep["components"]["noi_nhan"]["text"])

    def test_long_body_elided(self):
        body = "".join(p("Đoạn nội dung số %d." % i, align="both")
                       for i in range(400))
        content = build_docx(doc(
            p("CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", align="center", bold=True)
            + p("Độc lập - Tự do - Hạnh phúc", align="center", bold=True)
            + body))
        task = labeling_task_for(content)
        self.assertLess(len(task["units"]), 120)
        self.assertGreater(task["elided_unit_count"], 250)
        rep = check_document(content, labels={"labels": {}})
        # elided units count as nội dung, not as missing labels
        self.assertLess(len(rep["segmentation"]["missing"]), 120)
        self.assertIn("Đoạn nội dung số 200.",
                      rep["components"]["noi_dung"]["text"])


class _FakeWeKnora(BaseHTTPRequestHandler):
    """Stands in for WeKnora's POST /api/v1/llm/chat."""
    reply = None
    seen = []

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        type(self).seen.append((self.path, self.headers.get("X-API-Key"), body))
        out = {"success": True, "data": {
            "model_id": body.get("model_id", "qwen"),
            "model_name": "Qwen/Qwen3.6-35B-A3B-FP8",
            "content": type(self).reply(body), "finish_reason": "stop"}}
        data = json.dumps(out).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def log_message(self, *a):
        pass


class TestWeKnoraBackend(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.srv = HTTPServer(("127.0.0.1", 0), _FakeWeKnora)
        threading.Thread(target=cls.srv.serve_forever, daemon=True).start()
        cls.base = "http://127.0.0.1:%d/api/v1" % cls.srv.server_port

    @classmethod
    def tearDownClass(cls):
        cls.srv.shutdown()

    def setUp(self):
        self.content = flat_centered_doc()
        good = json.dumps(labels_for(labeling_task_for(self.content), GOOD))
        _FakeWeKnora.reply = staticmethod(lambda body: good)
        _FakeWeKnora.seen = []
        self.env = {k: os.environ.pop(k) for k in list(os.environ)
                    if k.startswith(("DOCFORMAT_LLM_", "WEKNORA_"))}

    def tearDown(self):
        for k in list(os.environ):
            if k.startswith(("DOCFORMAT_LLM_", "WEKNORA_")):
                del os.environ[k]
        os.environ.update(self.env)

    def test_env_selects_weknora_default_model(self):
        os.environ.update({"WEKNORA_BASE_URL": self.base,
                           "WEKNORA_API_KEY": "sk-wk"})
        cfg = LLMConfig.from_env()
        self.assertEqual((cfg.backend, cfg.model), ("weknora", "default"))
        rep = check_document(self.content, segmenter="auto")
        sg = rep["segmentation"]
        self.assertEqual(sg["method"], "llm")
        self.assertEqual(sg["backend"], "weknora")
        self.assertEqual(sg["model"], "Qwen/Qwen3.6-35B-A3B-FP8")
        self.assertEqual(rep["components"]["nguoi_ky"]["text"], "Phạm Văn Em")
        path, key, body = _FakeWeKnora.seen[0]
        self.assertEqual(path, "/api/v1/llm/chat")
        self.assertEqual(key, "sk-wk")
        self.assertNotIn("model_id", body, "default model = omit model_id")
        self.assertIs(body["thinking"], False)
        self.assertIs(body["json"], True)

    def test_explicit_model_id(self):
        os.environ.update({"WEKNORA_BASE_URL": self.base,
                           "WEKNORA_API_KEY": "sk-wk",
                           "DOCFORMAT_LLM_MODEL_ID": "m-123"})
        check_document(self.content, segmenter="llm")
        self.assertEqual(_FakeWeKnora.seen[0][2]["model_id"], "m-123")

    def test_openai_endpoint_wins_when_set(self):
        os.environ.update({"WEKNORA_API_KEY": "sk-wk",
                           "DOCFORMAT_LLM_BASE_URL": "http://x/v1",
                           "DOCFORMAT_LLM_MODEL": "m"})
        self.assertEqual(LLMConfig.from_env().backend, "openai")

    def test_nothing_configured(self):
        self.assertIsNone(LLMConfig.from_env())


class _FakeLLM(BaseHTTPRequestHandler):
    reply = None
    status = 200
    seen = []

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        type(self).seen.append((self.path, self.headers.get("Authorization"),
                                body))
        if type(self).status != 200:
            self.send_response(type(self).status)
            self.end_headers()
            self.wfile.write(b"boom")
            return
        out = {"choices": [{"message": {"content": type(self).reply(body)}}]}
        data = json.dumps(out).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def log_message(self, *a):
        pass


class TestLLMClient(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.srv = HTTPServer(("127.0.0.1", 0), _FakeLLM)
        threading.Thread(target=cls.srv.serve_forever, daemon=True).start()
        cls.cfg = LLMConfig(
            base_url="http://127.0.0.1:%d/v1" % cls.srv.server_port,
            model="fake", api_key="k", timeout=5)

    @classmethod
    def tearDownClass(cls):
        cls.srv.shutdown()

    def setUp(self):
        _FakeLLM.status = 200
        _FakeLLM.seen = []
        self.content = flat_centered_doc()
        task = labeling_task_for(self.content)
        good = json.dumps(labels_for(task, GOOD))
        _FakeLLM.reply = staticmethod(lambda body: "Đây là kết quả:\n" + good)

    def test_llm_segmenter(self):
        rep = check_document(self.content, segmenter="llm",
                             llm_config=self.cfg)
        self.assertEqual(rep["segmentation"]["method"], "llm")
        self.assertEqual(rep["components"]["nguoi_ky"]["text"], "Phạm Văn Em")
        path, auth, body = _FakeLLM.seen[0]
        self.assertEqual(path, "/v1/chat/completions")
        self.assertEqual(auth, "Bearer k")
        self.assertEqual(body["temperature"], 0)
        self.assertIn("Phạm Văn Em", body["messages"][1]["content"])

    def test_extra_body_merged(self):
        cfg = LLMConfig(base_url=self.cfg.base_url, model="fake",
                        extra_body={"chat_template_kwargs":
                                    {"enable_thinking": False}})
        check_document(self.content, segmenter="llm", llm_config=cfg)
        body = _FakeLLM.seen[0][2]
        self.assertEqual(body["chat_template_kwargs"],
                         {"enable_thinking": False})

    def test_type_is_structural_not_llm_guess(self):
        """'V/v thông báo ...' stays công văn even if the model says
        thong_bao."""
        from docformat.labeling import apply_labels, build_units
        from docformat.layout import inspect_docx
        from docformat.segment import segment
        content = build_docx(doc(
            p("Số: 45/SNV-VP") + p("V/v thông báo lịch họp")
            + p("Nội dung.", align="both")))
        layout = inspect_docx(content)
        heur = segment(layout)
        units = build_units(layout, heur)
        lab = {u["id"]: ("so_ky_hieu" if u["text"].startswith("Số")
                         else "trich_yeu" if u["text"].startswith("V/v")
                         else "noi_dung") for u in units}
        seg, diag = apply_labels(layout, {"document_type": "thong_bao",
                                          "labels": lab}, heur, units)
        self.assertEqual(seg.detected_type, "cong_van")
        self.assertEqual(diag["document_type"]["llm"], "thong_bao")

    def test_transient_error_retried_once(self):
        calls = {"n": 0}
        import docformat.llm as L
        orig = L._chat_once

        def once(cfg, messages, json_mode):
            calls["n"] += 1
            if calls["n"] == 1:
                err = L.LLMError("LLM unreachable: timed out")
                err.transient = True
                raise err
            return orig(cfg, messages, json_mode)
        L._chat_once = once
        try:
            rep = check_document(self.content, segmenter="llm",
                                 llm_config=self.cfg)
        finally:
            L._chat_once = orig
        self.assertEqual(rep["segmentation"]["method"], "llm")
        self.assertEqual(calls["n"], 2)

    def test_llm_error_falls_back_to_heuristic(self):
        _FakeLLM.status = 500
        rep = check_document(self.content, segmenter="llm",
                             llm_config=self.cfg)
        self.assertTrue(rep["ok"])
        self.assertEqual(rep["segmentation"]["method"], "heuristic")
        self.assertIn("HTTP 500", rep["segmentation"]["error"])

    def test_unconfigured_llm_reports(self):
        os.environ.pop("DOCFORMAT_LLM_BASE_URL", None)
        rep = check_document(self.content, segmenter="llm")
        self.assertEqual(rep["segmentation"]["method"], "heuristic")
        self.assertIn("chưa cấu hình", rep["segmentation"]["error"])


if __name__ == "__main__":
    unittest.main()
