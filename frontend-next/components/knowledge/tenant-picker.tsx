/* Unit (workspace) picker fed by GET /tenants/directory. excludeId hides
 * one unit (the owner, for unit grants and owner transfer); personal
 * invites keep it, since members of the owner unit can be invited. */
"use client";

import { useEffect, useState } from "react";
import { listTenantDirectory, type TenantDirectoryEntry } from "@/lib/api/tenants";
import { useT } from "@/lib/i18n";

export function TenantPicker({
  value,
  onChange,
  excludeId,
  required,
}: {
  value: string;
  onChange: (id: string) => void;
  excludeId?: number | string;
  required?: boolean;
}) {
  const { t } = useT();
  const [units, setUnits] = useState<TenantDirectoryEntry[]>([]);

  useEffect(() => {
    let alive = true;
    listTenantDirectory()
      .then((rows) => alive && setUnits(rows))
      .catch(() => alive && setUnits([]));
    return () => {
      alive = false;
    };
  }, []);

  const exclude = excludeId === undefined ? "" : String(excludeId);
  return (
    <select
      className="input"
      required={required}
      value={value}
      onChange={(e) => onChange(e.target.value)}
    >
      <option value="">{t("kbGrant.pickUnit")}</option>
      {units
        .filter((u) => String(u.id) !== exclude)
        .map((u) => (
          <option key={u.id} value={String(u.id)}>
            {u.name}
          </option>
        ))}
    </select>
  );
}
