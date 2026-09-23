"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { apiPost } from "@/lib/api-client";
import { Orb } from "@/components/orb";

export default function WorkspaceOnboarding() {
  const router = useRouter();
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
      // POST /api/v1/tenants — caller becomes Owner (see tenant/index.ts createTenant).
      const res = await apiPost<{ success: boolean; data?: { id: number }; message?: string }>(
        `/api/v1/tenants`,
        { name: name.trim(), description: description.trim() || undefined },
      );
      if (!res.success) throw new Error(res.message ?? "Create failed");
      const tid = res.data?.id;
      if (tid) {
        try {
          localStorage.setItem("weknora_selected_tenant_id", String(tid));
        } catch {
          /* ignore */
        }
      }
      router.push("/platform/knowledge-bases");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Create failed");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="relative flex min-h-screen items-center justify-center overflow-hidden bg-canvas px-6">
      <Orb color="sky" size={560} className="-top-48 left-1/2 -translate-x-1/2" />
      <div className="relative w-full max-w-[480px]">
        <div className="display-lg mb-2 text-center">Create workspace</div>
        <p className="body-sm mb-10 text-center text-muted">
          Workspaces isolate knowledge bases, agents and members.
        </p>
        <form className="card p-6 sm:p-8" onSubmit={submit}>
          <label className="mb-5 block">
            <span className="caption mb-1.5 block text-muted">Workspace name</span>
            <input
              className="input"
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. Acme Corp"
            />
          </label>
          <label className="mb-6 block">
            <span className="caption mb-1.5 block text-muted">Description</span>
            <input
              className="input"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Optional description"
            />
          </label>
          {error && <p className="body-sm mb-4 text-error">{error}</p>}
          <button type="submit" className="btn btn-primary w-full" disabled={busy || !name.trim()}>
            {busy ? "Creating…" : "Create workspace"}
          </button>
        </form>
      </div>
    </div>
  );
}
