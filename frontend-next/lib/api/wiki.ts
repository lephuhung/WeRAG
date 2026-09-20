/* Ported from frontend/src/api/wiki/index.ts. */
import { apiDel, apiGet, apiPost, apiPut } from "@/lib/api-client";

/* encodeSlugPath encodes each segment of a hierarchical wiki slug (e.g.
 * "foo/bar baz?") so the URL is safe while preserving "/" separators —
 * encodeURIComponent on the whole slug would also escape "/" and break
 * hierarchical routing on the backend. */
function encodeSlugPath(slug: string): string {
  return slug.split("/").map(encodeURIComponent).join("/");
}

export interface WikiPage {
  id: string;
  tenant_id: number;
  knowledge_base_id: string;
  slug: string;
  title: string;
  page_type: string;
  status: string;
  content: string;
  summary: string;
  aliases: string[];
  parent_slug?: string;
  category_path?: string[];
  wiki_path?: string;
  depth?: number;
  sort_order?: number;
  source_refs: string[];
  in_links: string[];
  out_links: string[];
  page_metadata: Record<string, unknown>;
  version: number;
  // Author kind of the current version: 'pipeline' | 'agent' | 'user' |
  // 'revert'. Empty/missing on legacy rows (treat as 'pipeline').
  last_edit_source?: string;
  last_editor_id?: string;
  created_at: string;
  updated_at: string;
}

export interface WikiPageListResponse {
  pages: WikiPage[];
  total: number;
  page: number;
  page_size: number;
  total_pages: number;
}

export interface WikiFolder {
  id: string;
  tenant_id: number;
  knowledge_base_id: string;
  parent_id: string;
  name: string;
  path: string;
  depth: number;
  sort_order: number;
  created_at: string;
  updated_at: string;
}

export interface WikiFolderNode extends WikiFolder {
  page_count: number;
  has_children: boolean;
}

export interface WikiFolderListResponse {
  parent_id: string;
  folders: WikiFolderNode[];
}

export interface WikiGraphMeta {
  mode: "overview" | "ego" | string;
  total: number;
  returned: number;
  truncated: boolean;
  center?: string;
  depth?: number;
  familiar_count?: number;
}

export interface WikiGraphData {
  nodes: {
    slug: string;
    title: string;
    page_type: string;
    link_count: number;
    familiar?: boolean;
  }[];
  edges: { source: string; target: string }[];
  meta: WikiGraphMeta;
}

export interface WikiStats {
  total_pages: number;
  pages_by_type: Record<string, number>;
  total_links: number;
  orphan_count: number;
  recent_updates: WikiPage[];
  pending_tasks: number;
  pending_issues: number;
  is_active: boolean;
}

export interface WikiPageIssue {
  id: string;
  tenant_id: number;
  knowledge_base_id: string;
  slug: string;
  issue_type: string;
  description: string;
  suspected_knowledge_ids: string[];
  status: string;
  reported_by: string;
  created_at: string;
  updated_at: string;
}

export function listWikiPages(
  kbId: string,
  params?: {
    page_type?: string;
    status?: string;
    query?: string;
    category_path?: string;
    category_depth?: number;
    page?: number;
    page_size?: number;
    sort_by?: string;
    sort_order?: string;
  },
) {
  const query = new URLSearchParams();
  if (params) {
    Object.entries(params).forEach(([key, value]) => {
      if (value !== undefined && value !== "") {
        query.set(key, String(value));
      }
    });
  }
  const qs = query.toString();
  return apiGet(`/api/v1/knowledgebase/${kbId}/wiki/pages${qs ? "?" + qs : ""}`);
}

/* Returns direct child folders of parentId ("" = root), each enriched with a
 * recursive page_count and has_children flag so the tree can render expand
 * affordances and empty folders without a second request. pageTypes scopes
 * the view to a sidebar tab. */
export function listWikiFolders(kbId: string, parentId = "", pageTypes = "") {
  const query = new URLSearchParams();
  if (parentId) query.set("parent_id", parentId);
  if (pageTypes) query.set("page_types", pageTypes);
  const qs = query.toString();
  return apiGet(`/api/v1/knowledgebase/${kbId}/wiki/folders${qs ? "?" + qs : ""}`);
}

export function createWikiFolder(kbId: string, parentId: string, name: string) {
  return apiPost(`/api/v1/knowledgebase/${kbId}/wiki/folders`, {
    parent_id: parentId,
    name,
  });
}

/* Rename and/or reparent a folder. Pass move_parent: true (and parent_id)
 * to reparent; omit it for a pure rename. */
export function updateWikiFolder(
  kbId: string,
  folderId: string,
  data: { name?: string; parent_id?: string; move_parent?: boolean },
) {
  return apiPut(`/api/v1/knowledgebase/${kbId}/wiki/folders/${folderId}`, data);
}

/** Removes an empty folder (no pages, no sub-folders). */
export function deleteWikiFolder(kbId: string, folderId: string) {
  return apiDel(`/api/v1/knowledgebase/${kbId}/wiki/folders/${folderId}`);
}

/* Relocates a page into folderId ("" = root). The slug travels in the body
 * because wiki slugs are hierarchical. */
export function moveWikiPage(kbId: string, slug: string, folderId: string) {
  return apiPut(`/api/v1/knowledgebase/${kbId}/wiki/move-page`, {
    slug,
    folder_id: folderId,
  });
}

export function createWikiPage(kbId: string, data: Partial<WikiPage>) {
  return apiPost(`/api/v1/knowledgebase/${kbId}/wiki/pages`, data);
}

export function getWikiPage(kbId: string, slug: string) {
  return apiGet<{ success: boolean; data?: WikiPage; message?: string }>(
    `/api/v1/knowledgebase/${kbId}/wiki/pages/${encodeSlugPath(slug)}`,
  );
}

/* Partial update: absent fields keep their stored value. `version` is the
 * optimistic-lock guard — send the version the page had when the user started
 * editing; the backend answers 409 (with `current_version`) on a conflict. */
export interface WikiPageUpdatePayload {
  title?: string;
  content?: string;
  summary?: string;
  page_type?: string;
  status?: string;
  aliases?: string[];
  version?: number;
}
export function updateWikiPage(kbId: string, slug: string, data: WikiPageUpdatePayload) {
  return apiPut<{ success: boolean; data?: WikiPage; message?: string }>(
    `/api/v1/knowledgebase/${kbId}/wiki/pages/${encodeSlugPath(slug)}`,
    data,
  );
}

export interface WikiPageRevision {
  id: string;
  tenant_id: number;
  knowledge_base_id: string;
  page_id: string;
  slug: string;
  version: number;
  title: string;
  page_type: string;
  status: string;
  content?: string;
  summary: string;
  aliases: string[];
  edit_source: string;
  editor_id: string;
  edited_at: string;
  created_at: string;
}

export interface WikiRevisionListResponse {
  revisions: WikiPageRevision[];
  total: number;
  current_version: number;
}

/* Historical snapshots newest-first (content omitted) plus the current
 * version number. The current version has no revision row — it is the page. */
export function listWikiRevisions(
  kbId: string,
  slug: string,
  params?: { limit?: number; offset?: number },
) {
  const query = new URLSearchParams();
  if (params?.limit !== undefined) query.set("limit", String(params.limit));
  if (params?.offset !== undefined) query.set("offset", String(params.offset));
  const qs = query.toString();
  return apiGet<{ success: boolean; data?: WikiRevisionListResponse; message?: string }>(
    `/api/v1/knowledgebase/${kbId}/wiki/revisions/${encodeSlugPath(slug)}${qs ? "?" + qs : ""}`,
  );
}

export function getWikiRevision(kbId: string, slug: string, version: number) {
  return apiGet(
    `/api/v1/knowledgebase/${kbId}/wiki/revisions/${encodeSlugPath(slug)}?version=${version}`,
  );
}

/* Rolls the page back to a stored revision. Applied as a regular edit: the
 * pre-revert state is snapshotted and version advances, so a revert is
 * itself revertable. */
export function revertWikiPage(kbId: string, slug: string, version: number) {
  return apiPost(`/api/v1/knowledgebase/${kbId}/wiki/revert`, { slug, version });
}

export interface WikiIndexEntryDTO {
  slug: string;
  title: string;
  summary: string;
  parent_slug?: string;
  category_path?: string[];
  wiki_path?: string;
  depth?: number;
  sort_order?: number;
}

export interface WikiIndexGroup {
  type: string;
  total: number;
  items: WikiIndexEntryDTO[];
  next_cursor?: string;
}

export interface WikiIndexResponse {
  intro: string;
  version: number;
  groups: WikiIndexGroup[];
}

/* Structured index view — { intro, groups } replaced the legacy markdown
 * blob so a 40k-page wiki no longer round-trips megabytes per open. `types`
 * restricts page_type buckets; `limit` bounds the per-group window; `cursor`
 * resumes from a previous response. */
export function getWikiIndex(
  kbId: string,
  params?: { types?: string[]; limit?: number; cursor?: string },
) {
  const query = new URLSearchParams();
  if (params) {
    if (params.types && params.types.length > 0) query.set("types", params.types.join(","));
    if (params.limit !== undefined) query.set("limit", String(params.limit));
    if (params.cursor) query.set("cursor", params.cursor);
  }
  const qs = query.toString();
  return apiGet(`/api/v1/knowledgebase/${kbId}/wiki/index${qs ? "?" + qs : ""}`);
}

export interface WikiGraphQueryParams {
  mode?: "overview" | "ego";
  center?: string;
  depth?: number;
  types?: string[];
  limit?: number;
}

/* A slice of the wiki link graph. Without params the backend returns the
 * top-500 most-connected pages (overview); `mode: 'ego', center: <slug>`
 * drills into one page's neighborhood. The overview cap is what prevents
 * the browser from choking on a 30MB payload / 100k SVG nodes. */
export function getWikiGraph(kbId: string, params?: WikiGraphQueryParams) {
  const query = new URLSearchParams();
  if (params) {
    if (params.mode) query.set("mode", params.mode);
    if (params.center) query.set("center", params.center);
    if (params.depth !== undefined) query.set("depth", String(params.depth));
    if (params.limit !== undefined) query.set("limit", String(params.limit));
    if (params.types && params.types.length > 0) {
      query.set("types", params.types.join(","));
    }
  }
  const qs = query.toString();
  return apiGet(`/api/v1/knowledgebase/${kbId}/wiki/graph${qs ? "?" + qs : ""}`);
}

export function getWikiStats(kbId: string) {
  return apiGet(`/api/v1/knowledgebase/${kbId}/wiki/stats`);
}

export function searchWikiPages(kbId: string, q: string, limit?: number) {
  const params = new URLSearchParams({ q });
  if (limit) params.set("limit", String(limit));
  return apiGet(`/api/v1/knowledgebase/${kbId}/wiki/search?${params.toString()}`);
}

export function listWikiIssues(kbId: string, slug?: string, status?: string) {
  const params = new URLSearchParams();
  if (slug) params.set("slug", slug);
  if (status) params.set("status", status);
  return apiGet(`/api/v1/knowledgebase/${kbId}/wiki/issues?${params.toString()}`);
}

export function updateWikiIssueStatus(kbId: string, issueId: string, status: string) {
  return apiPut(`/api/v1/knowledgebase/${kbId}/wiki/issues/${issueId}/status`, { status });
}

export function rebuildWikiLinks(kbId: string) {
  return apiPost(`/api/v1/knowledgebase/${kbId}/wiki/rebuild-links`, {});
}
