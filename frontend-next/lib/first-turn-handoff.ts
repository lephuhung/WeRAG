import type { QuestionOrigin } from "./question-origin.ts";

/* B-2a(2): session-scoped handoff for the create-chat first turn.
 *
 * The create-chat page uploads the selected files/images AFTER the session
 * exists but BEFORE navigating, then hands the resulting attachment IDs
 * (opaque server handles — never raw file bytes) plus their display-only
 * file names to the chat page through a one-shot same-tab sessionStorage
 * record keyed by the new session id. The navigation URL carries only the
 * innocuous `q` / `qokb` / `qok` marker (which predates attachments) plus
 * the `fh=1` flag when a file handoff is expected — never IDs or names, so
 * nothing private leaks into history, logs, or shoulder-surfable URLs.
 *
 * The chat page consumes the record exactly once for its session id. When
 * the flag is present but the record is absent (reload, other tab, expired
 * storage) the chat page must NOT auto-send the bare question: it surfaces
 * an actionable error and keeps the question as a draft. An upload failure
 * rejects, and the caller keeps the user on the create-chat page with the
 * selection intact for retry (no silent drop). */

/* Innocuous one-bit marker: the first turn of this session expects a file
 * handoff in sessionStorage. Carries no IDs, no names — safe for the URL. */
export const FIRST_TURN_HANDOFF_FLAG_PARAM = "fh";
export const FIRST_TURN_HANDOFF_FLAG_VALUE = "1";

/* sessionStorage namespace for the one-shot handoff records. The key embeds
 * only the session id — never attachment IDs or file names. */
export const FIRST_TURN_HANDOFF_STORAGE_PREFIX = "werag:first-turn-handoff:";

export function firstTurnHandoffKey(sessionId: string): string {
  return `${FIRST_TURN_HANDOFF_STORAGE_PREFIX}${sessionId}`;
}

export interface FirstTurnHandoffPayload {
  attachmentIds: string[];
  attachmentNames: string[];
}

/* Minimal store surface so the logic unit-tests without a DOM (pass
 * window.sessionStorage in the browser). */
export type HandoffStore = Pick<Storage, "getItem" | "setItem" | "removeItem">;

/* Persist the handoff for `sessionId`. THROWS on storage failure — the
 * caller must fail visibly before navigating instead of dropping files. */
export function saveFirstTurnHandoff(
  store: HandoffStore,
  sessionId: string,
  payload: FirstTurnHandoffPayload,
): void {
  try {
    store.setItem(
      firstTurnHandoffKey(sessionId),
      JSON.stringify({
        attachmentIds: mergeAttachmentIds(payload.attachmentIds),
        attachmentNames: (payload.attachmentNames ?? []).map((n) => n.trim()).filter(Boolean),
      }),
    );
  } catch (e) {
    throw new Error(
      `Could not stage attachments for the new chat (${e instanceof Error ? e.message : "storage unavailable"}) — files kept for retry`,
    );
  }
}

/* One-shot take: returns the payload and deletes the record so a reload or a
 * second session can never replay it. A missing key, a key for another
 * session (callers only ask for their own id), or a corrupt record resolves
 * to null — and the corrupt record is cleared. */
export function consumeFirstTurnHandoff(
  store: HandoffStore,
  sessionId: string,
): FirstTurnHandoffPayload | null {
  const key = firstTurnHandoffKey(sessionId);
  let raw: string | null;
  try {
    raw = store.getItem(key);
  } catch {
    return null;
  }
  if (!raw) return null;
  try {
    store.removeItem(key);
  } catch {
    /* best-effort: the parsed payload below is still valid for this turn */
  }
  try {
    const parsed = JSON.parse(raw) as Partial<FirstTurnHandoffPayload>;
    // Validate nonempty ID strings: JSON-valid but malformed payloads
    // (attachmentIds: [], wrong shapes, blank strings) normalize to an
    // empty list here so resolveFirstTurnHandoff can fail closed when
    // fh=1 instead of treating empty corrupt storage as a valid handoff.
    const rawIds = Array.isArray(parsed.attachmentIds) ? parsed.attachmentIds : [];
    const validIds = rawIds.filter(
      (x): x is string => typeof x === "string" && x.trim().length > 0,
    );
    return {
      attachmentIds: mergeAttachmentIds(validIds),
      attachmentNames: (Array.isArray(parsed.attachmentNames) ? parsed.attachmentNames : [])
        .filter((x): x is string => typeof x === "string")
        .map((n) => n.trim())
        .filter(Boolean),
    };
  } catch {
    try {
      store.removeItem(key);
    } catch {
      /* ignore */
    }
    return null;
  }
}

/* Chat-route decision: combine the URL marker with the (already consumed)
 * storage handoff. Returns the question/origin/IDs plus, when the `fh` flag
 * is set but the handoff is absent, an actionable error — the caller must
 * keep the draft and NOT auto-send in that case. */
export interface ResolvedFirstTurn {
  query: string | null;
  questionOrigin?: QuestionOrigin;
  expectsAttachments: boolean;
  attachmentIds: string[];
  attachmentNames: string[];
  handoffError: string | null;
}

export function resolveFirstTurnHandoff(
  params: URLSearchParams,
  handoff: FirstTurnHandoffPayload | null,
): ResolvedFirstTurn {
  const query = params.get("q");
  const kbId = params.get("qokb")?.trim();
  const knowledgeId = params.get("qok")?.trim();
  const questionOrigin = kbId
    ? {
        knowledge_base_id: kbId,
        ...(knowledgeId ? { knowledge_id: knowledgeId } : {}),
      }
    : undefined;
  const expectsAttachments = params.get(FIRST_TURN_HANDOFF_FLAG_PARAM) === FIRST_TURN_HANDOFF_FLAG_VALUE;
  if (!query) {
    return {
      query: null,
      questionOrigin,
      expectsAttachments: false,
      attachmentIds: [],
      attachmentNames: [],
      handoffError: null,
    };
  }
  // Fail closed when fh=1 but the handoff is missing OR malformed:
  // a JSON-valid-but-empty record (attachmentIds: [], wrong shape,
  // all-blank IDs) normalizes to an empty list via consumeFirstTurnHandoff
  // and must NOT count as a valid handoff — auto-sending the bare question
  // without its files would silently drop the user's attachments.
  if (expectsAttachments && (!handoff || handoff.attachmentIds.length === 0)) {
    return {
      query,
      questionOrigin,
      expectsAttachments,
      attachmentIds: [],
      attachmentNames: [],
      handoffError:
        "The attached files could not be transferred to this chat (the tab was reloaded, or the link was opened in another tab) — the question was kept as a draft and was NOT sent. Please re-attach the files and send again.",
    };
  }
  return {
    query,
    questionOrigin,
    expectsAttachments,
    attachmentIds: handoff ? mergeAttachmentIds(handoff.attachmentIds) : [],
    attachmentNames: handoff
      ? (handoff.attachmentNames ?? []).map((n) => n.trim()).filter(Boolean)
      : [],
    handoffError: null,
  };
}

/* Mirrors Go types.MaxTemporaryAttachmentsPerMessage: a single QA turn may
 * reference at most this many temporary attachment IDs (files + images). */
export const MAX_TEMPORARY_ATTACHMENTS_PER_MESSAGE = 5;

/* Client-side combined-cap gate: runs BEFORE any upload so an over-cap send
 * never creates orphan sessions/uploads. Returns an error string to display
 * (caller keeps the selection for retry) or null when within cap. */
export function combinedCapError(total: number): string | null {
  if (total <= MAX_TEMPORARY_ATTACHMENTS_PER_MESSAGE) return null;
  return `A message can use at most ${MAX_TEMPORARY_ATTACHMENTS_PER_MESSAGE} attachments (files + images combined) — remove some files before sending.`;
}

/* Order-preserving dedupe/merge of attachment ID lists (pre-uploaded IDs +
 * freshly uploaded file IDs + image IDs + one-shot handoff IDs). */
export function mergeAttachmentIds(...lists: Array<string[] | undefined>): string[] {
  const out: string[] = [];
  const seen = new Set<string>();
  for (const list of lists) {
    for (const raw of list ?? []) {
      const id = raw.trim();
      if (!id || seen.has(id)) continue;
      seen.add(id);
      out.push(id);
    }
  }
  return out;
}

/* Session-keyed one-shot handoff state. The chat component may be reused for
 * a new `id` while the old consumed flags linger — keying by session ID
 * resets IDs, names, origin AND the consumed flag, so a second session gets
 * its own handoff and nothing leaks into later turns. */
export interface HandoffSessionState {
  sessionId: string;
  questionOrigin?: QuestionOrigin;
  attachmentIds: string[];
  attachmentNames: string[];
  consumed: boolean;
}

export function initHandoffSessionState(
  sessionId: string,
  parsed: { questionOrigin?: QuestionOrigin; attachmentIds?: string[]; attachmentNames?: string[] },
): HandoffSessionState {
  return {
    sessionId,
    questionOrigin: parsed.questionOrigin,
    attachmentIds: mergeAttachmentIds(parsed.attachmentIds),
    attachmentNames: (parsed.attachmentNames ?? []).map((n) => n.trim()).filter(Boolean),
    consumed: false,
  };
}

export function rotateHandoffSessionState(
  prev: HandoffSessionState | null,
  sessionId: string,
  parsed: { questionOrigin?: QuestionOrigin; attachmentIds?: string[]; attachmentNames?: string[] },
): HandoffSessionState {
  if (prev && prev.sessionId === sessionId) return prev;
  return initHandoffSessionState(sessionId, parsed);
}

/* One-shot take: the first send gets origin + IDs + display names; every
 * later send gets empties so stale hints/IDs never re-attach. */
export function takeHandoffTurn(state: HandoffSessionState): {
  questionOrigin?: QuestionOrigin;
  attachmentIds: string[];
  attachmentNames: string[];
  next: HandoffSessionState;
} {
  if (state.consumed) {
    return { questionOrigin: undefined, attachmentIds: [], attachmentNames: [], next: state };
  }
  return {
    questionOrigin: state.questionOrigin,
    attachmentIds: [...state.attachmentIds],
    attachmentNames: [...state.attachmentNames],
    next: { ...state, consumed: true },
  };
}

/* Suggestion-click send: a picked suggestion must carry the CURRENTLY
 * selected files/images (not empty lists) or the selection is silently
 * dropped. Generic over the attachment/file types so .tsx component types
 * flow through structurally (result feeds ComposerSend). */
export function buildSuggestionSend<A, F>(args: {
  query: string;
  questionOrigin?: QuestionOrigin;
  attachments: A[];
  imageFiles: F[];
}): {
  query: string;
  modelId: string;
  mentionedItems: never[];
  imageFiles: F[];
  attachments: A[];
  questionOrigin?: QuestionOrigin;
} {
  return {
    query: args.query,
    modelId: "",
    mentionedItems: [],
    imageFiles: args.imageFiles,
    attachments: args.attachments,
    questionOrigin: args.questionOrigin,
  };
}

export function buildFirstTurnSearch(args: {
  query: string;
  questionOrigin?: QuestionOrigin;
  /* True when a file handoff was staged in sessionStorage for the new
   * session — routes only the innocuous `fh=1` flag, never IDs or names. */
  hasAttachments?: boolean;
}): string {
  const qp = new URLSearchParams({ q: args.query });
  if (args.questionOrigin?.knowledge_base_id) {
    qp.set("qokb", args.questionOrigin.knowledge_base_id);
  }
  if (args.questionOrigin?.knowledge_id) {
    qp.set("qok", args.questionOrigin.knowledge_id);
  }
  if (args.hasAttachments) {
    qp.set(FIRST_TURN_HANDOFF_FLAG_PARAM, FIRST_TURN_HANDOFF_FLAG_VALUE);
  }
  return qp.toString();
}

export function parseFirstTurnSearch(params: URLSearchParams): {
  query: string | null;
  questionOrigin?: QuestionOrigin;
  expectsAttachments: boolean;
} {
  const query = params.get("q");
  const kbId = params.get("qokb")?.trim();
  const knowledgeId = params.get("qok")?.trim();
  return {
    query,
    ...(kbId
      ? {
          questionOrigin: {
            knowledge_base_id: kbId,
            ...(knowledgeId ? { knowledge_id: knowledgeId } : {}),
          },
        }
      : {}),
    expectsAttachments:
      params.get(FIRST_TURN_HANDOFF_FLAG_PARAM) === FIRST_TURN_HANDOFF_FLAG_VALUE,
  };
}

/* Upload every selected file/image against the fresh session, in order
 * (files first, then images). Resolves with the server attachment IDs.
 * Rejects naming the failed file after best-effort cleanup of the partial
 * successes, so the caller can stay on the page and retry with the
 * selection intact instead of navigating with a silently dropped subset. */
export async function uploadFirstTurnFiles(
  sessionId: string,
  files: File[],
  images: File[],
  deps: {
    upload: (sessionId: string, file: File) => Promise<{ id: string }>;
    remove?: (sessionId: string, attachmentId: string) => Promise<unknown>;
  },
): Promise<string[]> {
  const all = [...files, ...images];
  const ids: string[] = [];
  try {
    for (const file of all) {
      const up = await deps.upload(sessionId, file);
      ids.push(up.id);
    }
    return ids;
  } catch (e) {
    if (deps.remove) {
      await Promise.all(ids.map((id) => deps.remove!(sessionId, id).catch(() => undefined)));
    }
    const failedName = all[ids.length]?.name ?? "file";
    throw new Error(
      `Failed to upload “${failedName}” (${e instanceof Error ? e.message : "upload failed"}) — files kept for retry`,
    );
  }
}

/* Best-effort orphan cleanup: when the first-turn upload (or handoff
 * staging) fails, the freshly created session would otherwise linger empty.
 * Delete it via the existing delete-session API — a cleanup failure never
 * masks the original upload error, so it always resolves. */
export async function cleanupSessionBestEffort(
  remove: (sessionId: string) => Promise<unknown>,
  sessionId: string,
): Promise<void> {
  try {
    await remove(sessionId);
  } catch {
    /* orphan cleanup must not mask the upload error */
  }
}
