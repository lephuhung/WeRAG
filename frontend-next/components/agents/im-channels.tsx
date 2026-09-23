/* Ported from frontend/src/components/IMChannelPanel.vue (Telegram-focused
 * subset + the shared channel mechanics: mode/output/session chips, webhook
 * callback URL display, enable toggle). Credential field sets per platform
 * follow the backend reader in internal/im/types.go + per-platform factories:
 *   telegram: bot_token (+ secret_token for webhook)
 *   slack:    webhook → bot_token + signing_secret; websocket → app_token + bot_token
 *   feishu/lark: app_id + app_secret
 */
"use client";

import { useCallback, useEffect, useState } from "react";
import { SlidePanel, SlidePanelHeader } from "@/components/slide-panel";
import { Modal } from "@/components/modal";
import { Toggle } from "@/components/settings/toggle";
import { Select } from "@/components/select";
import { copyToClipboard } from "@/lib/clipboard";
import { useT } from "@/lib/i18n";
import {
  listIMChannels,
  createIMChannel,
  updateIMChannel,
  deleteIMChannel,
  toggleIMChannel,
  type IMChannel,
} from "@/lib/api/agents";
import { listKnowledgeBases } from "@/lib/api/knowledge";
import type { KnowledgeBaseRow } from "@/lib/api/knowledge";

type Platform = IMChannel["platform"];

const PLATFORMS: { value: Platform; label: string; consoleUrl?: string }[] = [
  { value: "telegram", label: "Telegram", consoleUrl: "https://t.me/BotFather" },
  { value: "slack", label: "Slack", consoleUrl: "https://api.slack.com/apps" },
  { value: "feishu", label: "Feishu", consoleUrl: "https://open.feishu.cn/" },
  { value: "lark", label: "Lark", consoleUrl: "https://open.larksuite.com/" },
];

const THREAD_PLATFORMS: Platform[] = ["slack", "feishu", "lark", "telegram"];

function supportsThread(p: Platform) {
  return THREAD_PLATFORMS.includes(p);
}

function webhookOnlyCreds(p: Platform): boolean {
  /* telegram webhook adds the optional secret_token */
  return p === "telegram";
}

export function AgentIMChannels({ agentId, open, onClose, agentName }: {
  agentId: string;
  open: boolean;
  onClose: () => void;
  agentName: string;
}) {
  const { t } = useT();
  const [channels, setChannels] = useState<IMChannel[]>([]);
  const [loading, setLoading] = useState(open);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<IMChannel | null>(null);
  const [creating, setCreating] = useState(false);
  const [removing, setRemoving] = useState<IMChannel | null>(null);

  const load = useCallback(() => {
    listIMChannels(agentId)
      .then((res) => setChannels(res.data ?? []))
      .catch((e) => setError(e instanceof Error ? e.message : "Failed to load channels"));
  }, [agentId]);

  useEffect(() => {
    if (!open) return;
    setLoading(true);
    load();
    setLoading(false);
  }, [open, load]);

  const toggle = async (c: IMChannel) => {
    try {
      const next = (await toggleIMChannel(c.id)).data;
      setChannels((prev) => prev.map((x) => (x.id === c.id ? { ...x, enabled: next.enabled } : x)));
    } catch (e) {
      setError(e instanceof Error ? e.message : "Toggle failed");
    }
  };

  return (
    <SlidePanel open={open} onClose={onClose} label={agentName} width="w-[560px]">
      <div className="flex min-h-0 flex-1 flex-col">
        <div className="mb-4 flex items-center justify-between px-5 pt-5">
          <h2 className="title-md">
            {`${agentName} — ${t("agentEditor.im.title")}`}
          </h2>
          <button className="btn btn-primary btn-sm" onClick={() => setCreating(true)}>
            {t("agentEditor.im.addChannel")}
          </button>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto px-5 pb-5">
          {error && <p className="caption mb-3 text-error">{error}</p>}
          {loading && <p className="caption text-muted">…</p>}
          {(channels ?? []).map((c) => (
            <div key={c.id} className="border-b border-hairline py-3">
              <div className="flex items-center gap-3">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="badge-pill">{c.platform}</span>
                    <span className="truncate text-[14px] font-medium text-ink">{c.name}</span>
                  </div>
                  <div className="caption mt-0.5 text-muted">
                    {c.mode} · {c.output_mode === "stream" ? t("agentEditor.im.outputStream") : t("agentEditor.im.outputFull")}
                    {c.session_mode ? ` · ${c.session_mode}` : ""}
                  </div>
                </div>
                <Toggle
                  checked={c.enabled}
                  onChange={() => void toggle(c)}
                  label={t("agentEditor.im.toggle")}
                />
                <button className="btn btn-tertiary btn-sm text-[13px]" onClick={() => setEditing(c)}>
                  {t("common.edit")}
                </button>
                <button
                  className="btn btn-tertiary btn-sm text-[13px] text-error"
                  onClick={() => setRemoving(c)}
                >
                  {t("common.delete")}
                </button>
              </div>
              {c.mode === "webhook" && (
                <div className="mt-1 flex items-center gap-2">
                  <span className="caption text-muted">{t("agentEditor.im.callbackUrl")}:</span>
                  <code className="caption min-w-0 flex-1 overflow-x-auto text-ink">
                    {`${window.location.origin}/api/v1/im/callback/${c.id}`}
                  </code>
                  <button
                    className="btn btn-tertiary btn-sm"
                    onClick={() => void copyToClipboard(`${window.location.origin}/api/v1/im/callback/${c.id}`)}
                  >
                    {t("integrations.copy")}
                  </button>
                </div>
              )}
            </div>
          ))}
          {!loading && channels.length === 0 && !error && (
            <p className="caption text-muted-soft">{t("agentEditor.im.empty")}</p>
          )}
        </div>
      </div>

      {creating && (
        <IMChannelForm
          agentId={agentId}
          channel={null}
          onClose={() => setCreating(false)}
          onSaved={() => {
            setCreating(false);
            load();
          }}
        />
      )}
      {editing && (
        <IMChannelForm
          agentId={agentId}
          channel={editing}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null);
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
          {t("agentEditor.im.deleteBody").replace("{name}", removing?.name ?? "")}
        </p>
        <div className="mt-4 flex justify-end gap-2">
          <button className="btn btn-outline btn-sm" onClick={() => setRemoving(null)}>
            {t("common.cancel")}
          </button>
          <button
            className="btn btn-sm bg-[var(--color-error)] text-white"
            onClick={() => {
              void deleteIMChannel(removing!.id).then(() => {
                setRemoving(null);
                load();
              });
            }}
          >
            {t("common.delete")}
          </button>
        </div>
      </Modal>
    </SlidePanel>
  );
}

function IMChannelForm({ agentId, channel, onClose, onSaved }: {
  agentId: string;
  channel: IMChannel | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const { t } = useT();
  const [platform, setPlatform] = useState<Platform>(channel?.platform ?? "telegram");
  const [name, setName] = useState(channel?.name ?? "");
  const [mode, setMode] = useState<IMChannel["mode"]>(channel?.mode ?? "websocket");
  const [outputMode, setOutputMode] = useState<IMChannel["output_mode"]>(channel?.output_mode ?? "stream");
  const [sessionMode, setSessionMode] = useState<"user" | "thread">(channel?.session_mode ?? "user");
  const [kbId, setKbId] = useState<string>(channel?.knowledge_base_id ?? "");
  const [creds, setCreds] = useState<Record<string, string>>(()=>{
    const out: Record<string, string> = {};
    for (const [k, v] of Object.entries(channel?.credentials ?? {})) out[k] = String(v ?? "");
    return out;
  });
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [kbs, setKbs] = useState<KnowledgeBaseRow[]>([]);

  useEffect(() => {
    listKnowledgeBases()
      .then((rows) => setKbs(rows))
      .catch(() => setKbs([]));
  }, []);

  const webhookForTelegram = platform === "telegram" && mode === "webhook";
  const slackWebhook = platform === "slack" && mode === "webhook";

  const submit = async () => {
    if (!name.trim()) return;
    setSaving(true);
    setError("");
    try {
      const payload: Partial<IMChannel> = {
        platform,
        name: name.trim(),
        mode,
        output_mode: outputMode,
        session_mode: sessionMode,
        knowledge_base_id: kbId || undefined,
        credentials: creds,
      };
      if (channel) await updateIMChannel(channel.id, payload);
      else await createIMChannel(agentId, payload);
      onSaved();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Save failed");
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal
      open
      title={channel ? t("agentEditor.im.editChannel") : t("agentEditor.im.addChannel")}
      onClose={onClose}
      width="w-[560px]"
    >
      <div className="flex max-h-[70vh] flex-col gap-4 overflow-y-auto pr-1">
        {error && <p className="caption text-error">{error}</p>}

        <div className="flex gap-4">
          <label className="block flex-1">
            <span className="caption mb-1.5 block text-muted">{t("agentEditor.im.platform")}</span>
            <Select
              value={platform}
              disabled={Boolean(channel)}
              onChange={(v) => {
                const p = v as Platform;
                setPlatform(p);
                if (p === "dingtalk") setMode("websocket");
              }}
              options={PLATFORMS.map((p) => ({ value: p.value, label: p.label }))}
            />
          </label>
          <label className="block flex-1">
            <span className="caption mb-1.5 block text-muted">{t("agentEditor.im.channelName")}</span>
            <input className="input" value={name} onChange={(e) => setName(e.target.value)} />
          </label>
        </div>

        {/* access */}
        <div className="border-t border-hairline pt-4">
          <div className="caption-uppercase mb-2 text-muted">{t("agentEditor.im.sectionAccess")}</div>
          <div className="mb-3 flex gap-4">
            <span className="title-sm flex items-center">{t("agentEditor.im.mode")}</span>
            <div className="flex gap-1.5">
              <Chip active={mode === "websocket"} onClick={() => setMode("websocket")}>
                {platform === "dingtalk" ? "Stream" : "WebSocket"}
              </Chip>
              <Chip
                active={mode === "webhook"}
                disabled={platform === "dingtalk" || platform === "qqbot"}
                onClick={() => setMode("webhook")}
              >
                Webhook
              </Chip>
            </div>
          </div>
          <div className="flex gap-4">
            <span className="title-sm flex items-center">{t("agentEditor.im.outputMode")}</span>
            <Chip active={outputMode === "stream"} onClick={() => setOutputMode("stream")}>
              {t("agentEditor.im.outputStream")}
            </Chip>
            <Chip active={outputMode === "full"} onClick={() => setOutputMode("full")}>
              {t("agentEditor.im.outputFull")}
            </Chip>
          </div>
        </div>

        {/* session */}
        <div className="border-t border-hairline pt-4">
          <div className="caption-uppercase mb-2 text-muted">{t("agentEditor.im.sectionSession")}</div>
          <div className="flex gap-4">
            <span className="title-sm flex items-center">{t("agentEditor.im.sessionMode")}</span>
            <Chip active={sessionMode === "user"} onClick={() => setSessionMode("user")}>
              {t("agentEditor.im.sessionModeUser")}
            </Chip>
            <Chip active={sessionMode === "thread"} disabled={!supportsThread(platform)} onClick={() => setSessionMode("thread")}>
              {t("agentEditor.im.sessionModeThread")}
            </Chip>
          </div>
        </div>

        {/* callback URL on edit + webhook */}
        {channel && mode === "webhook" && (
          <div className="border-t border-hairline pt-4">
            <div className="caption-uppercase mb-2 text-muted">{t("agentEditor.im.sectionCallback")}</div>
            <code className="block overflow-x-auto rounded-[10px] bg-surface-strong px-3 py-2 font-mono text-[13px] text-ink">
              {`${window.location.origin}/api/v1/im/callback/${channel.id}`}
            </code>
          </div>
        )}

        {/* credentials */}
        <div className="border-t border-hairline pt-4">
          <div className="caption-uppercase mb-2 text-muted">{t("agentEditor.im.sectionCredentials")}</div>
          {PLATFORMS.find((p) => p.value === platform)?.consoleUrl && (
            <p className="caption mb-2">
              <a
                href={PLATFORMS.find((p) => p.value === platform)!.consoleUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="text-primary underline underline-offset-2"
              >
                {t("agentEditor.im.consoleLink")}
              </a>
              <span className="ml-2 text-muted">{t("agentEditor.im.consoleTip")}</span>
            </p>
          )}
          {(platform === "telegram" || platform === "slack") && (
            <>
              <label className="mb-3 block">
                <span className="caption mb-1.5 block text-muted">Bot Token</span>
                <input
                  className="input"
                  type="password"
                  placeholder={platform === "telegram" ? "123456789:AABBccdd…" : "xoxb-…"}
                  value={creds.bot_token ?? ""}
                  onChange={(e) => setCreds({ ...creds, bot_token: e.target.value })}
                />
              </label>
              {slackWebhook && (
                <label className="mb-3 block">
                  <span className="caption mb-1.5 block text-muted">Signing Secret</span>
                  <input
                    className="input"
                    type="password"
                    value={creds.signing_secret ?? ""}
                    onChange={(e) => setCreds({ ...creds, signing_secret: e.target.value })}
                  />
                </label>
              )}
              {webhookForTelegram && (
                <label className="block">
                  <span className="caption mb-1.5 block text-muted">Secret Token</span>
                  <input
                    className="input"
                    type="password"
                    placeholder={t("agentEditor.im.optional")}
                    value={creds.secret_token ?? ""}
                    onChange={(e) => setCreds({ ...creds, secret_token: e.target.value })}
                  />
                </label>
              )}
            </>
          )}
          {(platform === "feishu" || platform === "lark") && (
            <>
              <label className="mb-3 block">
                <span className="caption mb-1.5 block text-muted">App ID</span>
                <input className="input" value={creds.app_id ?? ""} onChange={(e) => setCreds({ ...creds, app_id: e.target.value })} />
              </label>
              <label className="block">
                <span className="caption mb-1.5 block text-muted">App Secret</span>
                <input className="input" type="password" value={creds.app_secret ?? ""} onChange={(e) => setCreds({ ...creds, app_secret: e.target.value })} />
              </label>
            </>
          )}
        </div>

        {/* KB scope */}
        <div className="border-t border-hairline pt-4">
          <div className="caption-uppercase mb-2 text-muted">{t("agentEditor.im.sectionKnowledge")}</div>
          <label className="block">
            <span className="caption mb-1.5 block text-muted">{t("agentEditor.im.fileKnowledgeBase")}</span>
            <Select
              value={kbId}
              onChange={setKbId}
              placeholder={t("agentEditor.im.useOwnKB")}
              options={[
                { value: "", label: t("agentEditor.im.useOwnKB") },
                ...kbs.map((kb) => ({ value: kb.id, label: kb.name })),
              ]}
            />
          </label>
        </div>
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

function Chip({ active, disabled, onClick, children }: {
  active: boolean;
  disabled?: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onClick}
      className={`h-8 rounded-full border px-3.5 text-[13px] font-medium transition-colors ${
        active
          ? "border-primary bg-primary text-on-primary"
          : "border-hairline-strong bg-surface-card text-body hover:border-ink hover:text-ink"
      } ${disabled ? "cursor-not-allowed opacity-40" : ""}`}
    >
      {children}
    </button>
  );
}
