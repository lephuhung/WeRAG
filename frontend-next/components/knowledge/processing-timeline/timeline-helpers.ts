/* Pure helpers shared by the processing-timeline coordinator and its
 * three presentational children (header / rows / detail).
 *
 * Nothing here touches React or the network: every function is a plain
 * derivation, so the coordinator and the children always agree on the
 * text they render. */

import { STAGE_LABEL, STATUS_LABEL } from "@/components/knowledge/trace-status";
import { vnParts } from "@/lib/datetime";
import type { KnowledgeTraceNode } from "@/lib/knowledge-trace";
import type { LocaleKey } from "@/lib/i18n";

export type SpanNode = KnowledgeTraceNode;

/** The translate function of the i18n context (see lib/i18n). */
export type Translate = (k: LocaleKey, vars?: Record<string, string | number>) => string;

/* ---------- timestamps ---------- */

export function parseTime(s?: string | null): number | null {
  if (!s) return null;
  const t = Date.parse(s);
  return Number.isNaN(t) ? null : t;
}

export function nodeStart(node: SpanNode): number | null {
  return parseTime(node.started_at || undefined);
}

export function nodeEnd(node: SpanNode): number | null {
  const e = parseTime(node.finished_at || undefined);
  if (e !== null) return e;
  const s = nodeStart(node);
  if (s !== null && typeof node.duration_ms === "number" && node.duration_ms > 0) {
    return s + node.duration_ms;
  }
  return null;
}

export function collectStarts(node: SpanNode | undefined, out: number[]) {
  if (!node) return;
  const s = nodeStart(node);
  if (s !== null) out.push(s);
  for (const c of node.children || []) collectStarts(c, out);
}

export function collectEnds(node: SpanNode | undefined, out: number[]) {
  if (!node) return;
  const e = nodeEnd(node);
  if (e !== null) out.push(e);
  for (const c of node.children || []) collectEnds(c, out);
}

/* ---------- formatting ---------- */

export function formatDuration(ms?: number): string {
  if (ms === undefined || ms === null || isNaN(ms) || ms < 0) return "—";
  if (ms < 1000) return `${Math.round(ms)}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(2)}s`;
  const mins = Math.floor(ms / 60000);
  const rem = ((ms % 60000) / 1000).toFixed(1);
  return `${mins}m${rem}s`;
}

/* A skipped stage did not execute — its bookkeeping interval is not
 * processing time, so don't present it as such. */
export function formatSpanDuration(node: SpanNode): string {
  if (node.status === "skipped" || node.status === "pending") return "—";
  return formatDuration(node.duration_ms);
}

export function formatTime(s?: string | null): string {
  if (!s) return "—";
  const ts = Date.parse(s);
  if (Number.isNaN(ts)) return s;
  const d = new Date(ts);
  const ms = String(d.getMilliseconds()).padStart(3, "0");
  // Vietnam time, whatever the viewer's zone.
  const p = vnParts(d);
  if (!p) return s;
  const pad = (n: number) => String(n).padStart(2, "0");
  const hh = pad(p.hour);
  const mm = pad(p.minute);
  const ss = pad(p.second);
  /* Omit the year when it matches the current one; month+day always
   * shown so older traces stay unambiguous. */
  const yyyy = p.year;
  const mo = pad(p.month);
  const dd = pad(p.day);
  const datePart = yyyy === vnParts(new Date())?.year ? `${mo}-${dd}` : `${yyyy}-${mo}-${dd}`;
  return `${datePart} ${hh}:${mm}:${ss}.${ms}`;
}

/* ---------- payloads ---------- */

export function isObjectWithKeys(v: unknown): v is Record<string, unknown> {
  return !!v && typeof v === "object" && !Array.isArray(v) && Object.keys(v).length > 0;
}

export function hasContent(v: unknown): boolean {
  if (v === null || v === undefined || v === "") return false;
  if (Array.isArray(v)) return v.length > 0;
  if (typeof v === "object") return Object.keys(v).length > 0;
  return true;
}

export function prettyJSON(v: unknown): string {
  try {
    return JSON.stringify(v, null, 2);
  } catch {
    return String(v);
  }
}

export function humanizeKey(k: string): string {
  return k
    .replace(/[_-]+/g, " ")
    .replace(/\s+/g, " ")
    .trim()
    .replace(/\b([a-z])/g, (_, c: string) => c.toUpperCase());
}

export interface KvEntry {
  key: string;
  label: string;
  kind: "scalar" | "bool" | "array" | "object";
  display: string;
  raw: unknown;
  /* Short payloads render their JSON inline — no extra click needed. */
  defaultExpanded: boolean;
}

const KV_INLINE_ARRAY_LIMIT = 8;
const KV_INLINE_OBJECT_KEY_LIMIT = 8;
const KV_INLINE_JSON_BYTES_LIMIT = 600;

function shouldInlineExpand(value: unknown): boolean {
  if (Array.isArray(value)) {
    if (value.length > KV_INLINE_ARRAY_LIMIT) return false;
    try {
      return JSON.stringify(value).length <= KV_INLINE_JSON_BYTES_LIMIT;
    } catch {
      return false;
    }
  }
  if (value && typeof value === "object") {
    if (Object.keys(value).length > KV_INLINE_OBJECT_KEY_LIMIT) return false;
    try {
      return JSON.stringify(value).length <= KV_INLINE_JSON_BYTES_LIMIT;
    } catch {
      return false;
    }
  }
  return false;
}

function toKvEntry(key: string, value: unknown): KvEntry {
  const label = humanizeKey(key);
  if (value === null || value === undefined) {
    return { key, label, kind: "scalar", display: "—", raw: value, defaultExpanded: false };
  }
  if (typeof value === "boolean") {
    return { key, label, kind: "bool", display: value ? "true" : "false", raw: value, defaultExpanded: false };
  }
  if (typeof value === "number") {
    return { key, label, kind: "scalar", display: value.toLocaleString(), raw: value, defaultExpanded: false };
  }
  if (typeof value === "string") {
    return { key, label, kind: "scalar", display: value, raw: value, defaultExpanded: false };
  }
  if (Array.isArray(value)) {
    return {
      key, label, kind: "array",
      display: `Array · ${value.length}`, raw: value,
      defaultExpanded: shouldInlineExpand(value),
    };
  }
  if (typeof value === "object") {
    const n = Object.keys(value as object).length;
    return {
      key, label, kind: "object",
      display: `Object · ${n} keys`, raw: value,
      defaultExpanded: shouldInlineExpand(value),
    };
  }
  return { key, label, kind: "scalar", display: String(value), raw: value, defaultExpanded: false };
}

export function buildKvEntries(obj: unknown): KvEntry[] {
  if (!isObjectWithKeys(obj)) return [];
  return Object.entries(obj).map(([key, value]) => toKvEntry(key, value));
}

/* ---------- file types ---------- */

export function normalizeFileType(value: string): string {
  return String(value || "").trim().replace(/^\./, "").toLowerCase();
}

export function getFileTypeFromName(name: string): string {
  const clean = String(name || "").split(/[?#]/)[0];
  const dot = clean.lastIndexOf(".");
  return dot >= 0 ? clean.slice(dot + 1) : "";
}

/* ---------- flattened rows ---------- */

export interface FlatRow {
  key: string;
  depth: number;
  node: SpanNode;
  hasChildren: boolean;
  isRoot: boolean;
  isStage: boolean;
  parentKey?: string;
}

export function rowKey(node: SpanNode, fallback: string): string {
  return node.span_id || fallback;
}

export function isPlaceholder(node: SpanNode): boolean {
  return !node.span_id && !node.started_at;
}

/* ---------- labels ---------- */

export function localizedStatus(status: string, t: Translate): string {
  const key = STATUS_LABEL[status];
  return key ? t(key) : status;
}

export function stageLabelFor(name: string, t: Translate): string {
  const key = STAGE_LABEL[name];
  return key ? t(key) : name;
}

export function rowLabel(row: FlatRow, t: Translate): string {
  if (row.isRoot) return t("ks.root");
  if (row.isStage) return stageLabelFor(row.node.name, t);
  if (row.node.name === "postprocess.graph") return t("ks.proc.graph");
  const graphChunk = /^postprocess\.graph\.chunk\[(\d+)\]$/.exec(row.node.name);
  if (graphChunk) return `${t("ks.proc.graph")} #${Number(graphChunk[1]) + 1}`;
  return row.node.name;
}

export function rowKindLabel(row: FlatRow): string {
  if (row.isRoot) return "root";
  if (row.isStage) return "stage";
  return row.node.kind || "span";
}

/* ---------- shared class names ---------- */

/* The small square icon button used by the header and the detail panel. */
export const ICON_BTN_CLS =
  "inline-flex h-[26px] w-[26px] items-center justify-center rounded-[6px] text-muted-soft transition-colors hover:bg-surface-strong hover:text-ink disabled:cursor-not-allowed disabled:opacity-40";
