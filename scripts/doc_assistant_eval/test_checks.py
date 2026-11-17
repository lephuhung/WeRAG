"""Unit tests for checks.py — run offline, no backend:

    .venv/bin/python -m unittest test_checks -v

The first version of article_refs_grounded accused four scenarios of
inventing articles the model had in fact read: persist.go strips chunk
bodies from the replayed stream, so the harness could not see them. These
tests pin that case, and the other ways an article reference can be written.
"""

from __future__ import annotations

import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import checks as C  # noqa: E402
from checks import FAIL, PASS, SKIP, WARN  # noqa: E402
from werag_client import ToolCall, Turn  # noqa: E402


def tool(name: str, output: str = "", **data) -> ToolCall:
    result = {"success": True, "output": output}
    result.update(data)
    return ToolCall(call_id="c1", name=name, arguments={}, result=result)


def turn(answer: str, *tools: ToolCall, query: str = "") -> Turn:
    return Turn(query=query, request={}, answer=answer, tool_calls=list(tools))


class ArticleRefsGrounded(unittest.TestCase):
    def check(self, t: Turn):
        return C.article_refs_grounded()(t, {})

    # ---- the regression that started this ---------------------------------

    def test_skips_when_the_stream_dropped_the_chunk_bodies(self):
        """read_document's text is not in the stream (persist.go strips it).

        The model read Điều 8–30; accusing it of inventing them is the one
        outcome worse than missing a real fabrication.
        """
        read = tool(
            "read_document",
            output="Listed 11/41 chunks from Nghị định 53/2022/NĐ-CP (content omitted from history)",
            display_type="knowledge_chunks_list",
            fetched_chunks=11,
            total_chunks=41,
        )
        res = self.check(turn("Điều 13 thẩm định, Điều 14 đánh giá, Điều 15 giám sát.", read))
        self.assertEqual(res.status, SKIP, res.detail)
        self.assertIn("not in the event stream", res.detail)

    def test_skips_a_grep_result_in_the_shape_persist_go_really_writes(self):
        """persist.go words and counts a grep differently from a chunk list.

        It writes "details omitted from history" and counts with
        total_matches / document_count — reading only fetched_chunks and
        "content omitted" missed this shape entirely.
        """
        grep = tool(
            "grep_chunks",
            output="Keyword search found 7 matching chunks across 2 document(s) "
                   "(details omitted from history)",
            display_type="grep_results",
            total_matches=7,
            document_count=2,
        )
        res = self.check(turn("Xem Điều 27.", grep))
        self.assertEqual(res.status, SKIP, res.detail)

    def test_skips_a_grep_counted_only_by_total_matches(self):
        """Same shape with the sentence rewritten: the display type decides."""
        grep = tool("grep_chunks", output="", display_type="grep_results", total_matches=4)
        self.assertEqual(self.check(turn("Xem Điều 27.", grep)).status, SKIP)

    def test_find_in_documents_keeps_its_output_so_it_is_checked(self):
        """find_in_documents is display_type document_search, not stripped."""
        found = tool("find_in_documents", output="Điều 27. Lưu trữ dữ liệu",
                     display_type="document_search")
        self.assertEqual(self.check(turn("Điều 27 …", found)).status, PASS)
        self.assertEqual(
            self.check(turn("Điều 28 …", found)).status, WARN,
            "a tool that kept its output must still be checked")

    def test_still_checks_when_the_chunks_are_present(self):
        listed = tool(
            "read_document",
            output="Điều 13. Thẩm định an ninh mạng",
            display_type="knowledge_chunks_list",
            fetched_chunks=1,
            chunks=[{"content": "Điều 13. Thẩm định an ninh mạng"}],
        )
        self.assertEqual(self.check(turn("Theo Điều 13 …", listed)).status, PASS)

    # ---- the core verdict -------------------------------------------------

    def test_flags_an_article_absent_from_the_evidence(self):
        res = self.check(turn("Điều 30 và Điều 31 quy định …",
                              tool("search_knowledge", output="Điều 30. Trách nhiệm …")))
        self.assertEqual(res.status, WARN)
        self.assertIn("Điều 31", res.detail)
        self.assertNotIn("Điều 30", res.detail.split("(of")[0])

    def test_passes_when_every_article_is_in_the_evidence(self):
        res = self.check(turn("Điều 5 và Điều 7.",
                              tool("search_knowledge", output="Điều 5 … Điều 7 …")))
        self.assertEqual(res.status, PASS)

    def test_status_is_configurable(self):
        res = C.article_refs_grounded(status=FAIL)(
            turn("Điều 99.", tool("search_knowledge", output="Điều 1")), {})
        self.assertEqual(res.status, FAIL)

    # ---- skips ------------------------------------------------------------

    def test_skips_an_answer_with_no_article(self):
        self.assertEqual(self.check(turn("Văn bản còn hiệu lực.",
                                         tool("check_legal_status", output="ok"))).status, SKIP)

    def test_skips_when_no_tool_ran(self):
        self.assertEqual(self.check(turn("Điều 13 quy định …")).status, SKIP)

    # ---- evidence sources -------------------------------------------------

    def test_the_question_counts_as_evidence(self):
        res = self.check(turn("Điều 13 nói về thẩm định.",
                              tool("search_knowledge", output="không có kết quả"),
                              query="Điều 13 nói gì?"))
        self.assertEqual(res.status, PASS)

    def test_the_models_own_arguments_do_not_count(self):
        """Asking for "section: Điều 4" must not prove Điều 4 to itself."""
        res = self.check(turn("Điều 4 quy định …",
                              tool("search_document_section", output="", query="Điều 4",
                                   section="Điều 4", fetched_chunks=0)))
        self.assertEqual(res.status, WARN, res.detail)
        self.assertIn("Điều 4", res.detail)

    def test_structured_data_counts_as_evidence(self):
        res = self.check(turn("Điều 21 …",
                              tool("search_knowledge", output="",
                                   results=[{"content": "Điều 21. Lưu trữ dữ liệu"}])))
        self.assertEqual(res.status, PASS)

    # ---- spellings --------------------------------------------------------

    def test_evidence_list_spelling_counts_for_each_number(self):
        res = self.check(turn("Điều 14 quy định …",
                              tool("search_knowledge", output="Điều 13, 14 và 15 quy định …")))
        self.assertEqual(res.status, PASS, res.detail)

    def test_evidence_range_spelling_counts_for_the_whole_range(self):
        for ev in ("Điều 5 đến Điều 9", "Điều 5-9", "từ Điều 5 tới Điều 9"):
            with self.subTest(ev=ev):
                res = self.check(turn("Điều 7 …", tool("search_knowledge", output=ev)))
                self.assertEqual(res.status, PASS, f"{ev}: {res.detail}")

    def test_absurd_range_is_not_expanded(self):
        res = self.check(turn("Điều 50 …", tool("search_knowledge", output="Điều 1 đến Điều 999")))
        self.assertEqual(res.status, WARN, res.detail)

    def test_evidence_is_case_and_form_insensitive(self):
        for ev in ("điều 13", "ĐIỀU 13", "khoản 2 điều 13"):
            with self.subTest(ev=ev):
                self.assertEqual(self.check(turn("Điều 13 …",
                                                 tool("search_knowledge", output=ev))).status, PASS)

    def test_four_digit_numbers_are_not_read_as_articles(self):
        """"Điều 2020" is a year gone wrong, not article 202."""
        self.assertEqual(self.check(turn("Nghị định ban hành Điều 2020 …",
                                         tool("search_knowledge", output="Điều 1"))).status,
                         SKIP, "a 4-digit number must not be taken for an article")

    def test_article_like_words_are_not_matched(self):
        for text in ("Điều khoản 5 nói …", "Điều kiện 3 là …"):
            with self.subTest(text=text):
                self.assertEqual(self.check(turn(text, tool("search_knowledge", output=""))).status,
                                 SKIP, text)


if __name__ == "__main__":
    unittest.main()
