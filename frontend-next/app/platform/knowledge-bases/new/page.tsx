"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { createKnowledgeBase, createPublicKnowledgeBase } from "@/lib/api/knowledge";
import { useTenantRole } from "@/lib/auth";
import { useT, type LocaleKey } from "@/lib/i18n";
import { buildPublicCreatePayload } from "@/lib/kb-public";
import type { KBMemberDefault } from "@/lib/kb-access";

type CreateScope = "tenant" | "restricted" | "public";

const SCOPE_KEYS: Record<CreateScope, { label: LocaleKey; desc: LocaleKey }> = {
  tenant: { label: "kbAccess.scope.tenant", desc: "kbAccess.scope.tenantDesc" },
  restricted: { label: "kbAccess.scope.restricted", desc: "kbAccess.scope.restrictedDesc" },
  public: { label: "kbAccess.scope.public", desc: "kbNew.publicNote" },
};

export default function NewKnowledgeBase() {
  const router = useRouter();
  const { t } = useT();
  /* Only Tenant Admins (or the platform SuperAdmin) reach this page —
   * the backend enforces it. The scope is chosen here (default: whole
   * unit); publishing happens later in the Access tab. Platform public is
   * a SuperAdmin-only choice that goes through POST /knowledge-bases/public. */
  const { isTenantAdmin, isGuest, isSystemAdmin } = useTenantRole();
  const [scope, setScope] = useState<CreateScope>("tenant");
  const [memberDefault, setMemberDefault] = useState<KBMemberDefault>("contribute");
  const scopes: CreateScope[] = isSystemAdmin ? ["tenant", "restricted", "public"] : ["tenant", "restricted"];
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
      const res =
        scope === "public"
          ? await createPublicKnowledgeBase(buildPublicCreatePayload({ name, description }))
          : await createKnowledgeBase({
              name: name.trim(),
              description: description.trim() || undefined,
              visibility: scope,
              ...(scope === "tenant" ? { member_default: memberDefault } : {}),
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
        {isGuest ? (
          <p className="body-sm mb-6 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-amber-700">
            {t("kbNew.guestBlocked")}
          </p>
        ) : (
          !isTenantAdmin && (
            <p className="body-sm mb-6 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-amber-700">
              {t("kbNew.adminOnly")}
            </p>
          )
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
          <fieldset className="mb-6 block">
            <legend className="caption mb-1.5 block text-muted">{t("kbNew.scope")}</legend>
            <div className="space-y-2">
              {scopes.map((v) => (
                <label
                  key={v}
                  className={`flex cursor-pointer items-start gap-2.5 rounded-xl border px-3 py-2 ${
                    scope === v ? "border-ink/40 bg-surface-strong/50" : "border-hairline"
                  }`}
                >
                  <input
                    type="radio"
                    name="kb-scope"
                    className="mt-1"
                    required
                    checked={scope === v}
                    onChange={() => setScope(v)}
                  />
                  <span>
                    <span className="block text-[13px] font-medium text-ink">{t(SCOPE_KEYS[v].label)}</span>
                    <span className="caption block text-muted">{t(SCOPE_KEYS[v].desc)}</span>
                  </span>
                </label>
              ))}
            </div>
            {scope === "tenant" && (
              <label className="mt-3 block">
                <span className="caption mb-1 block text-muted">{t("kbAccess.memberDefault")}</span>
                <select
                  className="input max-w-sm"
                  value={memberDefault}
                  onChange={(e) => setMemberDefault(e.target.value as KBMemberDefault)}
                >
                  <option value="contribute">{t("kbAccess.memberDefault.contribute")}</option>
                  <option value="view">{t("kbAccess.memberDefault.view")}</option>
                </select>
              </label>
            )}
          </fieldset>
          {error && <p className="body-sm mb-4 text-error">{error}</p>}
          <div className="flex gap-3">
            <button type="submit" className="btn btn-primary" disabled={busy || isGuest || !name.trim()}>
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
