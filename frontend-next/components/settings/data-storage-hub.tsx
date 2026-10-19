"use client";

import { useEffect, useState } from "react";
import { StorageSettings } from "@/components/settings/storage-settings";
import { VectorStoreSettings } from "@/components/settings/vector-store-settings";
import {
  resolveDataStorageTab,
  type DataStorageTab,
} from "@/lib/data-storage-tabs";
import { useT } from "@/lib/i18n";

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
      <div
        className="flex items-center gap-6 overflow-x-auto border-b border-hairline"
        role="tablist"
        aria-label={t("settingsNav.dataStorage")}
      >
        {tabs.map((item) => (
          <button
            key={item.id}
            type="button"
            role="tab"
            aria-selected={tab === item.id}
            onClick={() => setTab(item.id)}
            className={`-mb-px shrink-0 whitespace-nowrap border-b-2 px-1 pb-3 pt-1 text-sm transition-colors focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-ink ${
              tab === item.id
                ? "border-ink font-semibold text-ink"
                : "border-transparent font-medium text-muted hover:border-hairline-strong hover:text-ink"
            }`}
          >
            {item.label}
          </button>
        ))}
      </div>

      {tab === "vector" && <VectorStoreSettings />}
      {tab === "storage" && <StorageSettings />}
    </div>
  );
}
