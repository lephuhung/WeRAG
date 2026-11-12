/* File name of an answer exported to .docx — the same rule as the server's
 * Content-Disposition (internal/docformat/answerdocx.FileName): the
 * download goes through apiDownload, which hands back only the blob. */

const MAX_QUESTION_CHARS = 60;

export function answerDocxFileName(question?: string): string {
  let q = (question ?? "")
    .replace(/[\u0000-\u001f\u007f\\/:*?"<>|]/g, " ")
    .split(/\s+/)
    .filter(Boolean)
    .join(" ");
  const chars = Array.from(q);
  if (chars.length > MAX_QUESTION_CHARS) {
    let cut = chars.slice(0, MAX_QUESTION_CHARS).join("");
    const space = cut.lastIndexOf(" ");
    if (space > MAX_QUESTION_CHARS / 2) cut = cut.slice(0, space);
    q = cut;
  }
  q = q.replace(/[ .]+$/, "");
  return q ? `Trả lời - ${q}.docx` : "Trả lời WeRAG.docx";
}
