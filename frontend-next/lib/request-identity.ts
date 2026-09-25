// Identity guard for late async callbacks after a document/KB switch.
//
// Problem: a fetch started for document/KB A can resolve after the user
// has switched to B. The response must never write A's data into B's
// header/tab/detail state — neither on success nor on failure (a stale
// catch that clears state would wipe B's freshly loaded state).
//
// Contract: capture the owner's identity ({ id, gen }) when the request
// starts and call shouldApplyResponse before touching any state in both
// the success and the catch branches. Discarded responses do nothing.

export interface OwnedRequest {
  id: string;
  gen: number;
}

export function shouldApplyResponse(
  owner: OwnedRequest,
  current: OwnedRequest,
): boolean {
  return owner.id === current.id && owner.gen === current.gen;
}
