// Platform-public catalog helpers (Task 5).
//
// Pure UI helpers only: grouping/dedupe of catalog rows, public-endpoint
// response normalization, and request payload builders. No backend
// authorization happens here.
//
// This module is intentionally free of the `@/` import alias so it can be
// unit-tested with plain node --test like the other lib/*.test.ts files.

import { isPublicKBRow, type KBViewerRow } from "./kb-capabilities.ts";

export interface CatalogGroups<T extends KBViewerRow> {
  workspace: T[];
  public: T[];
  invited: T[];
}

function normalizeId(value: unknown): string {
  if (typeof value === "number" || typeof value === "string") return String(value).trim();
  return "";
}

function ownerOf(row: KBViewerRow): string {
  const v = row.owner_tenant_id;
  if (v === null || v === undefined) return "";
  return String(v).trim();
}

/* Split mixed catalog rows into workspace / platform-public / invited
 * sections. Ownership comes from owner_tenant_id + visibility (never data
 * tenant_id): owner 0 + public visibility is platform-public; an explicit
 * owner equal to the active tenant (or a legacy ownerless row living in the
 * active data scope) is workspace; anything else reachable is invited.
 * Duplicate ids keep their first occurrence so overlapping sources never
 * render a row twice. */
export function groupCatalogRows<T extends KBViewerRow>(
  rows: readonly T[],
  activeTenantId: number | string | null | undefined,
): CatalogGroups<T> {
  const active = activeTenantId === null || activeTenantId === undefined ? "" : String(activeTenantId).trim();
  const groups: CatalogGroups<T> = { workspace: [], public: [], invited: [] };
  const seen = new Set<string>();
  for (const row of rows) {
    if (!row) continue;
    const id = normalizeId((row as { id?: unknown }).id);
    if (id === "" || seen.has(id)) continue;
    seen.add(id);
    if (isPublicKBRow(row)) {
      groups.public.push(row);
      continue;
    }
    const owner = ownerOf(row);
    if ((owner !== "" && active !== "" && owner === active) || owner === "") {
      groups.workspace.push(row);
    } else {
      groups.invited.push(row);
    }
  }
  return groups;
}

export interface PublicCatalogPage<T = KBViewerRow> {
  items: T[];
  total: number;
  page: number;
  pageSize: number;
}

function toFiniteNumber(value: unknown): number | null {
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

/* Normalize the dedicated public-catalog envelope
 * {success, data:{items,total,page,page_size}}. Malformed payloads degrade
 * to a valid empty page (page 1, default size) rather than throwing, so a
 * confused response never breaks the public section. */
export function normalizePublicCatalog<T = KBViewerRow>(res: unknown): PublicCatalogPage<T> {
  const fallback: PublicCatalogPage<T> = { items: [], total: 0, page: 1, pageSize: 50 };
  const r = res as {
    data?: unknown;
  } | null;
  if (!r || typeof r !== "object") return fallback;
  const data = r.data;
  if (!data || typeof data !== "object") return fallback;
  const d = data as { items?: unknown; total?: unknown; page?: unknown; page_size?: unknown };
  if (!Array.isArray(d.items)) return fallback;
  return {
    items: d.items as T[],
    total: toFiniteNumber(d.total) ?? 0,
    page: toFiniteNumber(d.page) && (d.page as number) > 0 ? (d.page as number) : 1,
    pageSize: toFiniteNumber(d.page_size) && (d.page_size as number) > 0 ? (d.page_size as number) : 50,
  };
}

export interface PublicSectionSource<T extends KBViewerRow> {
  items: readonly T[];
  /* True once the dedicated endpoint answered successfully (even empty).
   * False while loading or on error, when the main-list window is the
   * only public data available. */
  ok: boolean;
}

/* Choose the public section rows: after a successful endpoint response the
 * selected endpoint page is authoritative on its own (a page-2 render must
 * not re-append the mixed list's first-50 window the pager no longer
 * describes); while loading or on error, fall back to the main-list public
 * window. Dedupe by id within the chosen source. */
export function selectPublicSection<T extends KBViewerRow>(
  mainList: readonly T[],
  endpoint: PublicSectionSource<T>,
): T[] {
  const source = endpoint.ok ? endpoint.items : mainList;
  const out: T[] = [];
  const seen = new Set<string>();
  for (const row of source) {
    if (!row) continue;
    const id = normalizeId((row as { id?: unknown }).id);
    if (id === "" || seen.has(id)) continue;
    seen.add(id);
    out.push(row);
  }
  return out;
}

export interface PublicCreateInput {
  name: string;
  description?: string;
  type?: "document" | "faq";
  [key: string]: unknown;
}

/* Build the dedicated public-create payload. Only name/description/type
 * cross the wire: tenant storage backends, vector-store bindings, and
 * selected-tenant defaults must never leak into a platform create (the
 * service resolves platform defaults and rejects tenant-bound bindings). */
export function buildPublicCreatePayload(input: PublicCreateInput): {
  name: string;
  description?: string;
  type?: "document" | "faq";
} {
  const payload: { name: string; description?: string; type?: "document" | "faq" } = {
    name: String(input.name ?? "").trim(),
  };
  const description = typeof input.description === "string" ? input.description.trim() : "";
  if (description !== "") payload.description = description;
  if (input.type === "document" || input.type === "faq") payload.type = input.type;
  return payload;
}

export interface ScopeChangeInput {
  visibility: "public" | "tenant";
  target_tenant_id?: number;
}

/* Build the visibility-transition payload. Public→tenant requires an
 * existing nonzero destination tenant selected by the SuperAdmin — a
 * missing target is a caller error, never a silent default. */
export function buildScopeChangePayload(
  from: "public",
  _kbId: string,
  targetTenantId?: number | string | null,
): { visibility: "tenant"; target_tenant_id: number };
export function buildScopeChangePayload(
  from: "tenant",
  _kbId: string,
  targetTenantId?: number | string | null,
): { visibility: "public" };
export function buildScopeChangePayload(
  from: "public" | "tenant",
  _kbId: string,
  targetTenantId?: number | string | null,
): ScopeChangeInput {
  if (from === "public") {
    const target = typeof targetTenantId === "string" ? Number(targetTenantId.trim()) : targetTenantId;
    if (typeof target !== "number" || !Number.isFinite(target) || target <= 0) {
      throw new Error("Choose an existing destination workspace (target tenant id) before moving a public knowledge base.");
    }
    return { visibility: "tenant", target_tenant_id: Math.floor(target) };
  }
  return { visibility: "public" };
}
