"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { createPublicKnowledgeBase } from "@/lib/api/knowledge";
import { buildPublicCreatePayload } from "@/lib/kb-public";
import { useTenantRole } from "@/lib/auth";
import { useT } from "@/lib/i18n";

/* Explicit-SuperAdmin-only platform-public KB creation. The request carries
 * only name/description/type — tenant storage backends, vector-store
 * bindings, and selected-workspace defaults never cross the wire (the
 * service resolves platform defaults and rejects tenant-bound bindings).
 * Backend remains authoritative: non-SuperAdmin submits fail closed. */
export default function NewPublicKnowledgeBase() {
  const router = useRouter();
  const { t } = useT();
  const { isSystemAdmin } = useTenantRole();
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
      const res = await createPublicKnowledgeBase(
        buildPublicCreatePayload({ name: name.trim(), description: description.trim(), type: "document" }),
      );
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
        <div className="caption-uppercase mb-3 text-muted">Platform</div>
        <h1 className="display-xl mb-4">{t("kbPublic.newPublic")}</h1>
        <p className="body-sm mb-10 max-w-[520px] text-body">{t("kbPublic.sectionDesc")}</p>
        {!isSystemAdmin && (
          <p className="body-sm mb-6 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-amber-700">
            Only platform system admins can create public knowledge bases.
          </p>
        )}
        <form className="card p-4 sm:p-8" onSubmit={submit}>
          <label className="mb-5 block">
            <span className="caption mb-1.5 block text-muted">Name</span>
            <input
              className="input"
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. Civil Code"
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
          {error && <p className="body-sm mb-4 text-error">{error}</p>}
          <div className="flex gap-3">
            <button type="submit" className="btn btn-primary" disabled={busy || !name.trim() || !isSystemAdmin}>
              {busy ? "Creating…" : t("kbPublic.newPublic")}
            </button>
            <button type="button" className="btn btn-outline" onClick={() => router.back()}>
              {t("kbPublic.cancel")}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
