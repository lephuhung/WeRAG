/* Document assistant: open documents are named inline in the message text as
 * "@<file name>", so a request can read "Báo cáo @a để so sánh @b". The text
 * is the source of truth: whatever "@name" survives editing is what is sent. */

export type MentionSegment = { text: string; mention: boolean };

/* Splits text into plain runs and "@name" runs. Longer names win, so
 * "@a.docx" never claims the head of "@a.docx (2).docx". */
export function splitDocumentMentions(text: string, names: string[]): MentionSegment[] {
  const tokens = [...new Set(names.filter(Boolean))].sort((a, b) => b.length - a.length).map((n) => `@${n}`);
  const out: MentionSegment[] = [];
  let plain = "";
  let i = 0;
  while (i < text.length) {
    const hit = text[i] === "@" ? tokens.find((tok) => text.startsWith(tok, i)) : undefined;
    if (hit) {
      if (plain) out.push({ text: plain, mention: false });
      plain = "";
      out.push({ text: hit, mention: true });
      i += hit.length;
    } else {
      plain += text[i];
      i++;
    }
  }
  if (plain) out.push({ text: plain, mention: false });
  return out;
}

/* The documents named in text, in order of first appearance. */
export function documentsNamedIn<T extends { name: string }>(text: string, docs: T[]): T[] {
  const names = splitDocumentMentions(text, docs.map((d) => d.name))
    .filter((s) => s.mention)
    .map((s) => s.text.slice(1));
  return [...new Set(names)].flatMap((name) => docs.filter((d) => d.name === name));
}
