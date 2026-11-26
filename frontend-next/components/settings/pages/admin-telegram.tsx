"use client";

/* SuperAdmin Telegram: the system alert bot. One bot for the whole platform
 * (its token is a secret the server never returns), the chats it writes to
 * (private chats of SuperAdmins or a group of the people who handle the
 * alerts, linked with a one-time code) and the alert kinds it sends. The
 * switch, token and muted kinds are the telegram_alerts.* system settings. */

import { useCallback, useEffect, useRef, useState } from "react";
import { SystemSection } from "@/components/settings/system-section";
import { Notice } from "@/components/settings/notice";
import { FieldLabel } from "@/components/settings/field";
import { Toggle } from "@/components/settings/toggle";
import { useToast } from "@/components/toast";
import { useConfirm } from "@/components/confirm-dialog";
import { useT, type LocaleKey } from "@/lib/i18n";
import { getCurrentUser } from "@/lib/api/auth";
import { formatDateTime } from "@/lib/datetime";
import {
  TELEGRAM_SETTING_KEYS as K,
  createTelegramLinkCode,
  deleteTelegramAlertChat,
  getSystemSetting,
  getTelegramAlertStatus,
  listTelegramAlertChats,
  sendTelegramTest,
  updateSystemSetting,
  type TelegramAlertChat,
  type TelegramAlertGroup,
  type TelegramAlertStatus,
  type TelegramLinkCode,
} from "@/lib/api/system";
import { technicalErrorDetail, userErrorMessage } from "@/lib/user-error";
import { TechnicalDetails } from "@/components/technical-details";

type Form = { enabled: boolean; muted: string[] };
const emptyForm: Form = { enabled: false, muted: [] };
const GROUPS: TelegramAlertGroup[] = ["billing", "review", "incident"];
const sameSet = (a: string[], b: string[]) => a.length === b.length && a.every((x) => b.includes(x));

export default function AdminTelegram() {
  const { t, locale } = useT();
  const toast = useToast();
  const confirm = useConfirm();
  const [allowed, setAllowed] = useState<boolean | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  /* loadError offers "retry" (reload); actionError does not (a reload
   * would drop the edits) */
  const [loadError, setLoadError] = useState("");
  const [actionError, setActionError] = useState("");
  const [form, setForm] = useState<Form>(emptyForm);
  const [base, setBase] = useState<Form>(emptyForm);
  const [tokenMasked, setTokenMasked] = useState("");
  const [token, setToken] = useState("");
  const [status, setStatus] = useState<TelegramAlertStatus | null>(null);
  const [chats, setChats] = useState<TelegramAlertChat[]>([]);
  const [code, setCode] = useState<TelegramLinkCode | null>(null);
  /* the active chats when the code was made: one more = it was used */
  const activeAtCode = useRef(0);
  /* t changes with the locale; a ref keeps load (and the form) stable */
  const tRef = useRef(t);
  tRef.current = t;
  const [result, setResult] = useState<{ ok: boolean; text: string; detail?: string } | null>(null);

  const refresh = useCallback(async () => {
    const [st, list] = await Promise.all([getTelegramAlertStatus(), listTelegramAlertChats()]);
    setStatus(st);
    setChats(list);
    return st;
  }, []);

  const load = useCallback(async () => {
    try {
      const [tok, st] = await Promise.all([getSystemSetting(K.token), refresh()]);
      const f: Form = { enabled: st.enabled, muted: [...(st.muted_kinds ?? [])].sort() };
      setForm(f);
      setBase(f);
      setTokenMasked(String(tok.value ?? ""));
      setToken("");
      setLoadError("");
      setActionError("");
    } catch (e) {
      setLoadError(userErrorMessage(e, tRef.current, "tg.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [refresh]);

  useEffect(() => {
    let alive = true;
    getCurrentUser()
      .then((me) => {
        if (!alive) return;
        const ok = me.data?.user?.is_system_admin === true;
        setAllowed(ok);
        if (ok) void load();
      })
      .catch(() => alive && setAllowed(false));
    return () => {
      alive = false;
    };
  }, [load]);

  /* while a code is shown, watch for the chat it links */
  useEffect(() => {
    if (!code) return;
    const timer = window.setInterval(() => {
      if (Date.now() > new Date(code.expires_at).getTime()) {
        setCode(null);
        return;
      }
      void listTelegramAlertChats()
        .then((list) => {
          setChats(list);
          // linked: the one-time code is spent
          if (list.filter((c) => c.active).length > activeAtCode.current) setCode(null);
        })
        .catch(() => undefined);
    }, 4000);
    return () => window.clearInterval(timer);
  }, [code]);

  const dirty = form.enabled !== base.enabled || !sameSet(form.muted, base.muted) || token !== "";

  /* the state after a change; a failure here is not a failed save */
  const refreshQuietly = () => refresh().catch(() => undefined);

  const save = async () => {
    setBusy(true);
    setActionError("");
    try {
      if (token !== "") {
        const row = await updateSystemSetting(K.token, token.trim());
        setTokenMasked(String(row.value ?? ""));
        setToken("");
      }
      if (!sameSet(form.muted, base.muted)) await updateSystemSetting(K.muted, form.muted.join(","));
      if (form.enabled !== base.enabled) await updateSystemSetting(K.enabled, form.enabled);
      setBase(form);
      toast.success(t("common.saved"));
    } catch (e) {
      setActionError(userErrorMessage(e, t, "tg.saveFailed"));
    } finally {
      setBusy(false);
    }
    await refreshQuietly();
  };

  const clearToken = async () => {
    const ok = await confirm({ title: t("tg.tokenClearTitle"), message: t("tg.tokenClearBody"), confirmLabel: t("tg.tokenClear"), danger: true });
    if (!ok) return;
    setBusy(true);
    setActionError("");
    setResult(null);
    try {
      await updateSystemSetting(K.token, "");
      setTokenMasked("");
      setToken("");
      setCode(null);
      toast.success(t("common.saved"));
    } catch (e) {
      setActionError(userErrorMessage(e, t, "tg.saveFailed"));
    } finally {
      setBusy(false);
    }
    await refreshQuietly();
  };

  const makeCode = async () => {
    setBusy(true);
    setResult(null);
    try {
      const c = await createTelegramLinkCode();
      activeAtCode.current = chats.filter((x) => x.active).length;
      setCode(c);
    } catch (e) {
      setResult({ ok: false, text: userErrorMessage(e, t, "tg.codeFailed", { prefix: true }), detail: technicalErrorDetail(e) });
    } finally {
      setBusy(false);
    }
  };

  const sendTest = async () => {
    setBusy(true);
    setResult(null);
    try {
      const n = await sendTelegramTest();
      setResult({ ok: true, text: t("tg.testQueued", { n }) });
      window.setTimeout(() => void refreshQuietly(), 4000);
    } catch (e) {
      setResult({ ok: false, text: userErrorMessage(e, t, "tg.testFailed", { prefix: true }), detail: technicalErrorDetail(e) });
    } finally {
      setBusy(false);
    }
  };

  const removeChat = async (c: TelegramAlertChat) => {
    const ok = await confirm({
      title: t("tg.removeTitle"),
      message: t("tg.removeBody", { name: c.title || t("tg.untitled") }),
      confirmLabel: t("tg.remove"),
      danger: true,
    });
    if (!ok) return;
    try {
      await deleteTelegramAlertChat(c.id);
      setChats((list) => list.filter((x) => x.id !== c.id));
    } catch (e) {
      setActionError(userErrorMessage(e, t, "tg.saveFailed"));
    }
  };

  const toggleKind = (kind: string, on: boolean) => {
    const muted = on ? form.muted.filter((k) => k !== kind) : [...form.muted, kind].sort();
    setForm({ ...form, muted });
  };

  /* a kind the dictionary does not know yet shows the server's label */
  const kindLabel = (kind: string, label: string) => {
    const key = `tg.kind.${kind}` as LocaleKey;
    const text = t(key);
    return text === key ? label : text;
  };

  const botLive = !!status?.has_token && !!status?.enabled;
  const chatState = (c: TelegramAlertChat) => {
    if (c.active && !botLive) return { tone: "text-muted", text: t("tg.chatBotOff") };
    if (c.active) return { tone: "text-success", text: t("tg.chatActive") };
    if (c.disabled_at) return { tone: "text-muted", text: c.last_error || t("tg.chatDisabled") };
    return { tone: "text-warning", text: t("tg.chatAdminGone") };
  };

  return (
    <SystemSection allowed={allowed} title={t("systemNav.telegram")} deniedMessage={t("tg.denied")} loading={loading} loadingLabel={t("common.loading")}>
      <div className="space-y-5 py-6">
        <p className="caption max-w-[860px] text-muted">{t("tg.desc")}</p>
        {loadError && (
          <Notice
            tone="error"
            action={
              <button type="button" className="btn btn-outline btn-sm" onClick={() => void load()}>
                {t("common.retry")}
              </button>
            }
          >
            {loadError}
          </Notice>
        )}
        {actionError && <Notice tone="error">{actionError}</Notice>}

        <div className="grid grid-cols-1 items-start gap-6 lg:grid-cols-2">
          {/* left: the bot and what it sends */}
          <div className="min-w-0 space-y-5">
            <div className="card flex items-center justify-between gap-4 px-4 py-3">
              <div className="min-w-0">
                <div className="text-[14px] font-medium text-ink">{t("tg.enabled")}</div>
                <div className="caption text-muted">{t("tg.enabledDesc")}</div>
              </div>
              <Toggle checked={form.enabled} label={t("tg.enabled")} onChange={(v) => setForm({ ...form, enabled: v })} disabled={busy} />
            </div>

            <div className="min-w-0">
              <FieldLabel htmlFor="tg-token">{t("tg.token")}</FieldLabel>
              <input id="tg-token" className="input w-full font-mono" type="password" autoComplete="off" value={token} disabled={busy}
                placeholder={tokenMasked ? t("tg.tokenKeep") : "123456789:AAH…"}
                onChange={(e) => setToken(e.target.value)} />
              <div className="caption mt-1 text-muted">{t("tg.tokenHelp")}</div>
              {tokenMasked && (
                <div className="caption mt-1 flex items-center gap-2 text-muted">
                  <span>{t("tg.tokenSet")}</span>
                  <button type="button" className="underline hover:text-ink" disabled={busy} onClick={() => void clearToken()}>
                    {t("tg.tokenClear")}
                  </button>
                </div>
              )}
            </div>

            {status?.has_token && (
              status.bot_error ? (
                <Notice tone="error">{status.bot_error}</Notice>
              ) : status.bot_username ? (
                <Notice tone="success">
                  {t("tg.botOk", { name: status.bot_name || status.bot_username, username: status.bot_username })}
                </Notice>
              ) : null
            )}

            <div className="card px-4 py-3">
              <div className="mb-2 text-[14px] font-medium text-ink">{t("tg.kindsTitle")}</div>
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-3 lg:grid-cols-1 2xl:grid-cols-3">
                {GROUPS.map((g) => (
                  <div key={g} className="min-w-0">
                    <div className="caption mb-1 font-medium text-muted">{t(`tg.group.${g}` as LocaleKey)}</div>
                    <div className="space-y-1">
                      {(status?.kinds ?? []).filter((k) => k.group === g).map((k) => (
                        <label key={k.kind} className="flex cursor-pointer items-start gap-2 text-[13px] text-ink">
                          <input type="checkbox" className="mt-0.5" checked={!form.muted.includes(k.kind)} disabled={busy}
                            onChange={(e) => toggleKind(k.kind, e.target.checked)} />
                          <span className="min-w-0">{kindLabel(k.kind, k.label)}</span>
                        </label>
                      ))}
                    </div>
                  </div>
                ))}
              </div>
            </div>

            <div className="flex flex-wrap items-center justify-end gap-3">
              <button type="button" className="btn btn-outline" disabled={busy || dirty || !base.enabled || !status?.has_token}
                title={dirty ? t("tg.saveFirst") : undefined} onClick={() => void sendTest()}>
                {t("tg.test")}
              </button>
              <button type="button" className="btn btn-primary" disabled={busy || !dirty} onClick={() => void save()}>
                {t("common.save")}
              </button>
            </div>
            {result && (
              <Notice tone={result.ok ? "success" : "error"}>
                <span className="break-words">{result.text}</span>
                <TechnicalDetails detail={result.detail} />
              </Notice>
            )}

          </div>

          {/* right: the chats it writes to */}
          <div className="min-w-0 space-y-3">
            <div className="card space-y-3 px-4 py-3">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div className="min-w-0">
                  <div className="text-[14px] font-medium text-ink">{t("tg.chatsTitle")}</div>
                  <div className="caption text-muted">{t("tg.chatsDesc")}</div>
                </div>
                <button type="button" className="btn btn-outline btn-sm" disabled={busy || !status?.has_token || dirty || !base.enabled}
                  title={dirty ? t("tg.saveFirst") : !base.enabled ? t("tg.enableFirst") : undefined} onClick={() => void makeCode()}>
                  {t("tg.makeCode")}
                </button>
              </div>

              {code && (
                <div className="rounded-lg border border-hairline bg-canvas px-3 py-3 text-[13px] text-ink">
                  <div className="flex flex-wrap items-baseline gap-2">
                    <span>{t("tg.codeLabel")}</span>
                    <code className="rounded bg-surface px-2 py-0.5 font-mono text-[15px] tracking-wider">{code.code}</code>
                    <span className="caption text-muted">{t("tg.codeExpires", { at: formatDateTime(code.expires_at, locale) })}</span>
                  </div>
                  <ol className="caption mt-2 list-decimal space-y-1 pl-5 text-muted">
                    <li>
                      {t("tg.stepPrivate")}{" "}
                      <a className="underline hover:text-ink" href={code.private_link} target="_blank" rel="noreferrer">{t("tg.openPrivate")}</a>
                    </li>
                    <li>
                      {t("tg.stepGroup")}{" "}
                      <a className="underline hover:text-ink" href={code.group_link} target="_blank" rel="noreferrer">{t("tg.openGroup")}</a>
                    </li>
                    <li>{t("tg.stepManual", { code: code.code })}</li>
                  </ol>
                  <div className="caption mt-2 text-warning">{t("tg.groupWarning")}</div>
                </div>
              )}

              {chats.length === 0 ? (
                <div className="caption text-muted">{t("tg.noChats")}</div>
              ) : (
                <ul className="divide-y divide-hairline">
                  {chats.map((c) => {
                    const st = chatState(c);
                    return (
                      <li key={c.id} className="flex items-center justify-between gap-3 py-2">
                        <div className="min-w-0">
                          <div className="truncate text-[14px] text-ink">
                            {c.title || t("tg.untitled")}{" "}
                            <span className="caption text-muted">· {c.chat_type === "private" ? t("tg.typePrivate") : t("tg.typeGroup")}</span>
                          </div>
                          <div className="caption text-muted">
                            {t("tg.linkedBy", { name: c.linked_by_name || c.linked_by, at: formatDateTime(c.linked_at, locale) })}
                          </div>
                          <div className={`caption break-words ${st.tone}`}>{st.text}</div>
                        </div>
                        <button type="button" className="btn btn-outline btn-sm shrink-0" onClick={() => void removeChat(c)}>
                          {t("tg.remove")}
                        </button>
                      </li>
                    );
                  })}
                </ul>
              )}
            </div>

            {status && (
              <div className="caption flex flex-wrap gap-x-4 gap-y-1 text-muted">
                <span>{t("tg.pending", { n: status.pending })}</span>
                {status.failed_24h > 0 && <span className="text-warning">{t("tg.failed", { n: status.failed_24h })}</span>}
                {status.last_sent_at && <span>{t("tg.lastSent", { at: formatDateTime(status.last_sent_at, locale) })}</span>}
              </div>
            )}
          </div>
        </div>
      </div>
    </SystemSection>
  );
}
