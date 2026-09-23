import type { SuggestedQuestion } from "@/lib/api/agents";

// The knowledge source a suggested question was generated from. Sent with the
// picked question so the agent searches that source before answering; the
// backend treats it as a hint inside the turn's search targets.
export interface QuestionOrigin {
  knowledge_base_id: string;
  knowledge_id?: string;
}

// Agent-authored starters have no knowledge source and yield undefined.
export function questionOriginFromSuggestion(
  item: Pick<SuggestedQuestion, "knowledge_base_id" | "knowledge_id"> | null | undefined,
): QuestionOrigin | undefined {
  const knowledgeBaseId = item?.knowledge_base_id?.trim();
  if (!item || !knowledgeBaseId) {
    return undefined;
  }
  const origin: QuestionOrigin = { knowledge_base_id: knowledgeBaseId };
  const knowledgeId = item?.knowledge_id?.trim();
  if (knowledgeId) {
    origin.knowledge_id = knowledgeId;
  }
  return origin;
}
