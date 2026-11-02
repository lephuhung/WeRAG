"use client";

import { useEffect, useRef, useState } from "react";

// The backend releases a document-assistant answer a sentence at a time (the
// answer guard drops a whole sentence that invites a button the turn did not
// produce), so the text arrives in bursts 300–700 ms apart. This reveals the
// received text at a steady pace instead: each tick shows a share of the
// backlog sized to drain it before the next burst, so the reveal never falls
// far behind the stream. Text that was not streaming when first seen
// (history) shows at once.

const TICK_MS = 40;
// Time to drain the backlog while the stream is live, and after it ended.
const DRAIN_MS = 450;
const FINAL_DRAIN_MS = 250;
const MIN_CHARS_PER_TICK = 2;

export function useSmoothText(text: string, streaming: boolean): { text: string; revealing: boolean } {
  const animate = useRef(streaming);
  if (streaming) animate.current = true;
  const [shown, setShown] = useState(() => (streaming ? 0 : text.length));
  // a retracted preamble can shorten the text below what was shown; new text
  // after it is revealed from there, not shown at once
  if (shown > text.length) setShown(text.length);
  // the interval reads the latest text and stream state without restarting:
  // a token stream faster than a tick must not keep postponing it
  const latest = useRef({ text, streaming });
  latest.current = { text, streaming };
  const behind = animate.current && shown < text.length;

  useEffect(() => {
    if (!behind) return;
    const timer = setInterval(() => {
      const { text: full, streaming: live } = latest.current;
      setShown((prev) => {
        if (prev >= full.length) return full.length;
        const backlog = full.length - prev;
        const step = Math.max(MIN_CHARS_PER_TICK, Math.ceil((backlog * TICK_MS) / (live ? DRAIN_MS : FINAL_DRAIN_MS)));
        let next = Math.min(full.length, prev + step);
        // never split a surrogate pair
        const code = full.charCodeAt(next - 1);
        if (code >= 0xd800 && code <= 0xdbff && next < full.length) next++;
        return next;
      });
    }, TICK_MS);
    return () => clearInterval(timer);
  }, [behind]);

  if (!animate.current) return { text, revealing: false };
  const n = Math.min(shown, text.length);
  return { text: n === text.length ? text : text.slice(0, n), revealing: n < text.length };
}
