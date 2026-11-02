# User memory: improvement plan (2026-11-02)

Research by Opus (live data on the dev store) and Fable (independent code read
plus external systems). Code: `internal/application/service/memory/`.

## What the dev store showed

- 6 live memories for 2 people after 1,827 distilled sessions; 490 counted
  topics, none promoted (every topic seen on a single day).
- 4 of the 6 should never have been stored:
  - "giá hornor magic 9 pro" → fact (a search phrase);
  - "Văn bản đang mở có mấy đoạn…" → task with no end, recalled 14 times
    (stored before the write gate existed; the gate never re-checks old rows);
  - "chỗ HN nói là … mua usdt …" → the user's fact (someone else's words,
    stored after the gate existed);
  - "xử lý vphc trong lĩnh vực giao thông" → task with no end, recalled 56 times.

## Weaknesses in the code (Fable, spot-checked)

1. The supersede key is the model's free-text topic (`types.MemoryItemKey`),
   so two unrelated facts under "phần mềm" replace each other.
2. A model `delete` went straight through; the gate covered add/update only.
3. Lexical recall has no Vietnamese stopwords; `use_count` is bumped for every
   injected resident item, so "used" means "was in the block".
4. `valid_from` is write time, not when the user said it; replays reorder.
5. Pending guesses never expire and extraction cannot see them.
6. Merges keep one source message.
7. The eval set measured the write gate only: no delete, one update, no recall.

## Borrowed from other systems

| System | Mechanism | Use here |
|---|---|---|
| mem0 | dedup in code before the model; dropped model-chosen DELETE | delete gate (done) |
| Zep / Graphiti | invalidate, never delete; contradiction decided in code; provenance list | change-cue rule (done), provenance (phase 3) |
| Letta, Memobase | fixed profile slots declared in code, hard size cap | phase 4 |
| MemoryOS | heat score in SQL for eviction | phase 2 |
| LongMemEval, LoCoMo, MemoryAgentBench | update / temporal / abstention are where every system fails | eval groups (done, extend) |

Not adopted: graph memory (mem0-graph, Graphiti, cognee) — 3–5 model calls per
write, weak on conflict benchmarks, and our memories are per-person one-liners;
model self-editing (A-MEM, Letta) — relies on Qwen following rewrite rules;
vendor LoCoMo numbers as targets.

## Phases

### Phase 1 — write guards (branch `feat/memory-guards`, DONE)

- Gate rules (`extract_gate.go`): someone else's words ("X nói là…",
  "bảo là…") dropped; pasted material (>1,200 characters, or a chat log with
  two or more "Name:" lines) dropped unless it says "hãy nhớ / từ nay";
  short search phrases with no statement marker and no first person dropped
  (after the question rule, so a short question still becomes a week's task).
- Deletes need a change cue in the user's own sentence ("không còn", "đã
  chuyển", "xong"…), one per run.
- Same-topic collision (`replacesOnKeyCollision`): profile / preference /
  interest stay single-valued; a fact or task replaces another only with a
  change cue or largely the same wording; otherwise it is kept as a sibling
  (`topic#fingerprint` key). A different kind never replaces.
- The daily review (`regateStored`) re-runs the gate over distilled rows using
  their source message and archives what it now rejects; a question saved as a
  fact, or a task with no end, is held to the gate's 7-day task life. On the
  dev store this archives 4 of the 6 rows (the form answer "Bộ phận: Đội 4"
  stays).
- Eval: 16 new cases (third party, lookup, update/delete); the harness grades
  both the raw model output and what the gate lets through, and simulates the
  store's topic-collision rule.

Measured on Qwen3.6-35B-A3B (.240), 3 repeats:

| | cases | stored pass | third party | update/delete |
|---|---|---|---|---|
| before (old gate) | 153 | 146 | 8/12 | 11/12 (store would lose the eOffice note) |
| after | 156 | 152 | 15/15 | 12/12 |

The remaining 4 failures are the model's (topic names, one empty reply).

### Phase 2 — recall precision

- Vietnamese stoplist in `lexical.go` tokenisation; consider Postgres FTS fused
  with pgvector by RRF (wiki already uses FTS).
- Touch only situational matches, not the resident block; heat score in SQL.
- Recall eval: ~30 Vietnamese query → expected-memory pairs, precision@5.

### Phase 3 — time and provenance

- `observed_at` = when the user said it; consolidation orders by it.
- Pending guesses archived after 30 days; extraction sees pending items.
- `source_message_ids` union on merge.

### Phase 4 — profile slots

Fixed slots in code (đơn vị, chức vụ, lĩnh vực, cách trả lời…); extraction
maps to a slot or `other`. Removes free-text topics as keys. Needs prompt,
schema and UI work.

## Running the eval

```
cd internal/application/service/memory
SSRF_WHITELIST=10.10.0.0/24 WEKNORA_MEMORY_EVAL_MODEL=Qwen/Qwen3.6-35B-A3B-FP8 \
WEKNORA_MEMORY_EVAL_BASE_URL=http://10.10.0.240:8000/v1 WEKNORA_MEMORY_EVAL_REPEAT=3 \
go test -run 'TestVietnamesePromptEval$' -count=1 -v .
```
