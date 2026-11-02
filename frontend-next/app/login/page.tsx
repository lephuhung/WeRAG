"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useCallback, useEffect, useRef, useState } from "react";
import { AuthShell, authStyles as styles } from "@/components/auth/auth-shell";
import { AuthCard } from "@/components/auth/auth-card";
import { AuthProvider, useAuth } from "@/lib/auth";
import { useT } from "@/lib/i18n";
import { resolveSafeNextPath } from "@/lib/safe-next";
import { IconEye } from "@/components/icons";

function LoginForm() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const { t } = useT();
  /* The sign-in API lives in lib/auth.tsx — the page must not re-implement
   * the token handling (it used to store the tokens itself). */
  const { login } = useAuth();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [totpRequired, setTotpRequired] = useState(false);
  const [totpDigits, setTotpDigits] = useState<string[]>(Array(6).fill(""));
  const [resetHelp, setResetHelp] = useState(false);
  const totpRefs = useRef<(HTMLInputElement | null)[]>([]);
  const totpCode = totpDigits.join("");

  const setDigit = useCallback((index: number, value: string) => {
    const digit = value.replace(/\D/g, "").slice(-1);
    setTotpDigits((prev) => {
      const next = [...prev];
      next[index] = digit;
      return next;
    });
    if (digit && index < 5) totpRefs.current[index + 1]?.focus();
  }, []);

  const onTotpKeyDown = (index: number, e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Backspace" && !totpDigits[index] && index > 0) {
      totpRefs.current[index - 1]?.focus();
    } else if (e.key === "ArrowLeft" && index > 0) {
      totpRefs.current[index - 1]?.focus();
    } else if (e.key === "ArrowRight" && index < 5) {
      totpRefs.current[index + 1]?.focus();
    }
  };

  const onTotpPaste = (e: React.ClipboardEvent<HTMLInputElement>) => {
    const pasted = e.clipboardData.getData("text").replace(/\D/g, "").slice(0, 6);
    if (!pasted) return;
    e.preventDefault();
    setTotpDigits((prev) => {
      const next = [...prev];
      pasted.split("").forEach((d, i) => (next[i] = d));
      return next;
    });
    totpRefs.current[Math.min(pasted.length, 5)]?.focus();
  };

  // When the full code is present after the 2FA prompt, submit on its own —
  // authenticator users expect paste-and-go.
  useEffect(() => {
    if (totpRequired && totpCode.length === 6 && !totpDigits.includes("")) {
      void submit(new Event("submit") as unknown as React.FormEvent);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [totpCode, totpRequired]);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const result = await login(email, password, totpCode);
      if (!result) {
        router.push(resolveSafeNextPath(searchParams.get("next")));
        return;
      }
      if (result.twoFactorRequired) {
        // Correct password but a 6-digit authenticator code is required.
        setTotpRequired(true);
        setError(t("auth.totpRequired"));
        return;
      }
      setError(
        result.error === "Invalid email or password"
          ? t("auth.loginInvalid")
          : result.error,
      );
    } catch {
      setError(t("auth.loginNetwork"));
    } finally {
      setBusy(false);
    }
  };

  return (
    <AuthShell mode="login">
      <AuthCard
        kicker={t("auth.loginKicker")}
        title={t("auth.loginTitle")}
        subtitle={t("auth.loginSubtitle")}
        note={t("auth.loginNote")}
      >
        <form onSubmit={submit}>
          <label className="mb-4 block">
            <span className="caption mb-1.5 block text-muted">{t("auth.email")}</span>
            <input
              className="input"
              type="email"
              required
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder={t("auth.emailPlaceholder")}
              autoComplete="email"
            />
          </label>
          <div className="mb-2">
            <label htmlFor="login-password" className="block">
              <span className="caption mb-1.5 block text-muted">{t("auth.password")}</span>
            </label>
            <div className="relative">
              <input
                id="login-password"
                className="input pr-12"
                type={showPassword ? "text" : "password"}
                required
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder={t("auth.passwordPlaceholder")}
                autoComplete="current-password"
              />
              <button
                type="button"
                className="absolute inset-y-0 right-1 flex w-10 items-center justify-center rounded text-muted transition-colors hover:text-ink"
                aria-label={showPassword ? t("auth.hidePassword") : t("auth.showPassword")}
                aria-pressed={showPassword}
                aria-controls="login-password"
                onClick={() => setShowPassword((visible) => !visible)}
              >
                <span className="relative" aria-hidden="true">
                  <IconEye className="h-5 w-5" />
                  {showPassword && (
                    <span className="absolute left-0 top-1/2 h-px w-5 -rotate-45 bg-current" />
                  )}
                </span>
              </button>
            </div>
          </div>
          <div className="mb-6 text-right">
            <button
              type="button"
              className="caption text-muted underline underline-offset-2 transition-colors hover:text-ink"
              aria-expanded={resetHelp}
              onClick={() => setResetHelp((shown) => !shown)}
            >
              {t("auth.forgotPassword")}
            </button>
            {resetHelp && (
              <p role="status" className="caption mt-2 text-muted">
                {t("auth.forgotPasswordSoon")}
              </p>
            )}
          </div>
          {totpRequired && (
            <div className={styles.totp}>
              <span className="caption mb-1.5 block text-muted">{t("auth.totpLabel")}</span>
              <div className={styles.totpRow} onPaste={onTotpPaste}>
                {totpDigits.map((digit, i) => (
                  <input
                    key={i}
                    ref={(el) => {
                      totpRefs.current[i] = el;
                    }}
                    className="input h-12 max-w-[48px] px-0 text-center text-[20px] font-semibold"
                    type="text"
                    inputMode="numeric"
                    autoComplete={i === 0 ? "one-time-code" : "off"}
                    maxLength={1}
                    autoFocus={i === 0}
                    required
                    aria-label={t("auth.totpDigit", { n: i + 1 })}
                    value={digit}
                    onChange={(e) => setDigit(i, e.target.value)}
                    onKeyDown={(e) => onTotpKeyDown(i, e)}
                  />
                ))}
              </div>
            </div>
          )}
          {error && (
            <p role="alert" className={styles.error}>
              {error}
            </p>
          )}
          <button type="submit" className="btn btn-primary h-11 w-full" disabled={busy}>
            {busy ? t("auth.loginBusy") : t("auth.loginSubmit")}
          </button>
        </form>
        <div className={styles.divider}>{t("auth.or")}</div>
        <Link href="/onboarding/workspace" className="btn btn-outline h-11 w-full">
          {t("auth.loginViaOrg")}
        </Link>
      </AuthCard>
    </AuthShell>
  );
}

export default function Login() {
  return (
    <AuthProvider>
      <Suspense>
        <LoginForm />
      </Suspense>
    </AuthProvider>
  );
}
