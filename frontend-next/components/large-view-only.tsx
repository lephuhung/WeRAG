"use client";

import { useSyncExternalStore, type ReactNode } from "react";
import { LARGE_VIEW, largeViewQuery, type LargeViewId } from "@/lib/large-view-policy";
import { IconClock, IconGraph } from "@/components/icons";
import { useT, type LocaleKey } from "@/lib/i18n";

/* Renders a surface only when the window is big enough for it to be usable;
 * otherwise one card that says so (see lib/large-view-policy.ts for the
 * thresholds and why those surfaces are gated at all).
 *
 * The server snapshot answers "fits": the graph tab is only mounted from a
 * click, and a desktop user must never see the notice flash before the
 * measurement lands. */

function subscribe(query: string, onChange: () => void) {
  const media = window.matchMedia(query);
  media.addEventListener("change", onChange);
  return () => media.removeEventListener("change", onChange);
}

/** One icon per gated surface, so the notice reads as that surface. */
const NOTICE_ICON: Record<LargeViewId, (p: { className?: string }) => ReactNode> = {
  graph: IconGraph,
  timeline: IconClock,
};

export function LargeViewOnly({
  id,
  onClose,
  children,
}: {
  id: LargeViewId;
  /** The gate replaces content that may carry its own close control (a
   *  drawer's header) — the notice then has to offer one, or a phone user
   *  would be stuck inside it. */
  onClose?: () => void;
  children: ReactNode;
}) {
  const { t } = useT();
  const query = largeViewQuery(id);
  const fits = useSyncExternalStore(
    (onChange) => subscribe(query, onChange),
    () => window.matchMedia(query).matches,
    () => true,
  );
  if (fits) return <>{children}</>;

  const view = LARGE_VIEW[id];
  const Icon = NOTICE_ICON[id];
  const current =
    typeof window === "undefined" ? "" : `${window.innerWidth} × ${window.innerHeight}`;
  return (
    <div className="flex min-h-0 flex-1 items-center justify-center overflow-y-auto p-6">
      <div className="flex max-w-[420px] flex-col items-center text-center">
        <Icon className="mb-3 h-8 w-8 text-muted-soft" />
        <p className="text-[15px] font-medium text-ink">{t(`largeView.${id}.title` as LocaleKey)}</p>
        <p className="body-sm mt-1 text-muted">
          {t(`largeView.${id}.desc` as LocaleKey, { size: view.sizeLabel })}
        </p>
        {current && (
          <p className="caption mt-3 text-muted-soft">{t("largeView.currentSize", { size: current })}</p>
        )}
        {onClose && (
          <button type="button" className="btn btn-outline btn-sm mt-4" onClick={onClose}>
            {t("common.close")}
          </button>
        )}
      </div>
    </div>
  );
}
