/* Ported from frontend/src/api/user-favorites.ts.
 * Favorites are scoped server-side to the (user, tenant) auth pair — these
 * helpers never pass user_id/tenant_id, and a tenant switch automatically
 * points reads at the right namespace. */
import { apiDel, apiGet, apiPost } from "@/lib/api-client";

export type FavoriteResourceType = "kb" | "agent";

export interface FavoriteEntry {
  user_id: string;
  tenant_id: number;
  resource_type: FavoriteResourceType;
  resource_id: string;
  /** ISO timestamp from the server (created_at column). */
  created_at: string;
}

export function listFavorites(type: FavoriteResourceType) {
  return apiGet<{ success: boolean; data: FavoriteEntry[] }>(
    `/api/v1/user/favorites?type=${encodeURIComponent(type)}`,
  );
}

export function addFavorite(type: FavoriteResourceType, id: string) {
  return apiPost("/api/v1/user/favorites", { type, id });
}

export function removeFavorite(type: FavoriteResourceType, id: string) {
  return apiDel(
    `/api/v1/user/favorites/${encodeURIComponent(type)}/${encodeURIComponent(id)}`,
  );
}
