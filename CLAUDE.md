# Frontend Rule: `frontend/` is frozen — port to `frontend-next/`

The Vue front-end in `frontend/` is **legacy and frozen for the UI migration to `frontend-next/` (Next.js)**.

## Rules for all agents

- **NEVER modify, fix, refactor, or add UI code in `frontend/`** (the Vue front-end). Treat it as read-only reference.
- **ALL UI changes, bug fixes, and new features MUST be implemented in `frontend-next/`**.
- When porting a feature, review the corresponding Vue implementation in `frontend/` to understand the behavior/design, then reimplement it in `frontend-next/` using its stack and conventions (Next.js/React, TypeScript, Tailwind).
- **i18n convention for `frontend-next/`**: only **2 locales — `en` and `vi`** (`lib/i18n.tsx`). Do NOT port the other Vue locales (`zh-CN`, `ja-JP`, `ko-KR`, `ru-RU`) or the vue-i18n machinery; when porting a feature, translate its UI strings into the `en` and `vi` dictionaries only.
- Exceptions (read-only actions on `frontend/` are allowed): reading code for reference, running/building it to compare behavior, and extracting assets (images, icons, i18n strings).
- Non-UI shared code (backend `internal/`, `cmd/`, etc.) is unaffected by this rule.
<!-- gitnexus:end -->
# Search: use ripgrep (`rg`) instead of `grep`

`rg` (ripgrep 14) is installed system-wide. In shell commands, use it in place of `grep -r`/`find | xargs grep`:

- `rg PATTERN [PATH]` — recursive by default, honours `.gitignore`, skips hidden/binary files. Add `-uu` to search ignored + hidden files (e.g. `node_modules`, `.env*`).
- Filter by type/glob: `rg -t go 'func New'`, `rg -t ts useState` (covers .tsx; see `rg --type-list`), `rg -g '*.yaml' -g '!vendor/**' key`.
- Useful flags: `-n` line numbers (default in a terminal), `-l` files only, `-c` counts, `-i`/`-S` ignore/smart case, `-w` whole word, `-F` literal string, `-C 3` context, `-o` only match, `--json` machine output.
- Regex is Rust syntax (no backrefs/lookaround); use `-P` for PCRE2 when you need them.
- List files instead of `find`: `rg --files -g '*.sql'`.
- Exit code 1 means "no match", not an error — append `|| true` in pipelines that must not fail.
- Plain `grep` is still fine for filtering a pipe (`cmd | grep x`); the rule is about searching files.
- For symbol-level questions in an indexed repo (callers, impact), prefer the GitNexus tools over text search.

<!-- gitnexus:start -->
# Database: Postgres only — SQLite is no longer maintained

Deployments run `DB_DRIVER=postgres`. The SQLite path (the former WeKnora Lite desktop build, removed 2026-10-17) is unmaintained since 2026-10-11; `migrations/sqlite` only remains as the schema for SQLite-backed tests:

- Write new migrations in `migrations/versioned/` only; do NOT add `migrations/sqlite/` mirrors or SQLite migration tests.
- Take the next number after the highest **committed** version; a lower number is skipped on databases already past it.
# GitNexus — Code Intelligence

This project is indexed by GitNexus as **WeRAG-main** (83109 symbols, 463059 relationships, 300 execution flows). Use the GitNexus MCP tools to understand code, assess impact, and navigate safely.

> Index stale? Run `node .gitnexus/run.cjs analyze` from the project root — it auto-selects an available runner. No `.gitnexus/run.cjs` yet? `npx gitnexus analyze` (npm 11 crash → `npm i -g gitnexus`; #1939).

## Always Do

- **MUST run impact analysis before editing any symbol.** Before modifying a function, class, or method, run `impact({target: "symbolName", direction: "upstream"})` and report the blast radius (direct callers, affected processes, risk level) to the user.
- **MUST run `detect_changes()` before committing** to verify your changes only affect expected symbols and execution flows. For regression review, compare against the default branch: `detect_changes({scope: "compare", base_ref: "main"})`.
- **MUST warn the user** if impact analysis returns HIGH or CRITICAL risk before proceeding with edits.
- When exploring unfamiliar code, use `query({query: "concept"})` to find execution flows instead of grepping. It returns process-grouped results ranked by relevance.
- When you need full context on a specific symbol — callers, callees, which execution flows it participates in — use `context({name: "symbolName"})`.

## Never Do

- NEVER edit a function, class, or method without first running `impact` on it.
- NEVER ignore HIGH or CRITICAL risk warnings from impact analysis.
- NEVER rename symbols with find-and-replace — use `rename` which understands the call graph.
- NEVER commit changes without running `detect_changes()` to check affected scope.

## Resources

| Resource | Use for |
|----------|---------|
| `gitnexus://repo/WeRAG-main/context` | Codebase overview, check index freshness |
| `gitnexus://repo/WeRAG-main/clusters` | All functional areas |
| `gitnexus://repo/WeRAG-main/processes` | All execution flows |
| `gitnexus://repo/WeRAG-main/process/{name}` | Step-by-step execution trace |

## CLI

| Task | Read this skill file |
|------|---------------------|
| Understand architecture / "How does X work?" | `.claude/skills/gitnexus/gitnexus-exploring/SKILL.md` |
| Blast radius / "What breaks if I change X?" | `.claude/skills/gitnexus/gitnexus-impact-analysis/SKILL.md` |
| Trace bugs / "Why is X failing?" | `.claude/skills/gitnexus/gitnexus-debugging/SKILL.md` |
| Rename / extract / split / refactor | `.claude/skills/gitnexus/gitnexus-refactoring/SKILL.md` |
| Tools, resources, schema reference | `.claude/skills/gitnexus/gitnexus-guide/SKILL.md` |
| Index, status, clean, wiki CLI commands | `.claude/skills/gitnexus/gitnexus-cli/SKILL.md` |

<!-- gitnexus:end -->
