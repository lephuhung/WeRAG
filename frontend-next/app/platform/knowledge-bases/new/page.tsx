"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { createKnowledgeBase, type KBVisibility } from "@/lib/api/knowledge";
import { Select } from "@/components/select";
import { useTenantRole } from "@/lib/auth";
import { useT } from "@/lib/i18n";

export default function NewKnowledgeBase() {
  const router = useRouter();
  const { t } = useT();
  /* Backend only lets the workspace Owner or a system admin create public
   * KBs (validateKBVisibility); everyone else keeps the tenant default. */
  const { isOwner } = useTenantRole();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [visibility, setVisibility] = useState<KBVisibility>("tenant");
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
        visibility,
      });
      const id =
        res.data && typeof res.data === "object" && "id" in res.data
          ? String((res.data as { id: unknown }).id)
          : null;
      router.push(id ? `/platform/knowledge-bases/${id}` : "/platform/knowledge-bases");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Create failed");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex-1 overflow-y-auto">
      <div className="mx-auto w-full max-w-[640px] px-4 py-6 sm:px-8 sm:py-10 lg:px-12">
        <div className="caption-uppercase mb-3 text-muted">Workspace</div>
        <h1 className="display-xl mb-10">New knowledge base</h1>
        <form className="card p-4 sm:p-8" onSubmit={submit}>
          <label className="mb-5 block">
            <span className="caption mb-1.5 block text-muted">Name</span>
            <input
              className="input"
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. Product Documentation"
            />
          </label>
          <label className="mb-6 block">
            <span className="caption mb-1.5 block text-muted">Description</span>
            <textarea
              className="input min-h-[96px] resize-y"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="What does this collection cover?"
            />
          </label>
          {isOwner && (
            <div className="mb-6 block">
              <span className="caption mb-1.5 block text-muted">{t("kbSettings.visibilityNote")}</span>
              <Select
                className="w-[280px]"
                value={visibility}
                onChange={(v) => setVisibility(v as KBVisibility)}
                options={[
                  { value: "tenant", label: t("kbSettings.visTenant") },
                  { value: "public", label: t("kbSettings.visPublic") },
                ]}
              />
              <p className="caption mt-1 text-muted-soft">
                {t(visibility === "public" ? "kbSettings.visPublicTip" : "kbSettings.visTenantTip")}
              </p>
            </div>
          )}
          {error && <p className="body-sm mb-4 text-error">{error}</p>}
          <div className="flex gap-3">
            <button type="submit" className="btn btn-primary" disabled={busy || !name.trim()}>
              {busy ? "Creating…" : "Create knowledge base"}
            </button>
            <button type="button" className="btn btn-outline" onClick={() => router.back()}>
              Cancel
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
