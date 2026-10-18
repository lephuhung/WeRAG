/* Ported from frontend/src/utils/artifactLibrary.ts + sessionArtifacts.ts format helpers. */

import { formatDateTime, vnDaysAgo } from "./datetime.ts";

export type ArtifactCategory =
  | "all"
  | "document"
  | "spreadsheet"
  | "presentation"
  | "image"
  | "web"
  | "data";

export const ARTIFACT_CATEGORIES: ArtifactCategory[] = [
  "all",
  "document",
  "spreadsheet",
  "presentation",
  "image",
  "web",
  "data",
];

const CATEGORY_EXTENSIONS: Record<Exclude<ArtifactCategory, "all">, string[]> = {
  document: [".pdf", ".doc", ".docx", ".md", ".txt", ".rtf", ".odt"],
  spreadsheet: [".xlsx", ".xls", ".csv", ".tsv", ".ods"],
  presentation: [".pptx", ".ppt", ".key", ".odp"],
  image: [".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp", ".bmp"],
  web: [".html", ".htm"],
  data: [".json", ".xml", ".yaml", ".yml", ".zip"],
};

export function artifactCategoryExtensions(category: ArtifactCategory): string[] {
  return category === "all" ? [] : [...CATEGORY_EXTENSIONS[category]];
}

export function parseArtifactCategory(raw: unknown): ArtifactCategory {
  return typeof raw === "string" && (ARTIFACT_CATEGORIES as string[]).includes(raw)
    ? (raw as ArtifactCategory)
    : "all";
}

export type ArtifactDateGroup = "today" | "yesterday" | "last7Days" | "last30Days" | "earlier";

export function artifactDateGroup(raw: string, now: Date = new Date()): ArtifactDateGroup {
  const at = new Date(raw);
  if (Number.isNaN(at.getTime())) return "earlier";
  // Calendar days in Vietnam time, whatever the viewer's zone.
  const days = vnDaysAgo(at, now);
  if (days <= 0) return "today";
  if (days === 1) return "yesterday";
  if (days < 7) return "last7Days";
  if (days < 30) return "last30Days";
  return "earlier";
}

export function groupArtifactsByDate<T extends { created_at: string }>(
  items: T[],
  now: Date = new Date(),
): { group: ArtifactDateGroup; items: T[] }[] {
  const sections: { group: ArtifactDateGroup; items: T[] }[] = [];
  for (const item of items) {
    const group = artifactDateGroup(item.created_at, now);
    const last = sections[sections.length - 1];
    if (last && last.group === group) last.items.push(item);
    else sections.push({ group, items: [item] });
  }
  return sections;
}

export function formatArtifactSize(size: number | undefined | null): string {
  if (!size || size < 0) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  let value = size;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }
  return unit === 0 ? `${value} ${units[unit]}` : `${value.toFixed(1)} ${units[unit]}`;
}

/** "14h05 - 16/10/2026" in Vietnam time. */
export function formatArtifactDateTime(raw: string | undefined | null, locale: string = "vi"): string {
  if (!raw) return "—";
  const parsed = new Date(raw);
  if (Number.isNaN(parsed.getTime())) return String(raw);
  return formatDateTime(parsed, locale);
}
