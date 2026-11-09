// A turn the language model could not answer (backend types/turn_failure.go).
// The live SSE error event carries the code as data.error_code; a reloaded
// message carries it as `failure`. Both views show the same note.

/** Failure codes; keep in sync with types.TurnFailure* in Go. */
export const TURN_FAILURE_KEYS = {
  llm_unavailable: "chat.err.llmUnavailable",
  llm_unavailable_after_search: "chat.err.llmUnavailableAfterSearch",
  // the partial answer already ends with the interruption note
  llm_interrupted: null,
} as const;

export type TurnFailureCode = keyof typeof TURN_FAILURE_KEYS;

export function isTurnFailure(code: unknown): code is TurnFailureCode {
  return typeof code === "string" && Object.prototype.hasOwnProperty.call(TURN_FAILURE_KEYS, code);
}

/**
 * The bubble state for a failed turn: always an error (no rating, no
 * follow-ups); `errorText` only when the note is not already in the answer.
 * null for anything that is not a turn failure.
 */
export function turnFailureView<K extends string>(
  code: unknown,
  tr: (key: K) => string,
): { isError: true; errorText?: string } | null {
  if (!isTurnFailure(code)) return null;
  const key = TURN_FAILURE_KEYS[code];
  return key ? { isError: true, errorText: tr(key as K) } : { isError: true };
}
