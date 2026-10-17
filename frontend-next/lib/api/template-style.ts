/* Font sizes and page margins of the NĐ30 templates drafted from chat: the platform default
 * (system setting, SuperAdmin) and the current workspace's override (its
 * admin). Every member reads them; the server checks the NĐ30 ranges. */
import { apiGet, apiPut } from "@/lib/api-client";
import { updateSystemSetting } from "@/lib/api/system";

export const TEMPLATE_FONT_SIZES_KEY = "document_template.font_sizes";

export type TemplateFontSizes = Record<string, number>;

export interface TemplateStyle {
  /** unit: "pt" for a font size, "mm" for a page margin */
  components: { key: string; unit: "pt" | "mm"; min: number; max: number; default: number }[];
  platform: TemplateFontSizes;
  tenant: TemplateFontSizes;
  effective: TemplateFontSizes;
}

export function getTemplateStyle(): Promise<{ success: boolean; data: TemplateStyle }> {
  return apiGet("/api/v1/tenant/document-template-style");
}

export function updateTenantTemplateStyle(fontSizes: TemplateFontSizes): Promise<{ success: boolean; data: TemplateStyle }> {
  return apiPut("/api/v1/tenant/document-template-style", { font_sizes: fontSizes });
}

export function updatePlatformTemplateStyle(fontSizes: TemplateFontSizes) {
  return updateSystemSetting(TEMPLATE_FONT_SIZES_KEY, fontSizes);
}
