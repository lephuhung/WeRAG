/* IM channels — tenant-wide overview of every messaging-app channel
 * (Telegram) bound to any agent. Read-only overview with
 * quick enable toggle; create/edit/delete reuse the per-agent
 * AgentIMChannels slide panel, so all channel mechanics live in one place. */
"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { Modal } from "@/components/modal";
import { IconPlus, IconRefresh } from "@/components/icons";
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
import { IMPlatformIcon } from "@/components/agents/im-platform-icon";
import { ConsolePage } from "@/components/settings/console-page";
import { Loading } from "@/components/settings/loading";
import { Notice } from "@/components/settings/notice";

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
    <ConsolePage size="form">
      {/* Header controls — hidden entirely while the create form is open,
          so there's only one "Thêm kênh" on screen at a time. */}
      {!createOpen && (
        <div className="mb-5 space-y-5">
          <section className="rounded-2xl border border-info/30 bg-info-surface p-5 sm:p-6">
            <div className="flex flex-wrap items-start gap-4">
              <div className="flex h-14 w-14 shrink-0 items-center justify-center rounded-2xl bg-surface-strong text-info shadow-sm">
                <IMPlatformIcon platform="telegram" className="h-8 w-8" />
              </div>
              <div className="min-w-0 flex-1 basis-52">
                <h3 className="text-xl font-semibold tracking-tight text-ink">Telegram</h3>
                <p className="mt-2 max-w-xl text-[13px] leading-relaxed text-muted">{t("imp.subtitle")}</p>
                {!loading && <p className="caption mt-3 inline-flex items-center gap-2 text-muted">
                  <span className={`h-1.5 w-1.5 rounded-full ${channels.some((c) => c.enabled) ? "bg-success" : "bg-muted-soft"}`} aria-hidden="true" />
                  {t("imp.activeChannels", { active: channels.filter((c) => c.enabled).length, total: channels.length })}
                </p>}
              </div>
              <button className="btn btn-primary btn-sm w-full shrink-0 sm:w-auto" onClick={() => setCreateOpen(true)}>
                <IconPlus className="h-3.5 w-3.5" /> {t("agentEditor.im.addChannel")}
              </button>
            </div>
          </section>
          <div className="flex flex-wrap items-center gap-2">
            {agents.length > 1 && (
              <Select
                id="im-agent-filter"
                ariaLabel={t("imp.allAgents")}
                className="w-full sm:w-[240px]"
                value={agentFilter}
                onChange={setAgentFilter}
                options={[
                  { value: "", label: t("imp.allAgents") },
                  ...agents.map((a) => ({ value: a.id, label: a.name })),
                ]}
              />
            )}
            <button className="btn btn-outline btn-sm ml-auto" onClick={load} disabled={loading}>
              <IconRefresh className={`h-3.5 w-3.5 ${loading ? "animate-spin motion-reduce:animate-none" : ""}`} />
              {t("common.refresh")}
            </button>
          </div>
        </div>
      )}

      {error && (
        <Notice
          tone="error"
          className="mb-4"
          action={
            <button type="button" className="btn btn-outline btn-sm" onClick={load}>
              {t("common.retry")}
            </button>
          }
        >
          {error}
        </Notice>
      )}
      {loading && <Loading />}
      {!createOpen && !loading && filtered.length === 0 && !error && (
        <div className="rounded-2xl border border-dashed border-hairline bg-surface-card px-5 py-12 text-center">
          <div className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-info-surface text-info">
            <IMPlatformIcon platform="telegram" className="h-6 w-6" />
          </div>
          <h4 className="text-[15px] font-medium text-ink">{t("imp.emptyTitle")}</h4>
          <p className="mx-auto mt-2 max-w-md text-[13px] leading-relaxed text-muted">{t(channels.length ? "imp.noMatches" : "imp.empty")}</p>
        </div>
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
                : "border-hairline/60 bg-surface-card/60 opacity-80"
            }`}
          >
            <div className="flex items-start justify-between gap-3">
              <div className="flex min-w-0 items-center gap-2.5">
                <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-info-surface text-info">
                  <IMPlatformIcon platform={c.platform} className="h-5 w-5" />
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
          <button type="button" className="btn btn-danger btn-sm" onClick={remove}>
            {t("common.delete")}
          </button>
        </div>
      </Modal>
    </ConsolePage>
  );
}
