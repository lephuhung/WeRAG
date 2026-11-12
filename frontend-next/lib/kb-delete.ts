/* Pure helpers of the "Delete knowledge base" dialog
 * (components/knowledge/kb-delete-dialog.tsx). */
import type { LocaleKey } from "./i18n.tsx";

/** A KB name as typed or shown: NFC (Vietnamese typed with combining marks
 * vs precomposed), whitespace runs folded, trimmed. */
export function normalizeKbName(s: string): string {
  return s.normalize("NFC").replace(/\s+/g, " ").trim();
}

/** Whether the typed confirmation matches the KB name. */
export function kbNameMatches(typed: string, kbName: string): boolean {
  const name = normalizeKbName(kbName);
  return name !== "" && normalizeKbName(typed) === name;
}

/** Who else loses the KB, from the access overview the managers get. */
export type KbDeleteReach = {
  visibility?: string;
  /** Personal invitations and organization grants. */
  grantees?: number;
};

/** Extra warning lines (i18n key + vars) for a KB others read too. */
export function kbDeleteWarnings(reach: KbDeleteReach | undefined): { key: LocaleKey; vars?: Record<string, number> }[] {
  if (!reach) return [];
  const out: { key: LocaleKey; vars?: Record<string, number> }[] = [];
  if (reach.visibility === "public") out.push({ key: "kbDelete.warnPublic" });
  else if (reach.visibility === "published") out.push({ key: "kbDelete.warnPublished" });
  if (reach.grantees && reach.grantees > 0) out.push({ key: "kbDelete.warnGrantees", vars: { count: reach.grantees } });
  return out;
}
