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
};
export const LEGAL_FAMILY: Record<string, GraphFamily> = {
  law: "red",
  decree: "skyDeep",
  circular: "emerald",
  decision: "amber",
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
  repeals: "repeals",
  expires: "repeals",
  partially_expires: "repeals",
  suspends: "repeals",
  guides: "guides",
  interprets: "guides",
  consolidates: "consolidates",
  based_on: "based_on",
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

export function StatusChip({ status }: { status: string }) {
  if (!status) return null;
  const s = status.toLowerCase();
  let tone = "bg-surface-strong text-muted";
  if (s.includes("hết hiệu lực toàn bộ") || s.includes("bị bãi bỏ")) tone = "bg-error-surface text-error";
  else if (s.includes("một phần") || s.includes("ngưng") || s.includes("chưa")) tone = "bg-warning-surface text-warning";
  else if (s.includes("còn hiệu lực") || s.includes("có hiệu lực")) tone = "bg-success-surface text-success";
  return <span className={`inline-block max-w-full truncate rounded px-1.5 py-px text-[10.5px] font-medium ${tone}`}>{status}</span>;
}

