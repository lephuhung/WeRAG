"use client";

import { useEffect, useState } from "react";
import { MemoryPersonalSettings } from "@/components/settings/memory-personal-settings";
import { MemoryWorkspaceSettings } from "@/components/settings/memory-workspace-settings";
import { ChatHistorySettings } from "@/components/settings/chat-history-settings";
import { useT } from "@/lib/i18n";

export type MemoryHubTab = "mine" | "config" | "chathistory";

/* One "Memory" section for what used to be three near-identical entries:
 * the user's own memory items, the workspace memory config, and chat-history
 * indexing (also recall over past conversations). */
export function MemoryHub({ initialTab = "mine" }: { initialTab?: MemoryHubTab }) {
  const { t } = useT();
  const [tab, setTab] = useState<MemoryHubTab>(initialTab);
  useEffect(() => setTab(initialTab), [initialTab]);

  const tabs: { id: MemoryHubTab; label: string }[] = [
    { id: "mine", label: t("memhub.mine") },
    { id: "config", label: t("memhub.config") },
    { id: "chathistory", label: t("memhub.chathistory") },
  ];

  return (
    <div className="space-y-5">
      <div className="flex items-center gap-6 overflow-x-auto border-b border-hairline" role="tablist">
        {tabs.map((it) => (
          <button
            key={it.id}
            type="button"
            role="tab"
            aria-selected={tab === it.id}
            onClick={() => setTab(it.id)}
            className={`-mb-px shrink-0 whitespace-nowrap border-b-2 px-1 pb-3 pt-1 text-sm transition-colors focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-ink ${
              tab === it.id
                ? "border-ink font-semibold text-ink"
                : "border-transparent font-medium text-muted hover:border-hairline-strong hover:text-ink"
            }`}
          >
            {it.label}
          </button>
        ))}
      </div>
      {tab === "mine" && <MemoryPersonalSettings />}
      {tab === "config" && <MemoryWorkspaceSettings />}
      {tab === "chathistory" && <ChatHistorySettings />}
    </div>
  );
}
