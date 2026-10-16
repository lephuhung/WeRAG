/* The current workspace's issuing agency (hồ sơ đơn vị): the header and
 * signature data of documents drafted from chat. Every member reads it;
 * the workspace's admin or a SuperAdmin saves it. `version` is the version
 * the editor read (0: none saved yet) — a newer save answers 409. */
import { apiGet, apiPut } from "@/lib/api-client";

export interface AgencyProfile {
  tenant_id?: number;
  parent_agency: string;
  agency_name: string;
  agency_abbr: string;
  locality: string;
  head_title: string;
  collective_name: string;
  default_recipients: string[];
  version: number;
  updated_at?: string;
}

export function getAgencyProfile(): Promise<{ success: boolean; data: AgencyProfile | null }> {
  return apiGet("/api/v1/tenant/agency-profile");
}

export function updateAgencyProfile(p: AgencyProfile): Promise<{ success: boolean; data: AgencyProfile }> {
  return apiPut("/api/v1/tenant/agency-profile", p);
}
