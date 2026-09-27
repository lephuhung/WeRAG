export interface ParserEngineInfo {
  Name: string
  Description: string
  FileTypes: string[]
  Available?: boolean
  UnavailableReason?: string
}

/**
 * Static parser-engine catalog (file-type topology only, no availability).
 * Mirrors the Go local registry (`internal/infrastructure/docparser/engines.go`
 * `init()` + docreader-connected steady state) so the UI can render a fixed
 * layout instantly. `GET /api/v1/system/parser-engines` only refreshes the
 * `Available` / `UnavailableReason` flags afterwards; remote-only engines
 * (e.g. vietnamese_legal, openai_ocr, future plugins) are appended when
 * the backend reports them. `Available` left undefined = assumed available
 * until the backend resolves.
 *
 * Leaf module: no `@/` imports so unit tests can load it under plain tsx.
 */
export const STATIC_PARSER_ENGINES: ParserEngineInfo[] = [
  { Name: 'builtin', Description: 'DocReader built-in parser engine', FileTypes: ['docx', 'doc', 'pdf', 'md', 'markdown', 'xlsx', 'xls', 'pptx', 'ppt', 'epub', 'html', 'htm', 'mhtml', 'xmind', 'jpg', 'jpeg', 'png', 'gif', 'bmp', 'tiff', 'webp'] },
  { Name: 'simple', Description: 'Simple format & image parsing (no external service required)', FileTypes: ['md', 'markdown', 'txt', 'csv', 'json', 'jpg', 'jpeg', 'png', 'gif', 'bmp', 'tiff', 'webp', 'mp3', 'wav', 'm4a', 'flac', 'ogg'] },
  { Name: 'anydoc', Description: 'anydoc in-process office document converter (no external service required)', FileTypes: ['csv', 'doc', 'docm', 'docx', 'epub', 'odp', 'ods', 'odt', 'pdf', 'ppt', 'pptm', 'pptx', 'rtf', 'xls', 'xlsm', 'xlsx'] },
  { Name: 'weknoracloud', Description: 'WeKnoraCloud document reader', FileTypes: ['docx', 'doc', 'pdf', 'md', 'markdown', 'xlsx', 'xls', 'pptx', 'ppt'] },
  { Name: 'mineru', Description: 'MinerU self-hosted service', FileTypes: ['pdf', 'jpg', 'jpeg', 'png', 'bmp', 'tiff', 'doc', 'docx', 'ppt', 'pptx'] },
  { Name: 'mineru_cloud', Description: 'MinerU Cloud API', FileTypes: ['pdf', 'jpg', 'jpeg', 'png', 'bmp', 'tiff', 'doc', 'docx', 'ppt', 'pptx'] },
  { Name: 'paddleocr_vl_cloud', Description: 'PaddleOCR-VL Cloud API', FileTypes: ['pdf', 'jpg', 'jpeg', 'png', 'bmp', 'tiff'] },
  { Name: 'markitdown', Description: 'MarkItDown converter (Microsoft MarkItDown library)', FileTypes: ['md', 'markdown', 'pdf', 'docx', 'doc', 'pptx', 'ppt', 'xlsx', 'xls', 'csv'] },
  { Name: 'opendataloader', Description: 'OpenDataLoader PDF (layout analysis, requires Java 11+)', FileTypes: ['pdf'] },
]

/**
 * Merge backend availability flags onto the static catalog without touching
 * the file-type topology (keeps the settings layout fixed). Engines unknown
 * to the static list (new remote plugins) are appended as-is.
 */
export function mergeParserEngineAvailability(
  base: ParserEngineInfo[],
  remote: ParserEngineInfo[] | null | undefined,
): ParserEngineInfo[] {
  if (!remote || !Array.isArray(remote) || remote.length === 0) return base
  const flags: Record<string, ParserEngineInfo> = {}
  for (const e of remote) flags[e.Name] = e
  const merged = base.map((e) => {
    const r = flags[e.Name]
    if (!r) return e
    return { ...e, Available: r.Available, UnavailableReason: r.UnavailableReason }
  })
  for (const r of remote) {
    if (base.some((e) => e.Name === r.Name)) continue
    merged.push(r)
  }
  return merged
}
