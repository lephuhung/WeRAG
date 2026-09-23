/* Global Vietnamese abbreviation dictionary (AIRAG port). The list is shared
 * across workspaces: GET/POST are Member+ (POST creates an inactive
 * suggestion), while PATCH — edits and is_active flips — and DELETE are
 * workspace-owner only. The server enforces both gates; the UI only hides
 * controls. */
import { apiDel, apiGet, apiPatch, apiPost } from "@/lib/api-client";

export interface Abbreviation {
  id: string;
  short_form: string;
  full_form: string;
  description: string;
  is_active: boolean;
  suggested_by: string;
  created_at: string;
  updated_at: string;
}

export interface AbbreviationListResponse {
  success: boolean;
  data: Abbreviation[];
  total: number;
  page: number;
}

export function listAbbreviations(params: {
  search?: string;
  isActive?: boolean;
  page?: number;
  pageSize?: number;
}): Promise<AbbreviationListResponse> {
  const q = new URLSearchParams();
  if (params.search) q.set("search", params.search);
  if (params.isActive !== undefined) q.set("is_active", String(params.isActive));
  if (params.page) q.set("page", String(params.page));
  if (params.pageSize) q.set("page_size", String(params.pageSize));
  const qs = q.toString();
  return apiGet(`/api/v1/abbreviations${qs ? `?${qs}` : ""}`);
}

export function createAbbreviation(body: {
  short_form: string;
  full_form: string;
  description?: string;
}): Promise<{ success: boolean; data: Abbreviation }> {
  return apiPost("/api/v1/abbreviations", body);
}

export function updateAbbreviation(
  id: string,
  body: {
    short_form?: string;
    full_form?: string;
    description?: string;
    is_active?: boolean;
  },
): Promise<{ success: boolean; data: Abbreviation }> {
  return apiPatch(`/api/v1/abbreviations/${id}`, body);
}

export function deleteAbbreviation(id: string): Promise<{ success: boolean }> {
  return apiDel(`/api/v1/abbreviations/${id}`);
}
