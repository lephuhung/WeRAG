import { get, post, put, del } from '@/utils/request'
import type { TenantRole } from './members'

// TenantOrg mirrors internal/types/tenant_org.go — a user group inside
// one tenant (NOT the cross-tenant Organization used for KB sharing).
// Org-scoped knowledge bases are readable only by members of the bound
// org plus tenant Admin/Owner and system admins.
export interface TenantOrg {
  id: number
  tenant_id: number
  name: string
  description?: string
  created_by: string
  member_count?: number
  created_at: string
}

// TenantOrgRole is the role a user holds inside an org.
export type TenantOrgRole = 'manager' | 'member'

export interface TenantOrgMember {
  org_id: number
  user_id: string
  username?: string
  email?: string
  role: TenantOrgRole
  created_at: string
}

export interface TenantOrgInvitation {
  id: number
  tenant_id: number
  org_id: number
  role: TenantRole
  expires_at: string
}

interface ApiResponse<T> {
  success: boolean
  data?: T
  message?: string
}

export function listOrgs() {
  return get<ApiResponse<TenantOrg[]>>(`/api/v1/orgs`)
}

export function createOrg(data: { name: string; description?: string }) {
  return post<ApiResponse<TenantOrg>>(`/api/v1/orgs`, data)
}

export function updateOrg(id: number, data: { name?: string; description?: string }) {
  return put<ApiResponse<TenantOrg>>(`/api/v1/orgs/${id}`, data)
}

export function deleteOrg(id: number) {
  return del<ApiResponse<null>>(`/api/v1/orgs/${id}`)
}

export function listOrgMembers(orgId: number) {
  return get<ApiResponse<TenantOrgMember[]>>(`/api/v1/orgs/${orgId}/members`)
}

export function addOrgMember(orgId: number, data: { user_id: string; role: TenantOrgRole }) {
  return post<ApiResponse<null>>(`/api/v1/orgs/${orgId}/members`, data)
}

export function updateOrgMemberRole(orgId: number, userId: string, data: { role: TenantOrgRole }) {
  return put<ApiResponse<null>>(`/api/v1/orgs/${orgId}/members/${userId}`, data)
}

export function removeOrgMember(orgId: number, userId: string) {
  return del<ApiResponse<null>>(`/api/v1/orgs/${orgId}/members/${userId}`)
}

// createOrgInviteLink mints a multi-use registration link bound to the
// org: accepting it registers the account, joins the tenant with the
// given role, and enrols the user into the org.
export function createOrgInviteLink(orgId: number, data: { role: TenantRole; message?: string }) {
  return post<ApiResponse<{ invitation: TenantOrgInvitation; url: string }>>(
    `/api/v1/orgs/${orgId}/invite-links`, data)
}
