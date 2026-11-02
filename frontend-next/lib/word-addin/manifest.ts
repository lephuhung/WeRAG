import { wordManifestTemplate } from "./manifest-template.ts";

function validateOrigin(value: string): string {
  const url = new URL(value);
  if (url.protocol !== "https:" || url.username || url.password || url.pathname !== "/" || url.search || url.hash) {
    throw new Error("Cần địa chỉ HTTPS của máy chủ WeRAG, không kèm đường dẫn.");
  }
  return url.origin;
}

export function renderWordManifest(origin: string): string {
  return wordManifestTemplate.replaceAll("{{WERAG_ORIGIN}}", validateOrigin(origin));
}

export function resolveWordAddinOrigin(configured: string | undefined, requestOrigin: string, development: boolean): string {
  if (configured?.trim()) return validateOrigin(configured.trim());
  const url = new URL(requestOrigin);
  if (development && ["localhost", "127.0.0.1", "[::1]"].includes(url.hostname)) return "https://localhost:3000";
  return validateOrigin(requestOrigin);
}
