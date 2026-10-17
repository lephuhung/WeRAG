/* Hồ sơ đơn vị: the workspace's issuing agency, used for the header and the
 * signature block of documents drafted from chat (Word add-in templates).
 * Members read it; the workspace admin or a SuperAdmin edits it (the
 * server enforces this; the form is read-only for the others). */
"use client";

import { useCallback, useEffect, useState } from "react";
import { ApiError } from "@/lib/api-client";
import { getAgencyProfile, updateAgencyProfile, type AgencyProfile } from "@/lib/api/agency-profile";
import { useAuth } from "@/lib/auth";
import { useT, type LocaleKey } from "@/lib/i18n";
import { TemplateFontSizesSettings } from "@/components/settings/template-font-sizes";

const EMPTY: AgencyProfile = {
  parent_agency: "", agency_name: "", agency_abbr: "", locality: "",
  head_title: "", collective_name: "", default_recipients: [], version: 0,
};

type TextField = "parent_agency" | "agency_name" | "agency_abbr" | "locality" | "head_title" | "collective_name";

const FIELDS: { key: TextField; label: LocaleKey; hint: LocaleKey; max: number }[] = [
  { key: "parent_agency", label: "agencyProfile.parent", hint: "agencyProfile.parentHint", max: 255 },
  { key: "agency_name", label: "agencyProfile.name", hint: "agencyProfile.nameHint", max: 255 },
  { key: "agency_abbr", label: "agencyProfile.abbr", hint: "agencyProfile.abbrHint", max: 32 },
  { key: "locality", label: "agencyProfile.locality", hint: "agencyProfile.localityHint", max: 128 },
  { key: "head_title", label: "agencyProfile.head", hint: "agencyProfile.headHint", max: 255 },
  { key: "collective_name", label: "agencyProfile.collective", hint: "agencyProfile.collectiveHint", max: 255 },
];

export function AgencyProfileSettings() {
  const { t } = useT();
  const auth = useAuth();
  const isSystemAdmin = auth.user?.is_system_admin === true;
  const currentRole = auth.memberships.find(
    (m) => String(m.tenant_id) === String(auth.selectedTenantId ?? auth.tenant?.id ?? ""),
  )?.role;
  const canEdit = isSystemAdmin || currentRole === "admin" || currentRole === "owner";

  const [profile, setProfile] = useState<AgencyProfile>(EMPTY);
  const [recipients, setRecipients] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState("");

  const apply = (p: AgencyProfile | null) => {
    const next = p ? { ...EMPTY, ...p, default_recipients: p.default_recipients ?? [] } : EMPTY;
    setProfile(next);
    setRecipients(next.default_recipients.join("\n"));
  };

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const res = await getAgencyProfile();
      apply(res.data);
    } catch (e) {
      setError(e instanceof Error ? e.message : t("agencyProfile.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    void load();
  }, [load]);

  const save = async () => {
    setSaving(true);
    setSaved(false);
    setError("");
    try {
      const lines = recipients.split("\n").map((l) => l.trim()).filter(Boolean);
      const res = await updateAgencyProfile({ ...profile, default_recipients: lines });
      apply(res.data);
      setSaved(true);
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) setError(t("agencyProfile.conflict"));
      else setError(e instanceof Error ? e.message : t("agencyProfile.saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  if (loading) return <p className="caption text-muted">{t("common.loading")}</p>;

  return (
    <div className="flex flex-col gap-4">
      <div>
        <h2 className="text-base font-semibold">{t("agencyProfile.title")}</h2>
        <p className="caption mt-1 text-muted">{t("agencyProfile.desc")}</p>
        {!canEdit && <p className="caption mt-1 text-muted">{t("agencyProfile.readOnly")}</p>}
      </div>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        {FIELDS.map((f) => (
          <label key={f.key} className="block">
            <span className="caption mb-1.5 block text-muted">{t(f.label)}</span>
            <input
              className="input w-full"
              value={profile[f.key]}
              maxLength={f.max}
              disabled={!canEdit || saving}
              required={f.key === "agency_name"}
              onChange={(e) => {
                setProfile({ ...profile, [f.key]: e.target.value });
                setSaved(false);
              }}
            />
            <span className="caption mt-1 block text-muted">{t(f.hint)}</span>
          </label>
        ))}
      </div>
      <label className="block">
        <span className="caption mb-1.5 block text-muted">{t("agencyProfile.recipients")}</span>
        <textarea
          className="input min-h-24 w-full"
          value={recipients}
          disabled={!canEdit || saving}
          placeholder={"- UBND tỉnh (để b/c);\n- Các phòng thuộc Sở;"}
          onChange={(e) => {
            setRecipients(e.target.value);
            setSaved(false);
          }}
        />
        <span className="caption mt-1 block text-muted">{t("agencyProfile.recipientsHint")}</span>
      </label>
      {error && <p className="caption text-error">{error}</p>}
      {canEdit && (
        <div className="flex items-center gap-3">
          <button
            type="button"
            className="btn btn-primary btn-sm"
            disabled={saving || !profile.agency_name.trim()}
            onClick={() => void save()}
          >
            {saving ? t("agencyProfile.saving") : t("common.save")}
          </button>
          {saved && <span className="caption text-muted">{t("agencyProfile.saved")}</span>}
        </div>
      )}
      <TemplateFontSizesSettings canEditTenant={canEdit} isSystemAdmin={isSystemAdmin} />
    </div>
  );
}
