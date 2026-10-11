/* Drawing the document assistant's illustrations in the browser.
 *
 * draw_diagram sends Mermaid source; it is drawn here (mermaid is loaded
 * on first use, it is large) so the Vietnamese labels come out exactly as
 * written. Labels are SVG text (htmlLabels off): an SVG with <foreignObject>
 * taints the canvas and could not become the PNG the editor inserts.
 */

/** Font of NĐ30 documents, so an inserted diagram matches the text. */
const DIAGRAM_FONT = '"Times New Roman", Times, serif';

type MermaidApi = typeof import("mermaid").default;
let mermaidPromise: Promise<MermaidApi> | null = null;

function loadMermaid(): Promise<MermaidApi> {
  mermaidPromise ??= import("mermaid").then((m) => {
    const mermaid = m.default;
    mermaid.initialize({
      startOnLoad: false,
      // no script, no links, labels escaped
      securityLevel: "strict",
      theme: "neutral",
      fontFamily: DIAGRAM_FONT,
      htmlLabels: false,
      flowchart: { htmlLabels: false, curve: "basis" },
      themeVariables: { fontFamily: DIAGRAM_FONT, fontSize: "16px" },
    });
    return mermaid;
  });
  return mermaidPromise;
}

let renderSeq = 0;

/** SVG markup of a Mermaid diagram; rejects with the parser's message. */
export async function renderMermaidSvg(source: string): Promise<string> {
  const mermaid = await loadMermaid();
  const id = `werag-diagram-${Date.now().toString(36)}-${++renderSeq}`;
  try {
    // \" is never valid Mermaid: a diagram saved before the server cleaned
    // double-escaped quotes would otherwise fail to draw
    const { svg } = await mermaid.render(id, source.replace(/\\"/g, '"'));
    return svg;
  } finally {
    // mermaid leaves a scratch node behind when it fails
    if (typeof document !== "undefined") document.getElementById(`d${id}`)?.remove();
  }
}

/** Intrinsic size of a rendered SVG (viewBox, else width/height). */
export function svgSize(svg: string): { width: number; height: number } {
  const doc = new DOMParser().parseFromString(svg, "image/svg+xml");
  const root = doc.documentElement;
  const vb = (root.getAttribute("viewBox") || "").trim().split(/[\s,]+/).map(Number);
  if (vb.length === 4 && vb[2] > 0 && vb[3] > 0) return { width: vb[2], height: vb[3] };
  const w = parseFloat(root.getAttribute("width") || "");
  const h = parseFloat(root.getAttribute("height") || "");
  return { width: w > 0 ? w : 800, height: h > 0 ? h : 600 };
}

export type RasterImage = { dataUrl: string; width: number; height: number };

/** PNG of an SVG at `scale`× its size on a white background (documents
 * are printed; a transparent PNG turns grey in some viewers). */
export async function svgToPng(svg: string, scale = 2): Promise<RasterImage> {
  const { width, height } = svgSize(svg);
  const doc = new DOMParser().parseFromString(svg, "image/svg+xml");
  const root = doc.documentElement;
  root.setAttribute("width", String(width));
  root.setAttribute("height", String(height));
  root.removeAttribute("style"); // mermaid sets max-width: …px
  const markup = new XMLSerializer().serializeToString(root);
  const url = URL.createObjectURL(new Blob([markup], { type: "image/svg+xml;charset=utf-8" }));
  try {
    const img = await loadImage(url);
    const canvas = document.createElement("canvas");
    canvas.width = Math.ceil(width * scale);
    canvas.height = Math.ceil(height * scale);
    const ctx = canvas.getContext("2d");
    if (!ctx) throw new Error("canvas unavailable");
    ctx.fillStyle = "#ffffff";
    ctx.fillRect(0, 0, canvas.width, canvas.height);
    ctx.drawImage(img, 0, 0, canvas.width, canvas.height);
    return { dataUrl: canvas.toDataURL("image/png"), width: canvas.width, height: canvas.height };
  } finally {
    URL.revokeObjectURL(url);
  }
}

/** A Blob (an artifact download) as a data URL plus its pixel size. */
export async function blobToRaster(blob: Blob): Promise<RasterImage> {
  const dataUrl = await new Promise<string>((resolve, reject) => {
    const r = new FileReader();
    r.onload = () => resolve(String(r.result));
    r.onerror = () => reject(r.error ?? new Error("read failed"));
    r.readAsDataURL(blob);
  });
  const img = await loadImage(dataUrl);
  return { dataUrl, width: img.naturalWidth, height: img.naturalHeight };
}

function loadImage(src: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const img = new Image();
    img.onload = () => resolve(img);
    img.onerror = () => reject(new Error("image failed to load"));
    img.src = src;
  });
}

/** Saves a data URL as a file. */
export function downloadDataUrl(dataUrl: string, fileName: string): void {
  const a = document.createElement("a");
  a.href = dataUrl;
  a.download = fileName;
  document.body.appendChild(a);
  a.click();
  a.remove();
}

/** A file name from a title: "Quy trình tiếp nhận" → "quy-trinh-tiep-nhan.png". */
export function illustrationFileName(title: string, ext = "png"): string {
  const slug = title
    .normalize("NFD")
    .replace(/[̀-ͯ]/g, "")
    .replace(/đ/g, "d")
    .replace(/Đ/g, "D")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 60);
  return `${slug || "hinh-minh-hoa"}.${ext}`;
}
