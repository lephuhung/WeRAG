"use client";

import { useEffect, useMemo, useState } from "react";
import { getParserEngines, type ParserEngineInfo } from "@/lib/api/system";
import type { ParserEngineRule } from "@/lib/api/knowledge";
import { useT, type LocaleKey } from "@/lib/i18n";
import { Select } from "@/components/select";

/* Embedded port of frontend/src/views/knowledge/settings/KBParserSettings.vue:
 * one engine <select> per file-type family present in the upload batch
 * (relevantExtensions), driven by the backend's parser-engine registry. */

const SIMPLE_EXTS = new Set(["md", "markdown", "txt", "csv", "json"]);

const GROUP_DEFS: { key: string; labelKey: string; exts: string[] }[] = [
  { key: "pdf", labelKey: "ps.fileTypePdf", exts: ["pdf"] },
  { key: "office", labelKey: "ps.fileTypeWord", exts: ["docx", "doc"] },
  { key: "ppt", labelKey: "ps.fileTypePpt", exts: ["pptx", "ppt"] },
  { key: "excel", labelKey: "ps.fileTypeExcel", exts: ["xlsx", "xls"] },
  { key: "ebook", labelKey: "ps.fileTypeEbook", exts: ["epub"] },
  { key: "webarchive", labelKey: "ps.fileTypeWebArchive", exts: ["mhtml"] },
  { key: "csv", labelKey: "ps.fileTypeCsv", exts: ["csv"] },
  { key: "markdown", labelKey: "Markdown", exts: ["md", "markdown"] },
  { key: "text", labelKey: "ps.fileTypeText", exts: ["txt"] },
  { key: "json", labelKey: "ps.fileTypeJson", exts: ["json"] },
  { key: "image", labelKey: "ps.fileTypeImage", exts: ["jpg", "jpeg", "png", "gif", "bmp", "tiff", "webp"] },
  { key: "audiovisual", labelKey: "ps.fileTypeAudiovisual", exts: ["mp3", "wav", "m4a", "flac", "ogg"] },
];

interface Group {
  key: string;
  label: string;
  extensions: string[];
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
        });
    }
    /* Extensions the backend registry knows but no family covers get their own
     * dynamic row, matching the Vue component. */
    const grouped = new Set(out.flatMap((g) => g.extensions));
    for (const ext of [...ft].filter((x) => !grouped.has(x) && x !== "url").sort()) {
      out.push({ key: `dyn-${ext}`, label: ext.toUpperCase(), extensions: [ext] });
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

  if (engines === null) {
    return <p className="caption text-muted">{t("ps.parserLoading")}</p>;
  }
  if (groups.length === 0) {
    return <p className="caption text-muted">{t("ps.parserNoEngines")}</p>;
  }

  return (
    <div
      className={`grid grid-cols-1 gap-x-6 sm:grid-cols-2 ${
        columns === 3 ? "lg:grid-cols-3" : ""
      }`}
    >
      {groups.map((g) => {
        const { available, defaultName } = engineOptions(g.extensions);
        const selected = engineFor(g.extensions);
        return (
          <div key={g.key} className="flex items-center justify-between gap-3 py-1">
            <div className="min-w-0 text-[12.5px] font-medium text-ink">
              <span className="truncate">{g.label}</span>
              <span className="ml-1.5 whitespace-nowrap text-[10.5px] font-normal text-muted-soft">
                {g.extensions.map((x) => `.${x}`).join(" ")}
              </span>
            </div>
            <div className="flex shrink-0 flex-col items-end gap-0.5">
              <Select
                className="h-7 w-[150px] py-0.5 px-2.5 text-[12px]"
                value={selected}
                onChange={(v) => setEngine(g.extensions, v)}
                disabled={available.length === 0}
                placeholder={t("ps.parserNoEngine")}
                options={available.map((e) => ({
                  value: e.Name,
                  label:
                    e.Name === defaultName ? `${e.Name} (${t("ps.parserDefault")})` : e.Name,
                }))}
              />
              {g.extensions.includes("xlsx") && selected === "builtin" && (
                /* Pill toggle for the two-state header flag. */
                <button
                  type="button"
                  role="switch"
                  aria-checked={ruleFor(g.extensions)?.xlsx_first_row_as_header === true}
                  onClick={() =>
                    setXlsxHeader(
                      g.extensions,
                      !(ruleFor(g.extensions)?.xlsx_first_row_as_header === true),
                    )
                  }
                  className={`rounded-full border px-2 py-0.5 text-[10.5px] font-medium transition-colors ${
                    ruleFor(g.extensions)?.xlsx_first_row_as_header === true
                      ? "border-emerald-500/30 bg-emerald-500/10 text-emerald-600"
                      : "border-hairline text-muted hover:text-ink"
                  }`}
                >
                  {t("ps.parserXlsxHeader")}
                </button>
              )}
            </div>
          </div>
        );
      })}
    </div>
  );
}
