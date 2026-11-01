/* Numbers typed into a domain pack formula example. Vietnamese writes
 * "1.000.000" and "1,5", English "1,000,000" and "1.5"; both are accepted
 * when the reading is certain:
 *   "1.000.000" → 1000000, "1.234,5" → 1234.5, "1,234.5" → 1234.5,
 *   "1,234,567" → 1234567, "0,5" → 0.5, "0.075" → 0.075.
 * One comma before exactly three digits ("1,500") is 1,5 in Vietnamese but
 * 1500 in English: it is "ambiguous" and the editor asks for 1.500 or 1,5,
 * as a value off by a factor of 1000 would be refused or, worse, match.
 * "0,015" is 0.015 (an English group never starts with 0). "" or junk →
 * null. */
export type ParsedFormulaNumber = number | "ambiguous" | null;

export function parseFormulaNumber(text: string): ParsedFormulaNumber {
  let s = text.trim().replace(/\s/g, "");
  if (!s) return null;
  if (/^-?[1-9]\d{0,2},\d{3}$/.test(s)) return "ambiguous";
  if (/^-?[1-9]\d{0,2}(\.\d{3})+(,\d+)?$/.test(s)) s = s.replace(/\./g, "").replace(",", ".");
  else if (/^-?[1-9]\d{0,2}(,\d{3})+(\.\d+)?$/.test(s)) s = s.replace(/,/g, "");
  else if (/^-?\d+,\d+$/.test(s)) s = s.replace(",", ".");
  if (!/^-?(\d+\.?\d*|\.\d+)(e[-+]?\d+)?$/i.test(s)) return null;
  const n = Number(s);
  return Number.isFinite(n) ? n : null;
}
