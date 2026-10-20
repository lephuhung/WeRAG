"use client";

import { useEffect, useState } from "react";
import { MemoryPersonalSettings } from "@/components/settings/memory-personal-settings";
import { MemoryWorkspaceSettings } from "@/components/settings/memory-workspace-settings";
import { ChatHistorySettings } from "@/components/settings/chat-history-settings";
import { useT } from "@/lib/i18n";
import { TabStrip } from "@/components/settings/tab-strip";

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
      <TabStrip tabs={tabs} value={tab} onChange={setTab} ariaLabel={t("settingsNav.memory")} />

      {tab === "mine" && (
        <div id="tabpanel-mine" role="tabpanel" aria-labelledby="tab-mine">
          <MemoryPersonalSettings />
        </div>
      )}
      {tab === "config" && (
        <div id="tabpanel-config" role="tabpanel" aria-labelledby="tab-config">
          <MemoryWorkspaceSettings />
        </div>
      )}
      {tab === "chathistory" && (
        <div id="tabpanel-chathistory" role="tabpanel" aria-labelledby="tab-chathistory">
          <ChatHistorySettings />
        </div>
      )}
    </div>
  );
}
