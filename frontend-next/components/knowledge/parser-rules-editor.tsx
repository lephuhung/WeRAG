"use client";

import { useEffect, useMemo, useState } from "react";
import { getParserEngines, type ParserEngineInfo } from "@/lib/api/system";
import type { ParserEngineRule } from "@/lib/api/knowledge";
import { useT, type LocaleKey } from "@/lib/i18n";
import { Select } from "@/components/select";

/* Embedded port of frontend/src/views/knowledge/settings/KBParserSettings.vue:
 * one engine <select> per file-type family present in the upload batch
 * (relevantExtensions), driven by the backend's parser-engine registry.
 * Rows are grouped into Documents / Data & Web / Media sections, each with a
 * soft-tinted type glyph (same color language as the mention picker). */

const SIMPLE_EXTS = new Set(["md", "markdown", "txt", "csv", "json"]);

type GroupCategory = "documents" | "data" | "media";

const GROUP_DEFS: {
  key: string;
  labelKey: string;
  exts: string[];
  category: GroupCategory;
}[] = [
  { key: "pdf", labelKey: "ps.fileTypePdf", exts: ["pdf"], category: "documents" },
  { key: "office", labelKey: "ps.fileTypeWord", exts: ["docx", "doc"], category: "documents" },
  { key: "ppt", labelKey: "ps.fileTypePpt", exts: ["pptx", "ppt"], category: "documents" },
  { key: "excel", labelKey: "ps.fileTypeExcel", exts: ["xlsx", "xls"], category: "documents" },
  { key: "ebook", labelKey: "ps.fileTypeEbook", exts: ["epub"], category: "documents" },
  { key: "text", labelKey: "ps.fileTypeText", exts: ["txt"], category: "documents" },
  { key: "markdown", labelKey: "Markdown", exts: ["md", "markdown"], category: "documents" },
  { key: "csv", labelKey: "ps.fileTypeCsv", exts: ["csv"], category: "data" },
  { key: "json", labelKey: "ps.fileTypeJson", exts: ["json"], category: "data" },
  { key: "webarchive", labelKey: "ps.fileTypeWebArchive", exts: ["mhtml"], category: "data" },
  { key: "image", labelKey: "ps.fileTypeImage", exts: ["jpg", "jpeg", "png", "gif", "bmp", "tiff", "webp"], category: "media" },
  { key: "audiovisual", labelKey: "ps.fileTypeAudiovisual", exts: ["mp3", "wav", "m4a", "flac", "ogg"], category: "media" },
];

const CATEGORY_ORDER: GroupCategory[] = ["documents", "data", "media"];

const CATEGORY_LABEL_KEY: Record<GroupCategory, LocaleKey> = {
  documents: "ps.groupDocuments",
  data: "ps.groupData",
  media: "ps.groupMedia",
};

/* Soft-tinted abbreviation glyph per family — mirrors the mention picker's
 * file-type badge colors (red/blue/orange/emerald…). */
const GROUP_GLYPH: Record<string, { glyph: string; cls: string }> = {
  pdf: { glyph: "PDF", cls: "bg-red-500/10 text-red-600 dark:text-red-400" },
  office: { glyph: "DOC", cls: "bg-blue-500/10 text-blue-600 dark:text-blue-400" },
  ppt: { glyph: "PPT", cls: "bg-orange-500/10 text-orange-600 dark:text-orange-400" },
  excel: { glyph: "XLS", cls: "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400" },
  ebook: { glyph: "EPUB", cls: "bg-amber-500/10 text-amber-600 dark:text-amber-400" },
  text: { glyph: "TXT", cls: "bg-sky-500/10 text-sky-600 dark:text-sky-400" },
  markdown: { glyph: "MD", cls: "bg-violet-500/10 text-violet-600 dark:text-violet-400" },
  csv: { glyph: "CSV", cls: "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400" },
  json: { glyph: "JSON", cls: "bg-sky-500/10 text-sky-600 dark:text-sky-400" },
  webarchive: { glyph: "MHT", cls: "bg-amber-500/10 text-amber-600 dark:text-amber-400" },
  image: { glyph: "IMG", cls: "bg-pink-500/10 text-pink-600 dark:text-pink-400" },
  audiovisual: { glyph: "AUD", cls: "bg-teal-500/10 text-teal-600 dark:text-teal-400" },
};

interface Group {
  key: string;
  label: string;
  extensions: string[];
  category: GroupCategory;
}

export function ParserRulesEditor({
  rules,
  relevantExtensions,
  onChange,
  columns = 2,
}: {
  rules: ParserEngineRule[] | undefined;
  /** Only show groups overlapping these extensions (empty = show all). */
  relevantExtensions: string[];
  onChange: (rules: ParserEngineRule[]) => void;
  /* Grid columns ≥lg: 2 keeps the upload dialog readable, 3 densifies the
   * full-width parse-defaults editor so it fits without scrolling. */
  columns?: 2 | 3;
}) {
  const { t } = useT();
  const [engines, setEngines] = useState<ParserEngineInfo[] | null>(null);

  useEffect(() => {
    let cancelled = false;
    getParserEngines()
      .then((res) => {
        if (!cancelled) setEngines(res?.data ?? []);
      })
      .catch(() => {
        if (!cancelled) setEngines([]);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const groups = useMemo<Group[]>(() => {
    if (!engines) return [];
    const ft = new Set<string>();
    for (const e of engines) for (const x of e.FileTypes || []) ft.add(x);

    const out: Group[] = [];
    for (const def of GROUP_DEFS) {
      const exts = def.exts.filter((x) => ft.has(x));
      if (exts.length)
        out.push({
          key: def.key,
          label: def.labelKey.startsWith("ps.") ? t(def.labelKey as LocaleKey) : def.labelKey,
          extensions: exts,
          category: def.category,
        });
    }
    /* Extensions the backend registry knows but no family covers get their own
     * dynamic row in the data section, matching the Vue component. */
    const grouped = new Set(out.flatMap((g) => g.extensions));
    for (const ext of [...ft].filter((x) => !grouped.has(x) && x !== "url").sort()) {
      out.push({ key: `dyn-${ext}`, label: ext.toUpperCase(), extensions: [ext], category: "data" });
    }

    if (!relevantExtensions.length) return out;
    const rel = new Set(relevantExtensions);
    const filtered = out.filter((g) => g.extensions.some((x) => rel.has(x)));
    return filtered.length > 0 ? filtered : out;
  }, [engines, relevantExtensions, t]);

  const engineOptions = (extensions: string[]) => {
    const raw = (engines ?? []).filter((e) =>
      extensions.some((ext) => (e.FileTypes || []).includes(ext)),
    );
    const available = raw.filter((e) => e.Available !== false);
    const allSimple = extensions.length > 0 && extensions.every((x) => SIMPLE_EXTS.has(x));
    const defaultName = !allSimple
      ? (available.find((e) => e.Name === "anydoc")?.Name ?? available[0]?.Name ?? "")
      : (available[0]?.Name ?? "");
    return { available, defaultName };
  };

  const engineFor = (extensions: string[]): string => {
    for (const rule of rules ?? []) {
      if (rule.file_types.some((x) => extensions.includes(x))) return rule.engine;
    }
    const { defaultName } = engineOptions(extensions);
    return defaultName;
  };

  const ruleFor = (extensions: string[]) =>
    (rules ?? []).find((r) => r.file_types.some((x) => extensions.includes(x)));

  /* Rebuild the full rule list the way the Vue component does: every visible
   * group contributes a rule (explicit or default engine), preserving the
   * xlsx header flag on existing rules. */
  const emitComplete = (mutate: (list: ParserEngineRule[]) => void) => {
    const next: ParserEngineRule[] = [];
    for (const g of groups) {
      const engine = engineFor(g.extensions);
      if (!engine) continue;
      const prev = ruleFor(g.extensions);
      next.push({
        file_types: [...g.extensions],
        engine,
        ...(prev?.xlsx_first_row_as_header !== undefined
          ? { xlsx_first_row_as_header: prev.xlsx_first_row_as_header }
          : {}),
      });
    }
    mutate(next);
    onChange(next);
  };

  const setEngine = (extensions: string[], engine: string) =>
    emitComplete((list) => {
      for (const r of list) {
        if (r.file_types.some((x) => extensions.includes(x))) r.engine = engine;
      }
    });

  const setXlsxHeader = (extensions: string[], checked: boolean) =>
    emitComplete((list) => {
      const r = list.find((item) => item.file_types.some((x) => extensions.includes(x)));
      if (r) r.xlsx_first_row_as_header = checked;
    });

  const rowGrid = `grid grid-cols-1 gap-x-6 sm:grid-cols-2 ${columns === 3 ? "lg:grid-cols-3" : ""}`;

  if (engines === null) {
    return <p className="caption text-muted">{t("ps.parserLoading")}</p>;
  }
  if (groups.length === 0) {
    return <p className="caption text-muted">{t("ps.parserNoEngines")}</p>;
  }

  return (
    <div className="space-y-4">
      {CATEGORY_ORDER.map((category) => {
        const catGroups = groups.filter((g) => g.category === category);
        if (catGroups.length === 0) return null;
        return (
          <div key={category}>
            <div className="caption-uppercase mb-1 flex items-center gap-2 text-muted-soft">
              {t(CATEGORY_LABEL_KEY[category])}
              <span className="h-px flex-1 bg-hairline" aria-hidden />
            </div>
            <div className={rowGrid}>
              {catGroups.map((g) => {
                const { available, defaultName } = engineOptions(g.extensions);
                const selected = engineFor(g.extensions);
                const rule = ruleFor(g.extensions);
                const isDefault = selected === defaultName;
                const glyph = GROUP_GLYPH[g.key] ?? {
                  glyph: g.key.replace(/^dyn-/, "").slice(0, 4).toUpperCase(),
                  cls: "bg-sky-500/10 text-sky-600 dark:text-sky-400",
                };
                const noEngine = available.length === 0;
                return (
                  <div
                    key={g.key}
                    className={`flex items-center gap-2.5 py-1 ${noEngine ? "opacity-55" : ""}`}
                  >
                    <span
                      className={`flex h-6 w-6 shrink-0 items-center justify-center rounded-[7px] text-[8px] font-bold tracking-tight ${glyph.cls}`}
                      aria-hidden
                    >
                      {glyph.glyph}
                    </span>
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-[12.5px] font-medium text-ink">{g.label}</div>
                      <div className="mt-0.5 flex flex-wrap gap-1">
                        {g.extensions.map((x) => (
                          <span
                            key={x}
                            className="rounded bg-surface-strong px-1 py-px text-[10px] leading-4 text-muted"
                          >
                            .{x}
                          </span>
                        ))}
                      </div>
                    </div>
                    {noEngine ? (
                      <span className="shrink-0 text-[11px] text-muted-soft">
                        — {t("ps.parserNoEngine")}
                      </span>
                    ) : (
                      <div className="flex shrink-0 items-center gap-1.5">
                        {g.extensions.includes("xlsx") && selected === "builtin" && (
                          /* Pill toggle for the two-state header flag — kept
                           * inline so rows stay a uniform height. */
                          <button
                            type="button"
                            role="switch"
                            aria-checked={rule?.xlsx_first_row_as_header === true}
                            onClick={() => setXlsxHeader(g.extensions, !(rule?.xlsx_first_row_as_header === true))}
                            className={`rounded-full border px-2 py-0.5 text-[10.5px] font-medium transition-colors ${
                              rule?.xlsx_first_row_as_header === true
                                ? "border-emerald-500/30 bg-emerald-500/10 text-emerald-600"
                                : "border-hairline text-muted hover:text-ink"
                            }`}
                          >
                            {t("ps.parserXlsxHeader")}
                          </button>
                        )}
                        <Select
                          className={`h-7 w-[150px] py-0.5 px-2.5 text-[12px] ${
                            isDefault
                              ? "text-muted"
                              : "border-sky-500/40 font-medium text-ink"
                          }`}
                          value={selected}
                          onChange={(v) => setEngine(g.extensions, v)}
                          placeholder={t("ps.parserNoEngine")}
                          options={available.map((e) => ({
                            value: e.Name,
                            label:
                              e.Name === defaultName
                                ? `${e.Name} · ${t("ps.parserDefault")}`
                                : e.Name,
                          }))}
                        />
                      </div>
                    )}
                  </div>
                );
              })}
            </div>
          </div>
        );
      })}
    </div>
  );
}
