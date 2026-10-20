"use client";

import { useEffect, useState } from "react";
import { StorageSettings } from "@/components/settings/storage-settings";
import { VectorStoreSettings } from "@/components/settings/vector-store-settings";
import {
  resolveDataStorageTab,
  type DataStorageTab,
} from "@/lib/data-storage-tabs";
import { useT } from "@/lib/i18n";
import { TabStrip } from "@/components/settings/tab-strip";

export function DataStorageHub({ initialTab = "vector" }: { initialTab?: DataStorageTab }) {
  const { t } = useT();
  const [tab, setTab] = useState<DataStorageTab>(resolveDataStorageTab(initialTab));

  useEffect(() => setTab(resolveDataStorageTab(initialTab)), [initialTab]);

  const tabs: { id: DataStorageTab; label: string }[] = [
    { id: "vector", label: t("settingsNav.vectorstore") },
    { id: "storage", label: t("settingsNav.storage") },
  ];

  return (
    <div className="space-y-5">
      <TabStrip tabs={tabs} value={tab} onChange={setTab} ariaLabel={t("settingsNav.dataStorage")} />

      {tab === "vector" && (
        <div id="tabpanel-vector" role="tabpanel" aria-labelledby="tab-vector">
          <VectorStoreSettings />
        </div>
      )}
      {tab === "storage" && (
        <div id="tabpanel-storage" role="tabpanel" aria-labelledby="tab-storage">
          <StorageSettings />
        </div>
      )}
    </div>
  );
}
