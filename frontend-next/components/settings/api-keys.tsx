/* Ported from frontend/src/views/system/PlatformAPIKeys.vue (core): list
 * tenant API keys, create with capability matrix / full_access, reveal the
 * created token once, delete. KB-level scoping pickers stay in the Vue app.
 */
"use client";

import { Modal } from "@/components/modal";
import { Toggle } from "@/components/settings/toggle";

import { useCallback, useEffect, useState } from "react";
import {
  listTenantAPIKeys,
  createTenantAPIKey,
  deleteTenantAPIKey,
  type TenantAPIKey,
  type TenantAPIKeyCapability,
  type CreatedTenantAPIKey,
} from "@/lib/api/tenants";
import { useAuth } from "@/lib/auth";
import { useT } from "@/lib/i18n";
import { copyToClipboard } from "@/lib/clipboard";

const CAPABILITIES: { id: TenantAPIKeyCapability; labelEn: string; labelVi: string }[] = [
  { id: "retrieve", labelEn: "Retrieve", labelVi: "Truy xuất" },
  { id: "chat", labelEn: "Chat", labelVi: "Chat" },
  { id: "read_agents", labelEn: "Read agents", labelVi: "Xem trợ lý" },
  { id: "ingest", labelEn: "Ingest", labelVi: "Nạp liệu" },
  { id: "manage_kbs", labelEn: "Manage KBs", labelVi: "Quản lý kho tri thức" },
  { id: "manage_agents", labelEn: "Manage agents", labelVi: "Quản lý trợ lý" },
  { id: "message_history", labelEn: "Message history", labelVi: "Lịch sử tin nhắn" },
  { id: "manage_models", labelEn: "Manage models", labelVi: "Quản lý mô hình" },
  { id: "manage_members", labelEn: "Manage members", labelVi: "Quản lý thành viên" },
  { id: "manage_tenant_settings", labelEn: "Manage workspace settings", labelVi: "Quản lý cài đặt không gian" },
];

function fmtDate(v?: string): string {
  if (!v) return "-";
  try {
    return new Date(v).toLocaleDateString();
  } catch {
    return v;
  }
}

export function ApiKeysSection() {
  const { t } = useT();
  const auth = useAuth();
  const [keys, setKeys] = useState<TenantAPIKey[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [createOpen, setCreateOpen] = useState(false);
  const [created, setCreated] = useState<CreatedTenantAPIKey | null>(null);

  const load = useCallback(() => {
    setLoading(true);
    const tenantId = Number(localStorage.getItem("weknora_selected_tenant_id") ?? auth.tenant?.id ?? 0);
    listTenantAPIKeys(tenantId)
      .then((res) => setKeys(res.data ?? []))
      .catch((e) => setError(e instanceof Error ? e.message : "Failed to load keys"))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const isOwner = ({ member: 10, admin: 30, owner: 40 }[
    auth.memberships.find(
      (m) => String(m.tenant_id) === String(auth.selectedTenantId ?? auth.tenant?.id ?? ""),
    )?.role ?? ""
  ] ?? 0) >= 40 || auth.user?.is_system_admin === true;

  return (
    <div className="flex flex-col gap-5">
      <div className="flex items-center justify-between">
        <p className="caption text-muted">
          {t("apiKeys.sectionDesc")}
        </p>
        {isOwner && (
          <button className="btn btn-primary btn-sm" onClick={() => setCreateOpen(true)}>
            {t("apiKeys.create")}
          </button>
        )}
      </div>

      {error && <p className="caption text-error">{error}</p>}
      {loading && <p className="caption text-muted">…</p>}

      <div className="flex flex-col divide-y divide-hairline">
        {(keys ?? []).map((k) => (
          <div key={k.id} className="flex items-center gap-4 py-3">
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2">
                <span className="truncate text-[14px] font-medium text-ink">{k.name}</span>
                {k.full_access ? (
                  <span className="badge-pill text-error">{t("apiKeys.fullAccess")}</span>
                ) : (
                  <span className="badge-pill">{(k.capabilities ?? []).length} caps</span>
                )}
              </div>
              <div className="caption mt-0.5 text-muted">
                {k.knowledge_base_ids
                  ? `${t("apiKeys.kbScoped")}: ${k.knowledge_base_ids.length}`
                  : t("apiKeys.allKbs")}
                {k.last_used_at ? ` · ${t("apiKeys.lastUsed")}: ${fmtDate(k.last_used_at)}` : ""}
              </div>
            </div>
            <code className="caption overflow-x-auto text-muted-soft">
              {k.api_key ? `${k.api_key.slice(0, 8)}…` : ""}
            </code>
            {isOwner && (
              <button
                className="btn btn-tertiary btn-sm text-[13px] text-error"
                onClick={() => {
                  void deleteTenantAPIKey(Number(auth.selectedTenantId ?? auth.tenant?.id ?? 0), k.id).then(() => load());
                }}
              >
                {t("common.delete")}
              </button>
            )}
          </div>
        ))}
        {keys !== null && keys.length === 0 && !loading && !error && (
          <p className="caption text-muted-soft">{t("apiKeys.empty")}</p>
        )}
      </div>

      {createOpen && (
        <CreateKeyModal
          onClose={() => setCreateOpen(false)}
          onCreated={(k) => {
            setCreateOpen(false);
            setCreated(k);
            load();
          }}
        />
      )

      }
      {created && (
        <Modal open title={t("apiKeys.createdTitle")} onClose={() => setCreated(null)} width="w-[560px]">
          <p className="body-sm text-body">{t("apiKeys.createdBody")}</p>
          <code className="mt-3 block max-h-[120px] overflow-y-auto break-all rounded-[10px] bg-surface-strong px-3.5 py-3 font-mono text-[13px] text-ink">
            {created.token ?? created.api_key}
          </code>
          <div className="mt-3 flex justify-end">
            <button
              className="btn btn-outline btn-sm"
              onClick={() => void copyToClipboard(created.token ?? created.api_key)}
            >
              {t("integrations.copy")}
            </button>
          </div>
        </Modal>
      )}
    </div>
  );
}

function CreateKeyModal({ onClose, onCreated }: {
  onClose: () => void;
  onCreated: (k: CreatedTenantAPIKey) => void;
}) {
  const { t } = useT();
  const [name, setName] = useState("");
  const [fullAccess, setFullAccess] = useState(false);
  const [caps, setCaps] = useState<Set<TenantAPIKeyCapability>>(new Set(["retrieve"]));
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  const toggleCap = (id: TenantAPIKeyCapability) => {
    setCaps((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const submit = async () => {
    if (!name.trim()) return;
    const tenantId = Number(localStorage.getItem("weknora_selected_tenant_id") ?? "0");
    setSaving(true);
    setError("");
    try {
      const payload = {
        name: name.trim(),
        full_access: fullAccess,
        ...(fullAccess ? {} : { capabilities: [...caps] }),
      };
      const createdRes = await createTenantAPIKey(tenantId, payload);
      if (createdRes.data) onCreated(createdRes.data);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Create failed");
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal open title={t("apiKeys.create")} onClose={onClose} width="w-[600px]">
      <div className="flex max-h-[70vh] flex-col gap-4 overflow-y-auto pr-1">
        {error && <p className="caption text-error">{error}</p>}
        <label className="block">
          <span className="caption mb-1.5 block text-muted">{t("apiKeys.nameLabel")}</span>
          <input className="input" value={name} onChange={(e) => setName(e.target.value)} autoFocus />
        </label>
        <div className="flex items-center gap-3">
          <Toggle checked={fullAccess} onChange={setFullAccess} label={t("apiKeys.fullAccess")} />
          <span className="title-sm">{t("apiKeys.fullAccess")}</span>
          <span className="caption text-muted">{t("apiKeys.fullAccessDesc")}</span>
        </div>
        {!fullAccess && (
          <div className="border-t border-hairline pt-4">
            <div className="caption-uppercase mb-3 text-muted">{t("apiKeys.capabilities")}</div>
            <div className="grid grid-cols-1 gap-x-6 gap-y-2 sm:grid-cols-2">
              {CAPABILITIES.map((c) => (
                <label key={c.id} className="flex items-center gap-2.5 py-1">
                  <input
                    type="checkbox"
                    className="h-4 w-4 accent-[var(--color-primary)]"
                    checked={caps.has(c.id)}
                    onChange={() => toggleCap(c.id)}
                  />
                  <span className="text-[14px] text-body">{c.labelEn}</span>
                </label>
              ))}
            </div>
          </div>
        )}
      </div>

      <div className="mt-5 flex items-center justify-end gap-2 border-t border-hairline pt-4">
        <button className="btn btn-outline btn-sm" onClick={onClose} disabled={saving}>
          {t("common.cancel")}
        </button>
        <button className="btn btn-primary btn-sm" disabled={saving || !name.trim()} onClick={() => void submit()}>
          {saving ? "…" : t("common.save")}
        </button>
      </div>
    </Modal>
  );
}
