"use client";

import { useState } from "react";
import { apiPost } from "@/lib/api-client";
import { useAuth } from "@/lib/auth";
import { useT } from "@/lib/i18n";
import { Notice } from "@/components/settings/notice";
import { SectionHeader } from "@/components/settings/section-header";
import { FieldLabel } from "@/components/settings/field";
import { useToast } from "@/components/toast";

export function WeKnoraCloudSettings() {
  const { t } = useT();
  const auth = useAuth();
  const isSystemAdmin = auth.user?.is_system_admin === true;

  const [appId, setAppId] = useState("");
  const [appSecret, setAppSecret] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const toast = useToast();

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!appId.trim() || !appSecret.trim()) return;
    setSaving(true);
    setError("");
    try {
      const res = await apiPost<{ success: boolean; message?: string }>(
        "/api/v1/weknoracloud/credentials",
        {
          app_id: appId.trim(),
          app_secret: appSecret.trim(),
        },
      );
      if (res.success) {
        toast.success(t("wkn.saved"));
        setAppSecret("");
      } else {
        setError(res.message || t("wkn.saveFailed"));
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : t("wkn.saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-8">
      {/* Header */}
      <SectionHeader title={t("wkn.title")} subtitle={t("wkn.subtitle")} />

      {error && (
        <Notice tone="error">{error}</Notice>
      )}

      {/* Form */}
      <form onSubmit={handleSubmit} className="space-y-4 max-w-lg">
        <div>
          <FieldLabel htmlFor="wkn-app-id" required className="text-sm font-medium text-ink">
            {t("wkn.appId")}
          </FieldLabel>
          <input
            id="wkn-app-id"
            type="text"
            required
            aria-required="true"
            disabled={!isSystemAdmin || saving}
            placeholder={t("wkn.appIdPh")}
            className="input font-mono text-sm"
            value={appId}
            onChange={(e) => setAppId(e.target.value)}
          />
        </div>

        <div>
          <FieldLabel htmlFor="wkn-app-secret" required className="text-sm font-medium text-ink">
            {t("wkn.appSecret")}
          </FieldLabel>
          <input
            id="wkn-app-secret"
            type="password"
            required
            aria-required="true"
            disabled={!isSystemAdmin || saving}
            placeholder="••••••••••••••••"
            className="input font-mono text-sm"
            value={appSecret}
            onChange={(e) => setAppSecret(e.target.value)}
          />
        </div>

        {isSystemAdmin && (
          <button
            type="submit"
            disabled={saving || !appId.trim() || !appSecret.trim()}
            className="btn btn-primary"
          >
            {saving ? t("wkn.saving") : t("wkn.saveCta")}
          </button>
        )}
      </form>
    </div>
  );
}
