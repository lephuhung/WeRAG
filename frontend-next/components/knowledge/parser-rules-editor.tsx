"use client";

import { useEffect, useMemo, useState } from "react";
import { getParserEngines, type ParserEngineInfo } from "@/lib/api/system";
import type { ParserEngineRule } from "@/lib/api/knowledge";
import { useT, type LocaleKey } from "@/lib/i18n";
import { Select } from "@/components/select";
import { KIND_COLOR, type FileKind } from "@/components/files/file-icon";
import { IMAGE_EXTENSIONS } from "@/lib/parse-settings";

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
  { key: "text", labelKey: "ps.fileTypeText", exts: ["txt"], category: "documents" },
  { key: "markdown", labelKey: "Markdown", exts: ["md", "markdown"], category: "documents" },
  { key: "csv", labelKey: "ps.fileTypeCsv", exts: ["csv"], category: "data" },
  { key: "json", labelKey: "ps.fileTypeJson", exts: ["json"], category: "data" },
  { key: "webarchive", labelKey: "ps.fileTypeWebArchive", exts: ["mhtml"], category: "data" },
  { key: "audiovisual", labelKey: "ps.fileTypeAudiovisual", exts: ["mp3", "wav", "m4a", "flac", "ogg"], category: "media" },
];

const CATEGORY_ORDER: GroupCategory[] = ["documents", "data", "media"];

const CATEGORY_LABEL_KEY: Record<GroupCategory, LocaleKey> = {
  documents: "ps.groupDocuments",
  data: "ps.groupData",
  media: "ps.groupMedia",
};

/* Soft-tinted abbreviation glyph per family — the same colour language as
 * the file icons (components/files/file-icon.ts): one KIND_COLOR map for
 * every file-type badge. */
const GROUP_GLYPH: Record<string, { glyph: string; kind: FileKind }> = {
  pdf: { glyph: "PDF", kind: "file-pdf" },
  office: { glyph: "DOC", kind: "file-word" },
  ppt: { glyph: "PPT", kind: "file-powerpoint" },
  excel: { glyph: "XLS", kind: "file-excel" },
  text: { glyph: "TXT", kind: "text" },
  markdown: { glyph: "MD", kind: "text" },
  csv: { glyph: "CSV", kind: "file-excel" },
  json: { glyph: "JSON", kind: "code" },
  webarchive: { glyph: "MHT", kind: "file" },
  audiovisual: { glyph: "AUD", kind: "sound" },
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
     * dynamic row in the data section, matching the Vue component. Images
     * are not: knowledge bases take documents only. */
    const grouped = new Set(out.flatMap((g) => g.extensions));
    const images = new Set([...IMAGE_EXTENSIONS, "tif", "tiff"]);
    for (const ext of [...ft].filter((x) => !grouped.has(x) && !images.has(x) && x !== "url").sort()) {
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
    const defaultName = available[0]?.Name ?? "";
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

  const rowGrid = `grid grid-cols-1 gap-2.5 sm:grid-cols-2 ${columns === 3 ? "lg:grid-cols-3" : ""}`;

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
            <div className="caption-uppercase mb-2 flex items-center gap-2 text-muted-soft">
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
                  kind: "file" as FileKind,
                };
                const glyphColor = KIND_COLOR[glyph.kind];
                const noEngine = available.length === 0;
                return (
                  <div
                    key={g.key}
                    className={`flex min-w-0 flex-col gap-3 rounded-lg2 border border-hairline bg-surface-card/60 p-3 ${noEngine ? "opacity-55" : ""}`}
                  >
                    <div className="flex min-w-0 items-center gap-2.5">
                      <span
                        className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md2 text-[9px] font-bold tracking-tight"
                        style={{
                          color: glyphColor,
                          background: `color-mix(in srgb, ${glyphColor} 12%, transparent)`,
                        }}
                        aria-hidden
                      >
                        {glyph.glyph}
                      </span>
                      <div className="min-w-0 flex-1">
                        <div className="truncate text-[13px] font-semibold text-ink">{g.label}</div>
                        <div className="mt-1 flex flex-wrap gap-1">
                          {g.extensions.map((x) => (
                            <span
                              key={x}
                              className="rounded bg-surface-strong px-1.5 py-px text-[10px] leading-4 text-muted"
                            >
                              .{x}
                            </span>
                          ))}
                        </div>
                      </div>
                    </div>
                    {noEngine ? (
                      <span className="flex h-8 items-center rounded-[8px] bg-surface-strong px-3 text-[11px] text-muted-soft">
                        — {t("ps.parserNoEngine")}
                      </span>
                    ) : (
                      <div className="flex w-full items-center gap-2">
                        {g.extensions.includes("xlsx") && selected === "builtin" && (
                          <button
                            type="button"
                            role="switch"
                            aria-checked={rule?.xlsx_first_row_as_header === true}
                            onClick={() => setXlsxHeader(g.extensions, !(rule?.xlsx_first_row_as_header === true))}
                            className={`h-8 shrink-0 rounded-[8px] border px-2.5 text-[10.5px] font-medium transition-colors ${
                              rule?.xlsx_first_row_as_header === true
                                ? "border-success/30 bg-success-surface text-success"
                                : "border-hairline text-muted hover:text-ink"
                            }`}
                          >
                            {t("ps.parserXlsxHeader")}
                          </button>
                        )}
                        <Select
                          className={`h-8 min-w-0 flex-1 px-2.5 py-0.5 text-[12px] ${
                            isDefault
                              ? "text-muted"
                              : "border-accent/40 font-medium text-ink"
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
