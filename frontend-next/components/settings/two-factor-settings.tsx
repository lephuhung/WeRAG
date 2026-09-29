/* Two-factor authentication (TOTP authenticator, 6-digit codes) inside the
 * Security settings section. The backend endpoints are planned — until they
 * ship, every call resolves through an error envelope and the section shows
 * itself as unavailable instead of crashing. Enable flow: setup (secret +
 * QR) → verify a 6-digit code → one-time recovery codes. */
"use client";

import { useEffect, useState } from "react";
import { QRCodeSVG } from "qrcode.react";
import {
  disableTwoFactor,
  enableTwoFactor,
  getTwoFactorStatus,
  setupTwoFactor,
  type TwoFactorSetupResponse,
} from "@/lib/api/auth";
import { useT } from "@/lib/i18n";

type Step = "idle" | "setup" | "recovery" | "disabling";

const CODE_RE = /^\d{6}$/;

function CodeInput({
  value,
  onChange,
  placeholder,
}: {
  value: string;
  onChange: (v: string) => void;
  placeholder: string;
}) {
  return (
    <input
      className="input w-44 tracking-[0.5em]"
      inputMode="numeric"
      autoComplete="one-time-code"
      maxLength={6}
      placeholder={placeholder}
      value={value}
      onChange={(e) => onChange(e.target.value.replace(/\D/g, "").slice(0, 6))}
    />
  );
}

export function TwoFactorSettings() {
  const { t } = useT();

  const [enabled, setEnabled] = useState<boolean | null>(null);
  const [unavailable, setUnavailable] = useState(false);
  const [step, setStep] = useState<Step>("idle");
  const [setup, setSetup] = useState<TwoFactorSetupResponse | null>(null);
  const [code, setCode] = useState("");
  const [recoveryCodes, setRecoveryCodes] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    let live = true;
    void getTwoFactorStatus().then((res) => {
      if (!live) return;
      if (!res.success && res.enabled === undefined) {
        // Backend not deployed (or endpoint missing) — keep the section but
        // mark it unavailable so the user isn't left guessing.
        setUnavailable(true);
        setEnabled(false);
        return;
      }
      setEnabled(res.enabled === true);
    });
    return () => {
      live = false;
    };
  }, []);

  const beginSetup = async () => {
    setBusy(true);
    setError(null);
    const res = await setupTwoFactor();
    setBusy(false);
    if (!res.success || !res.otpauth_url) {
      setError(res.message || t("twofa.setupFailed"));
      return;
    }
    setSetup(res);
    setCode("");
    setStep("setup");
  };

  const verifyCode = async () => {
    if (!CODE_RE.test(code)) return;
    setBusy(true);
    setError(null);
    const res = await enableTwoFactor(code);
    setBusy(false);
    if (!res.success) {
      setError(res.message || t("twofa.invalidCode"));
      return;
    }
    setEnabled(true);
    setRecoveryCodes(res.recovery_codes ?? []);
    setStep("recovery");
  };

  const confirmDisable = async () => {
    if (!CODE_RE.test(code)) return;
    setBusy(true);
    setError(null);
    const res = await disableTwoFactor(code);
    setBusy(false);
    if (!res.success) {
      setError(res.message || t("twofa.invalidCode"));
      return;
    }
    setEnabled(false);
    setSetup(null);
    setCode("");
    setStep("idle");
  };

  const copySecret = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      /* clipboard unavailable — the secret stays visible for manual entry */
    }
  };

  const badge = enabled ? (
    <span className="badge-pill bg-emerald-500/10 text-emerald-600">{t("userSettings.2faEnabled")}</span>
  ) : (
    <span className="badge-pill bg-surface-strong text-muted">{t("userSettings.2faDisabled")}</span>
  );

  return (
    <div>
      <h3 className="title-sm mb-3">{t("userSettings.2fa")}</h3>
      <p className="caption mb-3 text-muted">{t("userSettings.2faDesc")}</p>

      <div className="flex flex-wrap items-center gap-3">
        {badge}
        {unavailable && <span className="caption text-muted">{t("twofa.unavailable")}</span>}
        {!unavailable && enabled === false && step === "idle" && (
          <button className="btn btn-primary btn-sm" disabled={busy} onClick={() => void beginSetup()}>
            {t("userSettings.enable2fa")}
          </button>
        )}
        {!unavailable && enabled === true && step === "idle" && (
          <button
            className="btn btn-sm"
            onClick={() => {
              setCode("");
              setError(null);
              setStep("disabling");
            }}
          >
            {t("userSettings.disable")}
          </button>
        )}
      </div>

      {step === "setup" && setup && (
        <div className="mt-5 flex flex-col gap-4 rounded-xl border border-hairline p-4">
          <p className="caption text-muted">{t("twofa.scanHint")}</p>
          <div className="flex flex-wrap items-start gap-5">
            <div className="rounded-lg bg-white p-2">
              <QRCodeSVG value={setup.otpauth_url || ""} size={148} marginSize={0} />
            </div>
            <div className="flex min-w-0 flex-col gap-2">
              <span className="caption-uppercase text-muted-soft">{t("twofa.secretLabel")}</span>
              <code className="max-w-full break-all rounded bg-surface-strong px-2 py-1 text-[13px]">
                {setup.secret}
              </code>
              <button className="btn btn-sm self-start" onClick={() => void copySecret(setup.secret || "")}>
                {copied ? t("common.saved") : t("common.copy")}
              </button>
            </div>
          </div>
          <div className="flex flex-wrap items-center gap-3">
            <CodeInput value={code} onChange={setCode} placeholder="••••••" />
            <button
              className="btn btn-primary btn-sm"
              disabled={!CODE_RE.test(code) || busy}
              onClick={() => void verifyCode()}
            >
              {t("userSettings.verify")}
            </button>
          </div>
          {error && <span className="caption text-error">{error}</span>}
        </div>
      )}

      {step === "recovery" && (
        <div className="mt-5 flex flex-col gap-3 rounded-xl border border-hairline p-4">
          <span className="caption text-success">{t("userSettings.2faLinked")}</span>
          <p className="caption text-muted">{t("twofa.recoveryHint")}</p>
          {recoveryCodes.length > 0 ? (
            <>
              <div className="grid max-w-xs grid-cols-2 gap-1">
                {recoveryCodes.map((c) => (
                  <code key={c} className="rounded bg-surface-strong px-2 py-1 text-center text-[13px]">
                    {c}
                  </code>
                ))}
              </div>
              <button
                className="btn btn-sm self-start"
                onClick={() => void copySecret(recoveryCodes.join("\n"))}
              >
                {copied ? t("common.saved") : t("common.copy")}
              </button>
            </>
          ) : (
            <span className="caption text-muted">{t("twofa.recoverySkipped")}</span>
          )}
          <button className="btn btn-primary btn-sm self-start" onClick={() => setStep("idle")}>
            {t("common.close")}
          </button>
        </div>
      )}

      {step === "disabling" && (
        <div className="mt-5 flex flex-col gap-3 rounded-xl border border-hairline p-4">
          <p className="caption text-muted">{t("twofa.disableHint")}</p>
          <div className="flex flex-wrap items-center gap-3">
            <CodeInput value={code} onChange={setCode} placeholder="••••••" />
            <button
              className="btn btn-primary btn-sm"
              disabled={!CODE_RE.test(code) || busy}
              onClick={() => void confirmDisable()}
            >
              {t("userSettings.disable")}
            </button>
            <button
              className="btn btn-sm"
              onClick={() => {
                setStep("idle");
                setError(null);
              }}
            >
              {t("common.cancel")}
            </button>
          </div>
          {error && <span className="caption text-error">{error}</span>}
        </div>
      )}

      {step === "idle" && error && <span className="caption mt-3 block text-error">{error}</span>}
    </div>
  );
}
