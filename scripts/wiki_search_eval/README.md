# Wiki search evaluation (VBPL knowledge bases)

`queries.json` holds 64 queries in 7 groups (số hiệu, tên văn bản, không dấu /
đảo từ, diễn đạt khác, quan hệ văn bản, lĩnh vực / cơ quan, nhiễu) with the
expected page per query, resolved from the database at run time (document
number, title fragment, or slug). `run.py` sends each query to the live wiki
search API ("mới") and replays the pre-full-text regex query in SQL ("cũ"),
then prints hit@1, hit@5 and MRR per group plus every miss.

```
export PGPASSWORD=...
python3 scripts/wiki_search_eval/run.py --token <access token of a tenant member> \
    [--api http://localhost:18080] [--psql "psql -h 172.20.0.3 -U postgres -d WeKnora"]
```

Queries whose expected page is not in the KB (e.g. fully expired documents
that were never ingested) are reported as skipped, not as misses.

## Result on 2026-10-13 (56 scored queries, commit of migration 000135)

| Group | n | new hit@1 | new hit@5 | new MRR | old hit@1 | old hit@5 | old MRR |
|---|---|---|---|---|---|---|---|
| số hiệu | 12 | 0.83 | 1.00 | 0.89 | 0.50 | 0.75 | 0.61 |
| tên văn bản | 19 | 0.95 | 1.00 | 0.97 | 0.68 | 0.89 | 0.78 |
| không dấu / đảo từ | 8 | 0.88 | 0.88 | 0.88 | 0.00 | 0.00 | 0.00 |
| diễn đạt khác | 8 | 0.12 | 0.25 | 0.16 | 0.00 | 0.00 | 0.00 |
| quan hệ văn bản | 3 | 0.00 | 1.00 | 0.44 | 0.00 | 0.00 | 0.00 |
| lĩnh vực / cơ quan | 4 | 0.50 | 0.75 | 0.56 | 0.00 | 0.25 | 0.06 |
| nhiễu | 2 | 1.00 | 1.00 | 1.00 | 1.00 | 1.00 | 1.00 |
| **total** | 56 | **0.71** | **0.86** | **0.77** | 0.38 | 0.52 | 0.44 |

Mean latency: new 35 ms (API round trip), old 128 ms (direct SQL).

Known limits: "diễn đạt khác" queries ask about content that the catalogue
pages do not carry (a page has metadata, the ingest summary and relations,
not the article text), so "mức phạt không đội mũ bảo hiểm" cannot find
Nghị định 168/2024 through the wiki; that is a chat / search_knowledge
question. "thuế VAT" fails because the pages never say "VAT".
