"use client";

import { useState } from "react";
import { apiPost } from "@/lib/api-client";
import { useAuth } from "@/lib/auth";
import { useT } from "@/lib/i18n";

export function WeKnoraCloudSettings() {
  const { t } = useT();
  const auth = useAuth();
  const isSystemAdmin = auth.user?.is_system_admin === true;

  const [appId, setAppId] = useState("");
  const [appSecret, setAppSecret] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!appId.trim() || !appSecret.trim()) return;
    setSaving(true);
    setError("");
    setSuccess("");
    try {
      const res = await apiPost<{ success: boolean; message?: string }>(
        "/api/v1/weknoracloud/credentials",
        {
          app_id: appId.trim(),
          app_secret: appSecret.trim(),
        },
      );
      if (res.success) {
        setSuccess(t("wkn.saved"));
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
      <div className="border-b border-hairline pb-5">
        <h2 className="title-md font-semibold text-ink">{t("wkn.title")}</h2>
        <p className="caption text-muted mt-1">
          {t("wkn.subtitle")}
        </p>
      </div>

      {error && (
        <div className="rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700 dark:border-rose-900/50 dark:bg-rose-950/30 dark:text-rose-400">
          {error}
        </div>
      )}
      {success && (
        <div className="rounded-xl border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-700 dark:border-emerald-900/50 dark:bg-emerald-950/30 dark:text-emerald-400">
          {success}
        </div>
      )}

      {/* Form */}
      <form onSubmit={handleSubmit} className="space-y-4 max-w-lg">
        <label className="block">
          <span className="text-sm font-medium text-ink block mb-1">{t("wkn.appId")}</span>
          <input
            type="text"
            required
            disabled={!isSystemAdmin || saving}
            placeholder={t("wkn.appIdPh")}
            className="input font-mono text-sm"
            value={appId}
            onChange={(e) => setAppId(e.target.value)}
          />
        </label>

        <label className="block">
          <span className="text-sm font-medium text-ink block mb-1">{t("wkn.appSecret")}</span>
          <input
            type="password"
            required
            disabled={!isSystemAdmin || saving}
            placeholder="••••••••••••••••"
            className="input font-mono text-sm"
            value={appSecret}
            onChange={(e) => setAppSecret(e.target.value)}
          />
        </label>

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
