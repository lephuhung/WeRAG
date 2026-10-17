/* The format check report's findings, reviewed one by one in the report
 * modal: go to the place in the editor, apply the one formatParagraph a
 * finding carries, read the paragraph back. Ported from the Word add-in
 * (src/lib/word/format-findings.ts); the editor here is ONLYOFFICE. */
import { normalizeAnchorText, opValidationError, type DocumentOp, type ParagraphAnchor } from "./api/document-ops.ts";

export type ParagraphFormatOp = Extract<DocumentOp, { op: "formatParagraph" }>;

export type FormatFinding = {
  id: string;
  check_id: string;
  title: string;
  reason?: string;
  current?: string;
  expected?: string;
  anchor?: ParagraphAnchor;
  text?: string;
  text_occurrence?: number;
  /** Only one validated single-paragraph formatting op can be applied. */
  ops: ParagraphFormatOp[];
};

const object = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === "object" && !Array.isArray(value);
const string = (value: unknown): value is string => typeof value === "string";

/** The findings of a report, dropping malformed and duplicate ones. A
 * finding without a valid anchor keeps no ops; ops are kept only when there
 * is exactly one formatParagraph on the finding's own anchor. */
export function parseFormatFindings(raw: unknown): FormatFinding[] {
  if (!Array.isArray(raw)) return [];
  const seen = new Set<string>();
  return raw.flatMap((item): FormatFinding[] => {
    if (!object(item) || !string(item.id) || !item.id.trim() || !string(item.title) || !item.title.trim() || seen.has(item.id))
      return [];
    seen.add(item.id);
    const anchor =
      object(item.anchor) && !item.anchor.atStart && opValidationError({ op: "select", anchor: item.anchor }) === null
        ? (item.anchor as ParagraphAnchor)
        : undefined;
    const rawOps = item.ops;
    const validOps =
      anchor &&
      Array.isArray(rawOps) &&
      rawOps.length === 1 &&
      rawOps.every(
        (op) =>
          object(op) &&
          op.op === "formatParagraph" &&
          opValidationError(op) === null &&
          object(op.anchor) &&
          string(op.anchor.text) &&
          normalizeAnchorText(op.anchor.text) === normalizeAnchorText(anchor.text) &&
          (op.anchor.occurrence ?? 1) === (anchor.occurrence ?? 1) &&
          ["font", "sizePt", "alignment", "bold", "italic"].some((key) => op[key] !== undefined) &&
          (op.font === undefined || (string(op.font) && !!op.font.trim())) &&
          (op.sizePt === undefined || (typeof op.sizePt === "number" && Number.isFinite(op.sizePt) && op.sizePt > 0)),
      );
    return [
      {
        id: item.id,
        check_id: string(item.check_id) ? item.check_id : "",
        title: item.title,
        reason: string(item.reason) ? item.reason : undefined,
        current: string(item.current) ? item.current : undefined,
        expected: string(item.expected) ? item.expected : undefined,
        anchor,
        text: string(item.text) && item.text ? item.text : undefined,
        text_occurrence:
          typeof item.text_occurrence === "number" && Number.isInteger(item.text_occurrence) && item.text_occurrence > 0
            ? item.text_occurrence
            : undefined,
        ops: validOps ? (rawOps as ParagraphFormatOp[]) : [],
      },
    ];
  });
}

/** A missing leading component has no text to select: offer an explicitly
 * labelled document-start action, never a made-up paragraph anchor. */
export function findingNavigation(finding: FormatFinding): "anchor" | "document-start" | null {
  if (finding.anchor) return "anchor";
  return ["component.quoc_hieu.present", "component.tieu_ngu.present", "component.co_quan_ban_hanh.present"].includes(
    finding.check_id,
  )
    ? "document-start"
    : null;
}

/** The editor op that goes to the finding (selects its text, or the
 * cursor to the start of the document); null when it has no place. */
export function findingSelectOp(finding: FormatFinding): DocumentOp | null {
  const nav = findingNavigation(finding);
  if (nav === "document-start") return { op: "select", anchor: { atStart: true } };
  if (nav !== "anchor" || !finding.anchor) return null;
  return {
    op: "select",
    anchor: finding.anchor,
    ...(finding.text ? { text: finding.text } : {}),
    ...(finding.text && finding.text_occurrence ? { textOccurrence: finding.text_occurrence } : {}),
  };
}

/** The read-back of a fix: the same properties, checked in the editor. */
export function findingCheckOp(op: ParagraphFormatOp): DocumentOp {
  return { ...op, op: "checkFormat" };
}

/** Numbers the report's sections for display (the report is not changed). */
export function numberFormatReportSections(markdown: string): string {
  let fenced = false;
  let fence = "";
  let section = 0;
  const lines = markdown.split("\n");
  const sectionLabel =
    /^(?:Kết luận|Bảng điểm|Lỗi bắt buộc sửa|Các lỗi|Cảnh báo|Khuyến nghị|Đề xuất|Tổng quan|Đánh giá|Lưu ý)/i;
  const cleanLabel = (label: string) => label.replace(/^(?:\d+[.)]|[IVX]+[.)])\s*/i, "").replace(/:\s*$/, "");
  const headings: { level: number; label: string }[] = [];
  for (const line of lines) {
    const marker = line.match(/^\s*(`{3,}|~{3,})/);
    if (marker) {
      if (!fenced) {
        fenced = true;
        fence = marker[1][0];
      } else if (marker[1][0] === fence) fenced = false;
      continue;
    }
    const heading = !fenced && line.match(/^(#{1,3})\s+(.+)/);
    if (heading) headings.push({ level: heading[1].length, label: cleanLabel(heading[2]) });
  }
  fenced = false;
  fence = "";
  const namedSections = headings.filter((heading) => sectionLabel.test(heading.label));
  const candidates = namedSections.length ? namedSections : headings;
  const level = candidates.length ? Math.min(...candidates.map((heading) => heading.level)) : 2;
  return lines
    .map((line) => {
      const marker = line.match(/^\s*(`{3,}|~{3,})/);
      if (marker) {
        if (!fenced) {
          fenced = true;
          fence = marker[1][0];
        } else if (marker[1][0] === fence) fenced = false;
        return line;
      }
      if (fenced) return line;
      const heading = line.match(/^(#{1,3})\s+(.+?)\s*#*$/);
      const bold = line.match(/^\s*\*\*(.+?)\*\*\s*$/);
      const label = heading?.[2] ?? bold?.[1];
      if (!label || (heading && heading[1].length !== level)) return line;
      const clean = cleanLabel(label);
      // bold text is a section only when it names a report section
      if (!heading && !sectionLabel.test(clean)) return line;
      section += 1;
      return `## ${section}. ${clean}`;
    })
    .join("\n");
}
