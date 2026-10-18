/** Joins dictated text onto what is already typed: a space between words,
 *  and a capital letter when the text starts a sentence. */
export function appendDictated(before: string, text: string): string {
  if (!text) return before;
  const startsSentence = /(^|[.!?…:]\s*|\n\s*)$/.test(before);
  const piece = startsSentence ? text.charAt(0).toUpperCase() + text.slice(1) : text;
  const sep = before === "" || /\s$/.test(before) ? "" : " ";
  return before + sep + piece;
}
