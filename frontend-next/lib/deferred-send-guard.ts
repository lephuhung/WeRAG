/* Phase2a deferred-send liveness gate (pure, framework-free).
 *
 * chat-client send() awaits createSession / image uploads / local file
 * uploads BEFORE appending its optimistic bubble and launching streamChat.
 * When the route id changes (A→B) while such an upload is pending, the
 * session-change effect bumps the generation — the stale A turn must then
 * drop its commit phase entirely (no bubble, no abort-controller install,
 * no stream, no error surfacing) so it never touches B state. Every await
 * in the upload/createSession path re-checks this gate via shouldCommit…
 * before each state write. Unit-tests without React. */
import type { StreamGeneration } from "./stream-generation.ts";

/* True only when `gen` is still the live generation AND the route id has
 * not moved since the turn started. Both halves are required: a resend on
 * the same route bumps the generation, and a route switch changes the id. */
export function shouldCommitDeferredSendTurn(
  generations: StreamGeneration,
  gen: number,
  liveId: string,
  originId: string,
): boolean {
  return generations.isCurrent(gen) && liveId === originId;
}
