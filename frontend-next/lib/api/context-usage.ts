/* GET /sessions/:id/context-usage — the chat's context indicator. Another
 * user's session answers 404. */
import { apiGet } from "@/lib/api-client";
import { parseContextUsage, type ContextUsage } from "@/lib/context-usage";

export async function getContextUsage(sessionId: string): Promise<ContextUsage | null> {
  const res = await apiGet<{ success?: boolean; data?: unknown }>(
    `/api/v1/sessions/${encodeURIComponent(sessionId)}/context-usage`,
  );
  return parseContextUsage(res?.data);
}
