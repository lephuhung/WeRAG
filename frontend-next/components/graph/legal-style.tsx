"use client";

import { GRAPH_FAMILY, type GraphFamily } from "@/components/graph/graph-palette";
import type { LegalGraphDoc } from "@/lib/api/graph";

/* Colours and small parts shared by the focused legal graph and its
 * provision panel: doc-type families (cards), relation families (edges,
 * legend, operation chips) and the status chip. */

const LEGAL_GROUPS: Record<string, string[]> = {
  law: ["Hiến pháp", "Bộ luật", "Luật", "Pháp lệnh", "Sắc lệnh", "Sắc luật", "Lệnh"],
  decree: ["Nghị định"],
  circular: ["Thông tư", "Thông tư liên tịch", "Thông tư liên bộ"],
  decision: ["Quyết định", "Nghị quyết", "Nghị quyết liên tịch", "Chỉ thị"],
  // Party and steering documents (dangcongsan.vn): guidance, conclusions,
  // regulations, plans, notices, dispatches
  party: ["Hướng dẫn", "Kết luận", "Quy định", "Quy chế", "Kế hoạch", "Thông báo", "Công văn", "Chương trình", "Đề án", "Điều lệ"],
};
export const LEGAL_FAMILY: Record<string, GraphFamily> = {
  law: "red",
  decree: "skyDeep",
  circular: "emerald",
  decision: "amber",
  party: "purple",
  other: "slate",
};

export function legalGroup(docType: string) {
  for (const [group, docTypes] of Object.entries(LEGAL_GROUPS)) {
    if (docTypes.includes(docType)) return group;
  }
  return "other";
}

/** The legend's relation families; every catalogue relation maps onto one. */
export type RelFamily = "amends" | "replaces" | "repeals" | "guides" | "consolidates" | "based_on";
const REL_FAMILY: Record<string, RelFamily> = {
  amends: "amends",
  corrects: "amends",
  replaces: "replaces",
  succeeds: "replaces",
  repeals: "repeals",
  expires: "repeals",
  partially_expires: "repeals",
  suspends: "repeals",
  guides: "guides",
  interprets: "guides",
  consolidates: "consolidates",
  based_on: "based_on",
  references: "based_on",
};
export const REL_COLOR: Record<RelFamily, GraphFamily> = {
  amends: "indigo",
  replaces: "purple",
  repeals: "rose",
  guides: "teal",
  consolidates: "sky",
  based_on: "slate",
};
export const relFamily = (rel: string): RelFamily => REL_FAMILY[rel] ?? "amends";
export const relColor = (rel: string) => GRAPH_FAMILY[REL_COLOR[relFamily(rel)]].stroke;
/** The colour of a provision operation (its implied relation's family). */
export function opColor(op: string) {
  switch (op) {
    case "bai_bo":
    case "tam_ngung":
    case "dinh_chi":
      return relColor("repeals");
    case "thay_the":
      return relColor("replaces");
    case "giai_thich":
      return relColor("guides");
  }
  return relColor("amends");
}

export function docLabel(d: LegalGraphDoc) {
  const num = d.document_number?.trim();
  return num ? `${d.doc_type} ${num}`.trim() : d.title;
}

/* statusTone maps a validity status to its colour family: expired → error,
 * partly expired / suspended / not yet in force → warning, in force →
 * success, unknown → muted. */
export function statusTone(status: string): "error" | "warning" | "success" | "muted" {
  const s = (status ?? "").toLowerCase();
  if (s.includes("hết hiệu lực toàn bộ") || s.includes("bị bãi bỏ")) return "error";
  if (s.includes("một phần") || s.includes("ngưng") || s.includes("chưa")) return "warning";
  if (s.includes("còn hiệu lực") || s.includes("có hiệu lực")) return "success";
  return "muted";
}

const STATUS_CHIP: Record<ReturnType<typeof statusTone>, string> = {
  error: "bg-error-surface text-error",
  warning: "bg-warning-surface text-warning",
  success: "bg-success-surface text-success",
  muted: "bg-surface-strong text-muted",
};

const STATUS_DOT: Record<ReturnType<typeof statusTone>, string> = {
  error: "bg-error",
  warning: "bg-warning",
  success: "bg-success",
  muted: "bg-muted-soft",
};

export function StatusChip({ status }: { status: string }) {
  if (!status) return null;
  return <span className={`inline-block max-w-full truncate rounded px-1.5 py-px text-[10.5px] font-medium ${STATUS_CHIP[statusTone(status)]}`}>{status}</span>;
}

/* StatusDot is the chip shrunk to a dot for the compact graph cards; the
 * status itself is in the card's tooltip. */
export function StatusDot({ status }: { status: string }) {
  if (!status) return null;
  return <span className={`inline-block h-1.5 w-1.5 shrink-0 rounded-full ${STATUS_DOT[statusTone(status)]}`} title={status} />;
}

