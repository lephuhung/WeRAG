import { useEffect, useRef, useState } from "react";
import { getBrowserConnection, type BrowserAccountStatus } from "@/lib/api/browser";

/* Ports useBrowserConnectionStore's knownOffline getter:
 * loaded && !(enabled && connected). Once loaded, the sender stays
 * conservative — a failed poll keeps the last known status. */
export function useBrowserKnownOffline(): boolean {
  const [knownOffline, setKnownOffline] = useState(false);
  const alive = useRef(true);

  useEffect(() => {
    alive.current = true;
    let timer = 0;
    let stopped = false;

    const apply = (data: BrowserAccountStatus | null | undefined) => {
      if (!alive.current || !data) return;
      setKnownOffline(!(data.enabled === true && data.connected === true));
    };

    const poll = async () => {
      try {
        if (!document.hidden) {
          const res = await getBrowserConnection();
          apply(res.data);
        }
      } catch {
        /* keep last known status */
      }
      if (!stopped && alive.current) {
        timer = window.setTimeout(() => void poll(), 5000);
      }
    };

    void poll();
    const onVisible = () => {
      if (!document.hidden) void poll();
    };
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      stopped = true;
      alive.current = false;
      document.removeEventListener("visibilitychange", onVisible);
      clearTimeout(timer);
    };
  }, []);

  return knownOffline;
}
