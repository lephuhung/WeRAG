"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { createKnowledgeBase } from "@/lib/api/knowledge";
import { useTenantRole } from "@/lib/auth";
import { useT } from "@/lib/i18n";

export default function NewKnowledgeBase() {
  const router = useRouter();
  const { t } = useT();
  /* Only Tenant Admins (or the platform SuperAdmin) reach this page —
   * the backend enforces it. Public visibility is retired: every KB is
   * private to its workspace; cross-workspace reads use invitations. */
  const { isTenantAdmin } = useTenantRole();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;
    setBusy(true);
    setError(null);
    try {
      const res = await createKnowledgeBase({
        name: name.trim(),
        description: description.trim() || undefined,
        visibility: "tenant",
      });
      const id =
        res.data && typeof res.data === "object" && "id" in res.data
          ? String((res.data as { id: unknown }).id)
          : null;
      router.push(id ? `/platform/knowledge-bases/${id}` : "/platform/knowledge-bases");
    } catch (err) {
      setError(err instanceof Error ? err.message : t("kbNew.createFailed"));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex-1 overflow-y-auto">
      <div className="mx-auto w-full max-w-[640px] px-4 py-6 sm:px-8 sm:py-10 lg:px-12">
        <div className="caption-uppercase mb-3 text-muted">{t("kbNew.workspace")}</div>
        <h1 className="display-xl mb-10">{t("kbNew.title")}</h1>
        {!isTenantAdmin && (
          <p className="body-sm mb-6 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-amber-700">
            {t("kbNew.adminOnly")}
          </p>
        )}
        <form className="card p-4 sm:p-8" onSubmit={submit}>
          <label className="mb-5 block">
            <span className="caption mb-1.5 block text-muted">{t("kbNew.name")}</span>
            <input
              className="input"
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t("kbNew.namePh")}
            />
          </label>
          <label className="mb-6 block">
            <span className="caption mb-1.5 block text-muted">{t("kbNew.desc")}</span>
            <textarea
              className="input min-h-[96px] resize-y"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={t("kbNew.descPh")}
            />
          </label>
          <div className="mb-6 block">
            <span className="caption mb-1.5 block text-muted">{t("kbSettings.visibilityNote")}</span>
            <p className="caption mt-1 text-muted-soft">{t("kbSettings.visTenantTip")}</p>
          </div>
          {error && <p className="body-sm mb-4 text-error">{error}</p>}
          <div className="flex gap-3">
            <button type="submit" className="btn btn-primary" disabled={busy || !name.trim()}>
              {busy ? t("kbNew.creating") : t("kbNew.create")}
            </button>
            <button type="button" className="btn btn-outline" onClick={() => router.back()}>
              {t("common.cancel")}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
