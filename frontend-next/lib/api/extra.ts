import { apiGet } from "@/lib/api-client";

export type SuggestedQuestion = { question: string };

/* Ported from frontend/src/api/agent/index.ts getSuggestedQuestions.
 * Vue resolves agentId from settingsStore.selectedAgentId; the Next shell has
 * no settings store yet, so callers may pass the agent explicitly. When
 * omitted we resolve the default agent list and use the first entry.
 */
export async function getSuggestedQuestions(
  agentId?: string,
  kbIds?: string[],
): Promise<SuggestedQuestion[]> {
  let id = agentId;
  if (!id) {
    const agents = await apiGet<{ success: boolean; data?: Array<{ id: string }> }>(`/api/v1/agents`);
    id = Array.isArray(agents.data) ? agents.data[0]?.id : undefined;
  }
  if (!id) return [];
  const qs = kbIds?.length ? `?knowledge_base_ids=${encodeURIComponent(kbIds.join(","))}` : "";
  const res = await apiGet<{ success: boolean; data?: { questions?: SuggestedQuestion[] } }>(
    `/api/v1/agents/${id}/suggested-questions${qs}`,
  );
  return res.data?.questions ?? [];
}
