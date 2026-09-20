/* Ported from frontend/src/components/AgentEmbedChannelPanel.vue
 * (channel list + create/edit drawer + preview-lite + publish token reveal).
 * Fields mirror the payload the Vue builder assembles for
 * createEmbedChannel/updateEmbedChannel.
 */
"use client";

import { useCallback, useEffect, useState } from "react";
import { SlidePanel, SlidePanelHeader } from "@/components/slide-panel";
import { Modal } from "@/components/modal";
import { useT } from "@/lib/i18n";
import {
  listEmbedChannels,
  createEmbedChannel,
  updateEmbedChannel,
  rotateEmbedToken,
  deleteEmbedChannel,
  type EmbedChannel,
  type HeaderTitleMode,
  type WidgetPosition,
} from "@/lib/api/embed";

const LOCALES: { value: string; labelEn: string }[] = [
  { value: "", labelEn: "Follow browser" },
  { value: "en-US", labelEn: "English" },
  { value: "vi-VN", labelEn: "Tiếng Việt" },
  { value: "zh-CN", labelEn: "简体中文" },
  { value: "ko-KR", labelEn: "한국어" },
  { value: "ja-JP", labelEn: "日本語" },
  { value: "ru-RU", labelEn: "Русский" },
];

const POSITIONS: WidgetPosition[] = ["bottom-right", "bottom-left", "top-right", "top-left"];
const TITLE_MODES: HeaderTitleMode[] = ["channel", "session"];

type Draft = {
  name: string;
  welcome_message: string;
  allowed_origins: string;
  rate_limit_per_minute: number;
  rate_limit_per_day: number;
  primary_color: string;
  page_title: string;
  header_title_mode: HeaderTitleMode;
  show_suggested_questions: boolean;
  widget_position: WidgetPosition;
  allow_web_search: boolean;
  allow_file_upload: boolean;
  default_locale: string;
  webhook_url: string;
  webhook_secret: string;
};

function draftFrom(c: EmbedChannel | null): Draft {
  return {
    name: c?.name ?? "",
    welcome_message: c?.welcome_message ?? "",
    allowed_origins: (c?.allowed_origins ?? []).join("\n"),
    rate_limit_per_minute: c?.rate_limit_per_minute ?? 10,
    rate_limit_per_day: c?.rate_limit_per_day ?? 1000,
    primary_color: c?.primary_color ?? "",
    page_title: c?.page_title ?? "",
    header_title_mode: c?.header_title_mode ?? "channel",
    show_suggested_questions: c?.show_suggested_questions ?? false,
    widget_position: c?.widget_position ?? "bottom-right",
    allow_web_search: c?.allow_web_search ?? false,
    allow_file_upload: c?.allow_file_upload ?? false,
    default_locale: c?.default_locale ?? "",
    webhook_url: c?.webhook_url ?? "",
    webhook_secret: "",
  };
}

function parseOrigins(raw: string): { ok: boolean; origins: string[] } {
  const origins = raw
    .split(/[\n,]/)
    .map((x) => x.trim())
    .filter(Boolean);
  return { ok: origins.length > 0, origins };
}

export function AgentEmbedChannels({ agentId, open, onClose, agentName }: {
  agentId: string;
  open: boolean;
  onClose: () => void;
  agentName: string;
}) {
  const { t } = useT();
  const [channels, setChannels] = useState<EmbedChannel[]>([]);
  const [editing, setEditing] = useState<EmbedChannel | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [creating, setCreating] = useState(false);
  const [removing, setRemoving] = useState<EmbedChannel | null>(null);
  const [revealed, setRevealed] = useState<Set<string>>(new Set());

  const load = useCallback(() => {
    setLoading(true);
    listEmbedChannels(agentId)
      .then((res) => setChannels(res.data ?? []))
      .catch((e) => setError(e instanceof Error ? e.message : "Failed to load channels"))
      .finally(() => setLoading(false));
  }, [agentId]);

  useEffect(() => {
    if (open) {
      load();
      return () => {
        /* clearing will happen on unmount */
      };
    }
    return undefined;
  }, [open, load]);

  const reveal = useCallback((c: EmbedChannel) => {
    const token = c.publish_token ?? "";
    if (!token) return;
    setRevealed((prev) => {
      const next = new Set(prev);
      if (next.has(c.id)) next.delete(c.id);
      else next.add(c.id);
      return next;
    });
  }, []);

  const masked = (c: EmbedChannel, r: boolean) => (r ? c.publish_token || "" : "••••••••••••");

  return (
    <SlidePanel open={open} onClose={onClose} label={agentName} width="w-[620px]">
      <SlidePanelHeader title={`${agentName} — ${t("embedPublish.title")}`} onClose={onClose} />
      <div className="flex min-h-0 flex-1 flex-col px-5 pb-5">
        {error && <p className="caption mb-3 text-error">{error}</p>}
        {loading && <p className="caption text-muted">…</p>}
        <div className="min-h-0 flex-1 overflow-y-auto">
          {(channels ?? []).map((c) => (
            <div key={c.id} className="border-b border-hairline py-3.5">
              <div className="flex items-center gap-3">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="truncate text-[14px] font-medium text-ink">{c.name}</span>
                    <span className="badge-pill">{c.enabled ? t("common.on") : t("common.off")}</span>
                  </div>
                  <div className="caption mt-0.5 text-muted">
                    {(c.allowed_origins ?? []).length} origins · {c.rate_limit_per_minute}/min
                  </div>
                </div>
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
              <div className="mt-1 flex items-center gap-2">
                <span className="caption text-muted">{t("embedPublish.publishToken")}:</span>
                <code className="caption min-w-0 flex-1 truncate text-ink">
                  {masked(c, revealed.has(c.id))}
                </code>
                <button
                  className="btn btn-tertiary btn-sm text-muted"
                  onClick={() => reveal(c)}
                  aria-label={t("embedPublish.revealKey")}
                >
                  {revealed.has(c.id) ? "🙈" : "👁"}
                </button>
                <button
                  className="btn btn-tertiary btn-sm text-[13px]"
                  onClick={() => {
                    void rotateEmbedToken(c.id).then((res) => {
                      const fresh = (res as { data?: EmbedChannel }).data;
                      if (fresh?.publish_token) {
                        setChannels((prev) => prev.map((x) => (x.id === fresh.id ? fresh : x)));
                        setRevealed((prev) => new Set(prev).add(fresh.id));
                      }
                    });
                  }}
                >
                  {t("embedPublish.rotateToken")}
                </button>
              </div>
            </div>
          ))}
          {channels !== null && channels.length === 0 && !loading && (
            <p className="caption text-muted-soft">No embed channels for this agent yet.</p>
          )}
        </div>

        <div className="mt-3">
          <button className="btn btn-primary btn-sm" onClick={() => setCreating(true)}>
            {t("embedPublish.createTitle")}
          </button>
        </div>
      </div>

      {(creating || editing) && (
        <EmbedChannelForm
          agentId={agentId}
          channel={creating ? null : editing}
          onClose={() => {
            setCreating(false);
            setEditing(null);
          }}
          onSaved={() => {
            setCreating(false);
            setEditing(null);
            load();
          }}
        />
      )}

      <Modal
        open={removing !== null}
        title={t("embedPublish.deleteTitle")}
        onClose={() => setRemoving(null)}
        width="w-[420px]"
      >
        <p className="body-sm text-body">
          {t("embedPublish.deleteBody").replace("{name}", removing?.name ?? "")}
        </p>
        <div className="mt-4 flex justify-end gap-2">
          <button className="btn btn-outline btn-sm" onClick={() => setRemoving(null)}>
            {t("common.cancel")}
          </button>
          <button
            className="btn btn-sm bg-[var(--color-error)] text-white"
            onClick={() => {
              void deleteEmbedChannel(removing!.id).then(() => {
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

function EmbedChannelForm({ agentId, channel, onClose, onSaved }: {
  agentId: string;
  channel: EmbedChannel | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const { t } = useT();
  const [draft, setDraft] = useState<Draft>(() => draftFrom(channel));
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  const patch = <K extends keyof Draft>(k: K, v: Draft[K]) =>
    setDraft((d) => ({ ...d, [k]: v }));

  const submit = async () => {
    const origins = parseOrigins(draft.allowed_origins);
    if (!origins.ok) {
      setError("At least one allowed origin is required");
      return;
    }
    if (origins.origins.includes("*")) {
      setError("Wildcard origin '*' is not allowed");
      return;
    }
    setSaving(true);
    setError("");
    try {
      const payload: Partial<EmbedChannel> = {
        name: draft.name.trim() || `Embed ${agentId}`,
        welcome_message: draft.welcome_message,
        allowed_origins: origins.origins,
        rate_limit_per_minute: draft.rate_limit_per_minute,
        ...(draft.rate_limit_per_day ? { rate_limit_per_day: draft.rate_limit_per_day } : {}),
        primary_color: draft.primary_color || undefined,
        page_title: draft.page_title || undefined,
        header_title_mode: draft.header_title_mode,
        show_suggested_questions: draft.show_suggested_questions,
        widget_position: draft.widget_position,
        allow_web_search: draft.allow_web_search,
        allow_file_upload: draft.allow_file_upload,
        default_locale: draft.default_locale,
        webhook_url: draft.webhook_url || "",
        ...(draft.webhook_secret ? { webhook_secret: draft.webhook_secret } : {}),
        enabled: channel?.enabled ?? true,
        agent_id: agentId,
      };
      if (channel) await updateEmbedChannel(channel.id, payload);
      else await createEmbedChannel(agentId, payload);
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
      title={channel ? t("embedPublish.editTitle") : t("embedPublish.createTitle")}
      onClose={onClose}
      width="w-[640px]"
    >
      <div className="flex max-h-[70vh] flex-col gap-4 overflow-y-auto pr-1">
        {error && <p className="caption text-error">{error}</p>}
        <div className="flex gap-4">
          <label className="block flex-1">
            <span className="caption mb-1.5 block text-muted">{t("embedPublish.nameLabel")}</span>
            <input className="input" value={draft.name} onChange={(e) => patch("name", e.target.value)} />
          </label>
          <label className="block w-[180px]">
            <span className="caption mb-1.5 block text-muted">Rate / minute</span>
            <input
              className="input"
              type="number"
              min={1}
              max={600}
              value={draft.rate_limit_per_minute}
              onChange={(e) => patch("rate_limit_per_minute", Number(e.target.value))}
            />
          </label>
          <label className="block w-[180px]">
            <span className="caption mb-1.5 block text-muted">Rate / day</span>
            <input
              className="input"
              type="number"
              min={1}
              value={draft.rate_limit_per_day}
              onChange={(e) => patch("rate_limit_per_day", Number(e.target.value))}
            />
          </label>
        </div>

        <label className="block">
          <span className="caption mb-1.5 block text-muted">{t("embedPublish.allowedOrigins")}</span>
          <textarea
            className="input h-auto min-h-[72px] resize-y font-mono text-[13px]"
            value={draft.allowed_origins}
            placeholder={"https://app.example.com\nhttps://*.example.com"}
            onChange={(e) => patch("allowed_origins", e.target.value)}
          />
        </label>

        <label className="block">
          <span className="caption mb-1.5 block text-muted">{t("embedPublish.welcomeMessage")}</span>
          <textarea
            className="input h-auto min-h-[64px] resize-y"
            value={draft.welcome_message}
            onChange={(e) => patch("welcome_message", e.target.value)}
          />
        </label>

        <div className="flex gap-6">
          <ToggleRow label={t("embedPublish.showSuggested")} checked={draft.show_suggested_questions} onChange={(v) => patch("show_suggested_questions", v)} />
        </div>
        <div className="flex gap-6">
          <ToggleRow label={t("embedPublish.allowWebSearch")} checked={draft.allow_web_search} onChange={(v) => patch("allow_web_search", v)} />
          <ToggleRow label={t("embedPublish.allowFileUpload")} checked={draft.allow_file_upload} onChange={(v) => patch("allow_file_upload", v)} />
        </div>

        <div className="flex gap-4">
          <label className="block flex-1">
            <span className="caption mb-1.5 block text-muted">{t("embedPublish.pageTitle")}</span>
            <input className="input" value={draft.page_title} onChange={(e) => patch("page_title", e.target.value)} />
          </label>
          <label className="block w-[200px]">
            <span className="caption mb-1.5 block text-muted">{t("embedPublish.headerTitleMode")}</span>
            <select
              className="input"
              value={draft.header_title_mode}
              onChange={(e) => patch("header_title_mode", e.target.value as HeaderTitleMode)}
            >
              {TITLE_MODES.map((m) => (
                <option key={m} value={m}>{m}</option>
              ))}
            </select>
          </label>
          <label className="block w-[220px]">
            <span className="caption mb-1.5 block text-muted">{t("embedPublish.widgetPosition")}</span>
            <select
              className="input"
              value={draft.widget_position}
              onChange={(e) => patch("widget_position", e.target.value as WidgetPosition)}
            >
              {POSITIONS.map((p) => (
                <option key={p} value={p}>{p}</option>
              ))}
            </select>
          </label>
        </div>

        <div className="flex gap-4">
          <label className="block flex-1">
            <span className="caption mb-1.5 block text-muted">{t("embedPublish.defaultLocale")}</span>
            <select
              className="input"
              value={draft.default_locale}
              onChange={(e) => patch("default_locale", e.target.value)}
            >
              {LOCALES.map((l) => (
                <option key={l.value} value={l.value}>{l.labelEn}</option>
              ))}
            </select>
          </label>
          <label className="block w-[160px]">
            <span className="caption mb-1.5 block text-muted">{t("embedPublish.primaryColor")}</span>
            <input
              className="input"
              type="color"
              value={draft.primary_color || "#292524"}
              onChange={(e) => patch("primary_color", e.target.value)}
            />
          </label>
        </div>

        <div className="border-t border-hairline pt-4">
          <div className="caption-uppercase mb-2 text-muted">{t("embedPublish.sectionWebhook")}</div>
          <label className="mb-3 block">
            <span className="caption mb-1.5 block text-muted">{t("embedPublish.webhookUrl")}</span>
            <input
              className="input"
              value={draft.webhook_url}
              placeholder="https://…"
              onChange={(e) => patch("webhook_url", e.target.value)}
            />
          </label>
          <label className="block">
            <span className="caption mb-1.5 block text-muted">
              {t("embedPublish.webhookSecret")}
              {channel?.has_webhook_secret && (
                <span className="ml-2 text-muted-soft">{t("embedPublish.webhookSecretSet")}</span>
              )}
            </span>
            <input
              className="input"
              type="password"
              value={draft.webhook_secret}
              onChange={(e) => patch("webhook_secret", e.target.value)}
            />
          </label>
        </div>
      </div>

      <div className="mt-5 flex items-center justify-end gap-2 border-t border-hairline pt-4">
        <button className="btn btn-outline btn-sm" onClick={onClose} disabled={saving}>
          {t("common.cancel")}
        </button>
        <button className="btn btn-primary btn-sm" disabled={saving} onClick={() => void submit()}>
          {saving ? "…" : t("common.save")}
        </button>
      </div>
    </Modal>
  );
}

function ToggleRow({ label, checked, onChange }: {
  label: string;
  checked: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <div className="flex items-center gap-3">
      <button
        role="switch"
        aria-checked={checked}
        onClick={() => onChange(!checked)}
        className={`relative h-6 w-11 rounded-full transition-colors ${checked ? "bg-primary" : "bg-hairline-strong"}`}
      >
        <span className={`absolute top-0.5 h-5 w-5 rounded-full bg-surface-card transition-transform ${
          checked ? "translate-x-[22px]" : "translate-x-0.5"
        }`} />
      </button>
      <span className="title-sm">{label}</span>
    </div>
  );
}
