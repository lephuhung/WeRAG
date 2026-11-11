/* CSV built in the browser (member usage export). A text cell that starts
 * like a formula (= + - @, tab, carriage return) is prefixed with ' so a
 * spreadsheet shows it as text instead of running it (CSV injection: a
 * display name such as "=HYPERLINK(...)" is user-chosen). Numbers are left
 * untouched: a negative figure is data, not a formula. */

const FORMULA_START = /^[=+\-@\t\r]/;

/** One CSV field: neutralised when it looks like a formula, then quoted when
 * it holds a separator, quote or line break. */
export function csvField(v: string | number): string {
  if (typeof v === "number") return String(v);
  let s = v ?? "";
  if (FORMULA_START.test(s)) s = `'${s}`;
  return /[",\n\r]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
}

/** Rows → CSV text with CRLF line ends and a UTF-8 BOM, so spreadsheet
 * apps read Vietnamese names correctly. */
export function toCSV(rows: (string | number)[][]): string {
  return "\uFEFF" + rows.map((r) => r.map(csvField).join(",")).join("\r\n");
}
