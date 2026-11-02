export type DataStorageTab = "vector" | "storage";

export function resolveDataStorageTab(section?: string): DataStorageTab {
  return section === "storage" ? "storage" : "vector";
}
