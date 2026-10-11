/* Quick actions offered above the composer while a passage is highlighted
 * in the editor (document assistant). Picking one fills the composer with
 * its request; the selection rides that turn as usual. The diagram action
 * is named after what the passage looks like (steps → process, units →
 * organisation chart, dates → timeline). */

export type SelectionSuggestionKey =
  | "rewrite"
  | "shorten"
  | "spelling"
  | "diagramProcess"
  | "diagramOrg"
  | "diagramTimeline"
  | "diagram"
  | "illustration"
  | "legal";

const MAX_SUGGESTIONS = 5;

const PROCESS_RE =
  /(quy trình|trình tự|thủ tục|các bước|bước \d|tiếp nhận|thẩm định|phê duyệt|sau khi|tiếp theo|chuyển đến|luân chuyển)/i;
const ORG_RE =
  /(cơ cấu tổ chức|trực thuộc|phòng ban|các phòng|chi cục|ban giám đốc|giám đốc|phó giám đốc|lãnh đạo sở|đơn vị sự nghiệp|bộ phận)/i;
const LEGAL_RE = /(\bĐiều \d|\bkhoản \d|Nghị định|Thông tư|\bLuật\b|Quyết định số|Nghị quyết|Chỉ thị)/i;
const YEAR_RE = /\b(19|20)\d{2}\b/g;
const DATE_RE = /\bngày \d{1,2}(\/| tháng )\d{1,2}/gi;
const LIST_LINE_RE = /^\s*(\d+[.)]|[a-zđ]\)|[-–•+])\s+/i;

function wordCount(s: string): number {
  return s.trim().split(/\s+/).filter(Boolean).length;
}

/** The actions worth offering for `text`, most relevant first. */
export function selectionSuggestions(text: string): SelectionSuggestionKey[] {
  const flat = text.replace(/\s+/g, " ").trim();
  if (!flat) return [];
  const words = wordCount(flat);
  const out: SelectionSuggestionKey[] = ["rewrite"];
  if (words < 4) {
    // a word or two: spelling and a legal lookup are what make sense
    out.push("spelling");
    if (LEGAL_RE.test(flat)) out.push("legal");
    return out;
  }
  const listLines = text.split(/\r?\n/).filter((l) => LIST_LINE_RE.test(l)).length;
  const dates = (flat.match(YEAR_RE)?.length ?? 0) + (flat.match(DATE_RE)?.length ?? 0);
  if (ORG_RE.test(flat)) out.push("diagramOrg");
  if (PROCESS_RE.test(flat) || listLines >= 3) out.push("diagramProcess");
  if (dates >= 3) out.push("diagramTimeline");
  if (!out.some((k) => k.startsWith("diagram")) && words >= 25) out.push("diagram");
  if (words > 40) out.push("shorten");
  out.push("spelling");
  if (LEGAL_RE.test(flat)) out.push("legal");
  out.push("illustration");
  return out.slice(0, MAX_SUGGESTIONS);
}
