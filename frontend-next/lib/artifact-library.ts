/* Ported from frontend/src/utils/artifactLibrary.ts + sessionArtifacts.ts format helpers. */

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
  const startOfDay = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
  const days = Math.round((startOfDay(now) - startOfDay(at)) / 86_400_000);
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

export function formatArtifactDateTime(raw: string | undefined | null): string {
  if (!raw) return "—";
  const parsed = new Date(raw);
  if (Number.isNaN(parsed.getTime())) return String(raw);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${parsed.getFullYear()}-${pad(parsed.getMonth() + 1)}-${pad(parsed.getDate())} ${pad(parsed.getHours())}:${pad(parsed.getMinutes())}`;
}
