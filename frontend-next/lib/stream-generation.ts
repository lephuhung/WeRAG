/* Generation guard for async stream work across session route changes.
//
// When the route id changes while session A still streams, the session-change
// effect resets `busy` for B — but A's already-registered onChunk /
// then / catch / finally callbacks would otherwise keep mutating the new B
// state (clearing B's busy flag, appending A's text to B's rows). Every async
// unit of work captures its generation at start and re-checks it before each
// late state write; a route change or a newer send bumps the counter and
// invalidates every stale callback. Pure and framework-free so it unit-tests
// without React. */
export interface StreamGeneration {
  /** Current (live) generation number. */
  current(): number;
  /** Start a new generation (route change / new send); returns its number. */
  next(): number;
  /** True only when `gen` is still the live generation. */
  isCurrent(gen: number): boolean;
}

export function createStreamGeneration(): StreamGeneration {
  let current = 0;
  return {
    current: () => current,
    next: () => {
      current += 1;
      return current;
    },
    isCurrent: (gen: number) => gen === current,
  };
}
