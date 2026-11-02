/* Human labels of provision paths ("dieu:5/khoan:2/diem:a") used by the
 * legal link review (admin-legal-links.tsx). */

/** "dieu:5/khoan:2/diem:a" → "điểm a khoản 2 Điều 5" (vi) or "Art. 5(2)(a)" (en). */
export function provisionLabel(path: string, locale: string): string {
  if (!path) return "";
  const appendix = path.startsWith("phuluc/");
  const parts: Record<string, string> = {};
  for (const seg of path.replace(/^phuluc\//, "").split("/")) {
    const [k, v] = seg.split(":");
    if (k && v) parts[k] = v;
  }
  let out: string;
  if (locale === "vi") {
    out = [parts.diem && `điểm ${parts.diem}`, parts.khoan && `khoản ${parts.khoan}`, parts.dieu && `Điều ${parts.dieu}`]
      .filter(Boolean)
      .join(" ");
    if (appendix) out += " (phụ lục)";
  } else {
    out = `Art. ${parts.dieu ?? "?"}${parts.khoan ? `(${parts.khoan})` : ""}${parts.diem ? `(${parts.diem})` : ""}`;
    if (appendix) out += " (appendix)";
  }
  return out || path;
}
