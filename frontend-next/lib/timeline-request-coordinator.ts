// Request coordination for ProcessingTimeline span fetches.
//
// Problem: switching attempts (or the knowledge ID) while an older spans
// request is still in flight could (a) drop the newer fetch entirely — the
// old `fetchInFlight` guard returned early and the poll tick only retries
// every 2s — and (b) let the late old payload overwrite the newest
// selection's data/summary/status/loading. A stale terminal payload could
// additionally freeze polling for a selection that is actually still live.
//
// Contract: exactly one spans request is ever in flight per timeline. A
// fetch requested while another is in flight is remembered as the single
// pending selection (newest wins); when the in-flight ticket settles, the
// payload is accepted only if it is still the newest request for the
// current selection, and any pending selection is returned so the caller
// fires it immediately instead of waiting for the next poll tick. Settle
// always releases the in-flight slot, so a late resolution can never
// deadlock newer fetches.

export interface TimelineSelection {
  knowledgeId: string;
  attempt: number | undefined;
}

export interface TimelineTicket {
  seq: number;
  selection: TimelineSelection;
}

export interface TimelineSettleResult {
  /** True only when this ticket is still the newest request for the
   * current selection — the only case where the caller may mutate
   * data/summary/status/loading. */
  accept: boolean;
  /** Newest selection that still needs fetching, or null when caught up. */
  retry: TimelineSelection | null;
}

function sameSelection(a: TimelineSelection, b: TimelineSelection): boolean {
  return a.knowledgeId === b.knowledgeId && a.attempt === b.attempt;
}

export class TimelineRequestCoordinator {
  private seq = 0;
  private current: TimelineSelection;
  private inFlight: TimelineTicket | null = null;
  private pending: TimelineSelection | null = null;

  constructor(initial: TimelineSelection) {
    this.current = { ...initial };
  }

  updateSelection(sel: TimelineSelection): void {
    this.current = { ...sel };
  }

  getCurrent(): TimelineSelection {
    return { ...this.current };
  }

  getLatestSeq(): number {
    return this.seq;
  }

  isInFlight(): boolean {
    return this.inFlight !== null;
  }

  hasPending(): boolean {
    return this.pending !== null;
  }

  /** Document switch: forget in-flight/pending work for the old document.
   * A late response for a pre-reset ticket settles with accept=false and
   * no retry, so it can never touch the new document's state. */
  reset(sel: TimelineSelection): void {
    this.current = { ...sel };
    this.inFlight = null;
    this.pending = null;
  }

  /** Request a fetch for the current selection. Returns a ticket to
   * execute, or null when a fetch is already in flight — the current
   * selection is then remembered as pending (newest wins). */
  requestFetch(): TimelineTicket | null {
    const ticket: TimelineTicket = { seq: ++this.seq, selection: { ...this.current } };
    if (this.inFlight) {
      this.pending = { ...this.current };
      return null;
    }
    this.inFlight = ticket;
    return ticket;
  }

  /** Settle a previously issued ticket. Always releases the in-flight
   * slot for that ticket, so callers must settle every executed ticket
   * exactly once (success or failure) to avoid wedging the coordinator. */
  settle(ticket: TimelineTicket): TimelineSettleResult {
    const ownsFlight = this.inFlight !== null && this.inFlight.seq === ticket.seq;
    if (ownsFlight) this.inFlight = null;
    const accept =
      ownsFlight && ticket.seq === this.seq && sameSelection(ticket.selection, this.current);
    let retry: TimelineSelection | null = null;
    if (this.pending) {
      const next = this.pending;
      this.pending = null;
      // Refetch when the settled ticket was stale, or when the pending
      // selection moved on from what just settled.
      if (!accept || !sameSelection(next, ticket.selection)) retry = next;
    }
    return { accept, retry };
  }

  /** Attempt auto-selection (adopting the payload's attempt) is allowed
   * only when this fetch explicitly targeted no attempt AND the user has
   * still not chosen one — otherwise the newer user choice wins. */
  shouldAutoSelectAttempt(capturedAttempt: number | undefined): boolean {
    return capturedAttempt === undefined && this.current.attempt === undefined;
  }
}
