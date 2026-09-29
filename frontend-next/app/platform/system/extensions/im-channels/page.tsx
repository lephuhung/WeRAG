/* IM channels — tenant-wide overview of every messaging-app channel
 * (Telegram, Slack, Feishu…) bound to any agent. Read-only overview with
 * quick enable toggle; create/edit/delete reuse the per-agent
 * AgentIMChannels slide panel, so all channel mechanics live in one place. */
"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { Modal } from "@/components/modal";
import { IconChat, IconPlus, IconRefresh } from "@/components/icons";
import {
  listAllIMChannels,
  listAgents,
  toggleIMChannel,
  deleteIMChannel,
  type IMChannelOverview,
} from "@/lib/api/agents";
import { Toggle } from "@/components/settings/toggle";
import { Select } from "@/components/select";
import { RequireSystemAccess } from "@/components/require-system-access";
import { useInSettingsModal } from "@/components/system/in-modal-nav";
import { useT } from "@/lib/i18n";
import { AgentIMChannels, IMChannelInlineForm } from "@/components/agents/im-channels";

export default function IMChannelsPage() {
  return (
    <RequireSystemAccess minRole="system">
      <IMChannelsPanel />
    </RequireSystemAccess>
  );
}

function IMChannelsPanel() {
  const inModal = useInSettingsModal();
  const { t } = useT();
  const [channels, setChannels] = useState<IMChannelOverview[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [agentFilter, setAgentFilter] = useState("");
  const [panelAgent, setPanelAgent] = useState<{ id: string; name: string } | null>(null);
  const [createOpen, setCreateOpen] = useState(false);
  const [removing, setRemoving] = useState<IMChannelOverview | null>(null);

  /* Agent list comes from the agents API, not inferred from channels — a
   * fresh workspace has agents but zero channels. */
  const [agentNames, setAgentNames] = useState<Record<string, string>>({});

  const load = useCallback(() => {
    setLoading(true);
    setError("");
    listAllIMChannels()
      .then((res) => setChannels(res.data ?? []))
      .catch((e) => setError(e instanceof Error ? e.message : t("imp.loadFailed")))
      .finally(() => setLoading(false));
    listAgents()
      .then((res) => {
        const map: Record<string, string> = {};
        for (const a of res.data ?? []) map[a.id] = a.name;
        setAgentNames(map);
      })
      .catch(() => setAgentNames({}));
  }, [t]);

  useEffect(() => {
    load();
  }, [load]);

  const agentName = (id: string) => agentNames[id] || id;

  const agents = useMemo(
    () =>
      [...new Set([...Object.keys(agentNames), ...channels.map((c) => c.agent_id)])]
        .map((id) => ({ id, name: agentName(id) }))
        .sort((a, b) => a.name.localeCompare(b.name)),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [agentNames, channels],
  );

  const filtered = agentFilter
    ? channels.filter((c) => c.agent_id === agentFilter)
    : channels;

  const toggle = async (c: IMChannelOverview) => {
    try {
      const next = (await toggleIMChannel(c.id)).data;
      setChannels((prev) => prev.map((x) => (x.id === c.id ? { ...x, enabled: next.enabled } : x)));
    } catch (e) {
      setError(e instanceof Error ? e.message : t("imp.toggleFailed"));
    }
  };

  const remove = () => {
    if (!removing) return;
    deleteIMChannel(removing.id)
      .then(() => {
        setRemoving(null);
        load();
      })
      .catch((e) => setError(e instanceof Error ? e.message : t("imp.deleteFailed")));
  };

  return (
    <div>
      <div className="mb-6 flex flex-wrap items-center justify-between gap-4">
        <p className="caption max-w-[560px] text-muted">{t("imp.subtitle")}</p>
        <div className="flex items-center gap-2">
          {/* Hidden while the create form is open — the form carries the
              (single) agent picker for that moment. */}
          {agents.length > 1 && !createOpen && (
            <Select
              className="w-[200px]"
              value={agentFilter}
              onChange={setAgentFilter}
              options={[
                { value: "", label: t("imp.allAgents") },
                ...agents.map((a) => ({ value: a.id, label: a.name })),
              ]}
            />
          )}
          <button className="btn btn-outline btn-sm" onClick={load} disabled={loading}>
            <IconRefresh className={`h-3.5 w-3.5 ${loading ? "animate-spin" : ""}`} />
            {t("common.refresh")}
          </button>
          <button className="btn btn-primary btn-sm" onClick={() => setCreateOpen(true)}>
            <IconPlus className="h-3.5 w-3.5" /> {t("agentEditor.im.addChannel")}
          </button>
        </div>
      </div>

      {error && <p className="caption mb-4 text-error">{error}</p>}
      {loading && <p className="caption text-muted">…</p>}
      {!loading && filtered.length === 0 && !error && (
        <p className="caption text-muted-soft">{t("imp.empty")}</p>
      )}

      <div
        className={`grid grid-cols-1 gap-4 ${inModal ? "" : "lg:grid-cols-2"}`}
      >
        {filtered.map((c) => (
          <div
            key={c.id}
            className={`rounded-xl border p-4.5 shadow-sm transition-all ${
              c.enabled
                ? "border-hairline bg-surface-card hover:border-hairline-strong hover:shadow-md"
                : "border-hairline/60 bg-surface/60 opacity-80"
            }`}
          >
            <div className="flex items-start justify-between gap-3">
              <div className="flex min-w-0 items-center gap-2.5">
                <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-surface-strong text-ink">
                  <IconChat className="h-5 w-5" />
                </div>
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="badge-pill shrink-0">{c.platform}</span>
                    <span className="truncate text-[14px] font-medium text-ink" title={c.name}>
                      {c.name}
                    </span>
                  </div>
                  <p className="caption mt-0.5 truncate text-muted">
                    {t("imp.agent")}: {agentName(c.agent_id)} · {c.mode} ·{" "}
                    {c.output_mode === "stream" ? t("agentEditor.im.outputStream") : t("agentEditor.im.outputFull")}
                    {c.session_mode ? ` · ${c.session_mode}` : ""}
                  </p>
                </div>
              </div>
              <Toggle
                checked={c.enabled}
                onChange={() => void toggle(c)}
                label={t("agentEditor.im.toggle")}
              />
            </div>

            {c.mode === "webhook" && c.bot_identity && (
              <p className="caption mt-2 truncate text-muted-soft" title={c.bot_identity}>
                {c.bot_identity}
              </p>
            )}

            <div className="mt-3 flex items-center justify-end gap-1 border-t border-hairline pt-3">
              <button
                className="btn btn-tertiary btn-sm text-[13px]"
                onClick={() => setPanelAgent({ id: c.agent_id, name: agentName(c.agent_id) })}
              >
                {t("common.edit")}
              </button>
              <button
                className="btn btn-tertiary btn-sm text-[13px] text-error"
                onClick={() => setRemoving(c)}
              >
                {t("common.delete")}
              </button>
            </div>
          </div>
        ))}
      </div>

      {/* Edit opens the per-agent panel (full CRUD + credential form). */}
      {panelAgent && (
        <AgentIMChannels
          agentId={panelAgent.id}
          agentName={panelAgent.name}
          open
          onClose={() => {
            setPanelAgent(null);
            load();
          }}
        />
      )}

      {/* Create opens as an inline card in the page content — no modal. */}
      {createOpen && (
        <IMChannelInlineForm
          agentId={agentFilter || undefined}
          agents={agents}
          channel={null}
          onClose={() => setCreateOpen(false)}
          onSaved={() => {
            setCreateOpen(false);
            load();
          }}
        />
      )}

      <Modal
        open={removing !== null}
        title={t("agentEditor.im.deleteTitle")}
        onClose={() => setRemoving(null)}
        width="w-[420px]"
      >
        <p className="body-sm text-body">
          {t("agentEditor.im.deleteBody", { name: removing?.name ?? "" })}
        </p>
        <div className="mt-4 flex justify-end gap-2">
          <button className="btn btn-outline btn-sm" onClick={() => setRemoving(null)}>
            {t("common.cancel")}
          </button>
          <button className="btn btn-sm bg-[var(--color-error)] text-white" onClick={remove}>
            {t("common.delete")}
          </button>
        </div>
      </Modal>
    </div>
  );
}
