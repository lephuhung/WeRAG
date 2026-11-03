# TVPL → WeRAG amendment links

Reads a document on thuvienphapluat.vn (TVPL), converts it to
"WeRAG legal HTML v1" and pushes it to `POST /system/legal-documents/push`
(spec: `docs/legal-document-feed-spec.md` §9). The server resolves A and every
amending document B in the catalogue and files each link in the SuperAdmin
review queue (tab "Liên kết điều khoản"); nothing is used before approval,
and the resolver reads approved TVPL links only when
`legal.current_wording.feed_source` is on.

| File | Does |
|---|---|
| `tvpl_fetch.py` | loads pages with Lightpanda, ≥ 5 s apart, cached; loads the note of every highlighted provision (`GetLqhlTip`) from inside the page; stops at a Cloudflare challenge, a login wall or locked notes |
| `tvpl_convert.py` | page + notes → v1 HTML, metadata, report |
| `tvpl_push.py` | CLI: search by số hiệu → fetch → convert → push |
| `test_tvpl_convert.py` | `python3 -m unittest scripts/tvpl/test_tvpl_convert.py` |

## Run

Lightpanda (tested: nightly `1.1.0-nightly.11582`, x86_64 Linux) —
download `lightpanda-x86_64-linux` from
<https://github.com/lightpanda-io/browser/releases/tag/nightly>, `chmod +x`.
Python 3.10+ with `beautifulsoup4`.

```bash
# SuperAdmin JWT or an API key created in platform tenant 10000 (ingest)
export WERAG_API_KEY=sk-...
# only if notes are members-only (most documents): read at run time, only
# in a 0600 temp script inside the 0700 cache dir during sign-in, then
# deleted; the session cookies stay in --cache-dir/session-cookies.json
export TVPL_USER=... TVPL_PASS=...

python3 -I scripts/tvpl/tvpl_push.py --api http://localhost:18080/api/v1 \
    --lightpanda ~/bin/lightpanda --cache-dir ~/data/tvpl-cache --out-dir ~/data/tvpl-out \
    --login 123/2020/NĐ-CP 78/2021/TT-BTC
```

`--login` is needed once; later runs reuse the cookies until TVPL expires
them (then the script stops with "hides the amendment notes … sign in").
`--dry-run` writes `<out-dir>/<số hiệu>.html/.json` without pushing.
`--mode` defaults to `links` (review queue only, never a KB).

## What is read

* body: `div.content1`; Điều/khoản/điểm from anchors `dieu_4`,
  `khoan_5_4`, `diem_a_5_4` (`diem_dd_…` = điểm đ), text patterns otherwise;
  quoted wording, headings and phụ lục are content;
* marks: `atmm=".lqhlTootip-<id>"` → `GetLqhlTip` answer, one
  `div.bmTitle` per amending document: the note text gives the operation
  ("được sửa đổi/bổ sung/thay thế/bãi bỏ/hướng dẫn… bởi") and the effective
  date; the số hiệu of B is the one written in the note, else the first
  number of B's URL slug (`…-so-76-2025-QH15-659899.aspx`); B's position
  is the link's `anchor=` or the note's words, the more specific one when
  they agree;
* a mark on an unnumbered paragraph goes to the provision the note names
  ("Khoản này …"); marks on Chương/Mục headings are reported, not placed;
* TVPL's own inserted guidance (`<huongdan>`, `#note_…`) is dropped.

Do not add a way around a Cloudflare challenge: the script stops, wait and
run again later, or ask TVPL (under the agreement) to allow the server IP.
