/* Work context of the user profile: đơn vị công tác and tỉnh/thành phố.
 * Stored in the user's preferences and handed to the assistant as user
 * context; the province is the default for local document numbers every
 * province reuses. Used by the profile settings and by WorkContextPrompt. */
"use client";

import { useEffect, useMemo, useState } from "react";
import { useAuth } from "@/lib/auth";
import { useT } from "@/lib/i18n";
import { getProvinces, updateMyPreferences, type Province } from "@/lib/api/auth";
import { Select } from "@/components/select";
import { useToast } from "@/components/toast";
import { Notice } from "@/components/settings/notice";
import { FieldLabel } from "@/components/settings/field";

export function WorkContextForm({ onSaved, compact = false }: { onSaved?: () => void; compact?: boolean }) {
  const { t } = useT();
  const auth = useAuth();
  const toast = useToast();
  const prefs = auth.user?.preferences;
  const [provinces, setProvinces] = useState<Province[]>([]);
  const [unit, setUnit] = useState(prefs?.work_unit ?? "");
  const [provinceId, setProvinceId] = useState(prefs?.province_id ?? "");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    getProvinces()
      .then(setProvinces)
      .catch(() => setProvinces([]));
  }, []);

  useEffect(() => {
    if (saving) return;
    setUnit(prefs?.work_unit ?? "");
    setProvinceId(prefs?.province_id ?? "");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [prefs?.work_unit, prefs?.province_id]);

  const options = useMemo(
    () =>
      [...provinces]
        .sort((a, b) => a.short.localeCompare(b.short, "vi"))
        .map((p) => ({
          value: p.id,
          label: p.former_names?.length ? `${p.name} (${t("workCtx.formerly")} ${p.former_names.join(", ")})` : p.name,
          // Former names match too: "Quảng Nam" finds Thành phố Đà Nẵng.
          keywords: [p.name, ...(p.former_names ?? [])].join(" "),
        })),
    [provinces, t]
  );
  const dirty = unit.trim() !== (prefs?.work_unit ?? "") || provinceId !== (prefs?.province_id ?? "");

  const save = async () => {
    setSaving(true);
    setError("");
    try {
      const res = await updateMyPreferences({ work_unit: unit.trim(), province_id: provinceId });
      if (!res.success) throw new Error(res.message || t("common.error"));
      await auth.refreshMe();
      toast.success(t("common.saved"));
      onSaved?.();
    } catch (e) {
      setError(e instanceof Error ? e.message : t("common.error"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="flex flex-col gap-3">
      {!compact && <p className="caption text-muted">{t("workCtx.hint")}</p>}
      <div>
        <FieldLabel htmlFor="workCtx.unit">{t("workCtx.unit")}</FieldLabel>
        <input
          id="workCtx.unit"
          className="input"
          value={unit}
          maxLength={200}
          placeholder={t("workCtx.unitPlaceholder")}
          onChange={(e) => setUnit(e.target.value)}
        />
      </div>
      <div>
        <FieldLabel htmlFor="workCtx.province">{t("workCtx.province")}</FieldLabel>
        <Select
          id="workCtx.province"
          value={provinceId}
          onChange={setProvinceId}
          options={options}
          placeholder={t("workCtx.provincePlaceholder")}
          searchable
          searchPlaceholder={t("workCtx.provinceSearch")}
        />
      </div>
      <div className="flex items-center gap-3">
        <button className="btn btn-primary btn-sm" onClick={() => void save()} disabled={saving || !dirty}>
          {saving ? "…" : t("common.save")}
        </button>
      </div>
      {error && <Notice tone="error">{error}</Notice>}
    </div>
  );
}
