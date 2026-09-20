/* Ported from frontend/src/utils/files.ts getFileIcon + artifactFileIcon.ts
 * + the kind-color table in artifact-file-icon.less. Renders the folded-sheet
 * SVG with an ext label banner — same geometry the Vue app uses for file
 * rows and artifact cards. */
export type FileKind =
  | "file-pdf"
  | "file-word"
  | "file-excel"
  | "file-powerpoint"
  | "image"
  | "video"
  | "code"
  | "sound"
  | "file"
  | "edit"
  | "link";

export function getFileKind(input: string | { type?: string; file_type?: string; file_name?: string }): FileKind {
  let type: string | undefined;
  let ext: string | undefined;
  if (typeof input === "string") {
    ext = input.split(".").pop()?.toLowerCase();
  } else {
    type = input.type;
    if (type === "manual") return "edit";
    if (type === "url") return "link";
    ext = (input.file_type ?? input.file_name?.split(".").pop() ?? "").toLowerCase();
  }
  if (!ext) return "file";
  if (["pdf"].includes(ext)) return "file-pdf";
  if (["doc", "docx"].includes(ext)) return "file-word";
  if (["xls", "xlsx", "csv"].includes(ext)) return "file-excel";
  if (["ppt", "pptx"].includes(ext)) return "file-powerpoint";
  if (["png", "jpg", "jpeg", "gif", "webp", "bmp", "svg"].includes(ext)) return "image";
  if (["mp4", "mov", "webm", "mkv", "avi"].includes(ext)) return "video";
  if (["mp3", "wav", "m4a", "flac", "ogg", "aac"].includes(ext)) return "sound";
  if (
    ["py", "pyc", "js", "mjs", "cjs", "ts", "tsx", "jsx", "go", "rs", "java", "c", "cc", "cpp", "h", "hpp", "sh", "bash", "rb", "php", "sql", "html", "htm"].includes(ext)
  ) {
    return "code";
  }
  return "file";
}

const KIND_COLOR: Record<FileKind, string> = {
  "file-pdf": "#c56868",
  "file-word": "#5381c4",
  "file-excel": "#419781",
  "file-powerpoint": "#c18a4e",
  image: "#9273bd",
  video: "#9273bd",
  code: "#5d95ad",
  sound: "#5d95ad",
  file: "#777169",
  edit: "#5d95ad",
  link: "#5d95ad",
};

/** Same SVG geometry as the Vue artifact-file-icon, colored inline. */
export function renderFileIconSvg(fileName: string, fileType?: string, docType?: string): string {
  const ext = fileName.split(".").pop()?.toLowerCase() ?? "";
  const kind = getFileKind(fileName);
  const badge = /^[a-z0-9]{1,4}$/.test(ext) ? ext.toUpperCase() : "FILE";
  // doc_type from the model-written profile (e.g. "user manual") replaces the
  // extension label on the banner when available.
  const label = docType
    ? docType.slice(0, 6).toUpperCase()
    : badge;
  const color = KIND_COLOR[kind];
  return (
    `<svg viewBox="0 0 32 38" fill="none" aria-hidden="true" style="width:100%;height:100%">` +
    `<path d="M6 1.5h13L28.5 11v22A3.5 3.5 0 0 1 25 36.5H6A3.5 3.5 0 0 1 2.5 33V5A3.5 3.5 0 0 1 6 1.5Z" fill="#fff" stroke="${color}" stroke-opacity="0.35" stroke-width="1.2"/>` +
    `<path d="M19 1.5V8a3 3 0 0 0 3 3h6.5" stroke="${color}" stroke-opacity="0.35" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round"/>` +
    `<path d="M8 14h9M8 18h14" stroke="${color}" stroke-opacity="0.35" stroke-width="1.2" stroke-linecap="round"/>` +
    `<rect x="5" y="23" width="24" height="11" rx="3" fill="${color}"/>` +
    `<text x="17" y="30.8" text-anchor="middle" style="fill:#fff;font-family:ui-sans-serif,system-ui,sans-serif;font-size:7px;font-weight:650;letter-spacing:0.2px">${badge}</text></svg>`
  );
}
