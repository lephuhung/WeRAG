"""Vietnamese punctuation + capitalisation for ASR text (vibert-capu, ONNX).

The model (GECToR-style tagger by dragonSwing, ONNX export by welcomyou)
labels every word with one edit: keep, capitalise, upper-case, or append
one of , . : ? after it. Only those edits are applied, so the words
themselves never change (the model's MERGE action is ignored).
"""

import os
import re
import unicodedata

import numpy as np
import onnxruntime as ort
from tokenizers import Tokenizer
from tokenizers.models import WordPiece

CAPU_DIR = os.environ.get("ASR_CAPU_DIR", "/models/vibert-capu")
CAPU_FILE = os.environ.get("ASR_CAPU_FILE", "vibert-capu.int8.onnx")

LABELS = [
    "$KEEP", "$TRANSFORM_CASE_CAPITAL", "$APPEND_,", "$APPEND_.", "$TRANSFORM_VERB_VB_VBN",
    "$TRANSFORM_CASE_UPPER", "$APPEND_:", "$APPEND_?", "$TRANSFORM_VERB_VB_VBC",
    "$TRANSFORM_CASE_LOWER", "$TRANSFORM_CASE_CAPITAL_1", "$TRANSFORM_CASE_UPPER_-1",
    "$MERGE_SPACE", "@@UNKNOWN@@", "@@PADDING@@",
]
PUNCT = {",", ".", ":", "?"}
# Mid-sentence casing and colons need a confident model: below this it
# title-cases short phrases ("Nghị Định Ba Mươi Về…") and adds "Số:" form
# colons. Proper names score >= 0.95.
SURE = 0.9
MAX_WORDS = 60  # the model was trained on up to 64 words
OVERLAP = 12


class Capu:
    def __init__(self, model_dir: str = CAPU_DIR, model_file: str = CAPU_FILE, threads: int = 2):
        vocab = {}
        with open(f"{model_dir}/vocab.txt", encoding="utf-8") as f:
            for i, line in enumerate(f):
                vocab[line.rstrip("\n")] = i
        self.start_id = len(vocab)  # "$START" was appended to the vocabulary when training
        self.unk_id = vocab["[UNK]"]
        self.wp = Tokenizer(WordPiece(vocab, unk_token="[UNK]", max_input_chars_per_word=100))
        opts = ort.SessionOptions()
        opts.intra_op_num_threads = threads
        opts.inter_op_num_threads = 1
        self.sess = ort.InferenceSession(f"{model_dir}/{model_file}", opts, providers=["CPUExecutionProvider"])

    def _labels(self, words: list[str]) -> list[tuple[int, float]]:
        """(label index, prob) for each word."""
        ids, offsets = [self.start_id], [0]
        for w in words:
            offsets.append(len(ids))
            ids.extend(self.wp.encode(w, add_special_tokens=False).ids or [self.unk_id])
        n = len(ids)
        logits, _ = self.sess.run(None, {
            "input_ids": np.array([ids], dtype=np.int64),
            "attention_mask": np.ones((1, n), dtype=np.int64),
            "token_type_ids": np.zeros((1, n), dtype=np.int64),
            "input_offsets": np.array([offsets], dtype=np.int64),
        })
        p = logits[0]
        if p.min() < 0 or abs(p.sum(-1).mean() - 1) > 1e-3:  # logits, not probabilities
            p = np.exp(p - p.max(-1, keepdims=True))
            p /= p.sum(-1, keepdims=True)
        # position 0 is $START; word i is position i + 1
        return [(int(p[i + 1].argmax()), float(p[i + 1].max())) for i in range(len(words))]

    def _pass(self, tokens: list[str]) -> list[str]:
        if not tokens:
            return tokens
        labels: list[tuple[int, float]] = [(0, 1.0)] * len(tokens)
        # Long text: overlapping windows, each word labelled by the window
        # where it sits furthest from an edge.
        start = 0
        while True:
            end = min(len(tokens), start + MAX_WORDS)
            got = self._labels(tokens[start:end])
            lo = start + (OVERLAP // 2 if start > 0 else 0)
            for k in range(lo, end):
                labels[k] = got[k - start]
            if end == len(tokens):
                break
            start = end - OVERLAP
        out = []
        for tok, (li, prob) in zip(tokens, labels):
            lab = LABELS[li]
            sentence_start = not out or out[-1] in (".", "?")
            if lab.startswith("$TRANSFORM_CASE") and lab != "$TRANSFORM_CASE_LOWER" and prob < SURE:
                lab = "$KEEP"
            if lab == "$APPEND_:" and prob < SURE:
                lab = "$KEEP"
            if lab == "$TRANSFORM_CASE_CAPITAL" or (sentence_start and lab == "$KEEP"):
                tok = tok[:1].upper() + tok[1:]
            elif lab == "$TRANSFORM_CASE_UPPER":
                tok = tok.upper()
            elif lab == "$TRANSFORM_CASE_CAPITAL_1" and len(tok) > 1:
                tok = tok[0] + tok[1].upper() + tok[2:]
            elif lab == "$TRANSFORM_CASE_UPPER_-1" and len(tok) > 1:
                tok = tok[:-1].upper() + tok[-1]
            elif lab == "$TRANSFORM_CASE_LOWER" and not sentence_start:
                tok = tok.lower()
            out.append(tok)
            if lab.startswith("$APPEND_"):
                mark = lab[len("$APPEND_"):]
                if tok not in PUNCT:
                    out.append(mark)
        # never two marks in a row
        dedup = []
        for t in out:
            if t in PUNCT and dedup and dedup[-1] in PUNCT:
                continue
            dedup.append(t)
        return dedup

    def decorate(self, context: list[str], words: list[str], iterations: int = 2) -> list[str]:
        """Each of ``words`` with its casing fixed and any punctuation that
        follows it attached. ``context`` (earlier tokens, punctuation as
        separate tokens) is only read, never returned."""
        tokens = list(context) + list(words)
        for _ in range(iterations):
            new = self._pass(tokens)
            if new == tokens:
                break
            tokens = new
        groups: list[str] = []
        for t in tokens:
            if t in PUNCT:
                if groups:
                    groups[-1] += t
            else:
                groups.append(t)
        n_ctx = sum(1 for t in context if t not in PUNCT)
        out = groups[n_ctx:]
        assert len(out) == len(words), "the tagger never adds or drops words"
        return out

    def restore(self, text: str) -> str:
        """Punctuate and capitalise plain lower-case text."""
        s = " ".join(self.decorate([], split_tokens(text)))
        return s[:1].upper() + s[1:]


def split_tokens(text: str) -> list[str]:
    """Words and , . : ? as separate tokens (other marks stay on the word)."""
    return re.findall(r"[^\s,.:?]+|[,.:?]", unicodedata.normalize("NFC", text))


class Transcript:
    """Punctuated text of one dictation session. Each new utterance is
    punctuated together with the words before it, so a pause mid-sentence
    becomes a comma (or nothing) rather than a full stop. Words more than
    KEEP_LIVE behind the end are frozen to keep the work per utterance small."""

    KEEP_LIVE = 24
    CONTEXT = 24

    def __init__(self, capu: Capu, draft: str = ""):
        self.capu = capu
        # The end of what the user had already typed is read-only context.
        self.draft = split_tokens(draft)[-self.CONTEXT:]
        self.frozen: list[str] = []
        self.live_raw: list[str] = []
        self.live: list[str] = []

    def add(self, utterance: str) -> None:
        self.live_raw += utterance.split()
        context = (self.draft + [t for w in self.frozen for t in split_tokens(w)])[-self.CONTEXT:]
        self.live = self.capu.decorate(context, self.live_raw)
        if len(self.live_raw) > 2 * self.KEEP_LIVE:
            cut = len(self.live_raw) - self.KEEP_LIVE
            self.frozen += self.live[:cut]
            self.live, self.live_raw = self.live[cut:], self.live_raw[cut:]

    def text(self) -> str:
        return " ".join(self.frozen + self.live)
