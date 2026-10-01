"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useCallback, useEffect, useRef, useState } from "react";
import { Orb } from "@/components/orb";
import { BrandLogo } from "@/components/brand-logo";
import { resolveSafeNextPath } from "@/lib/safe-next";

function LoginForm() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [totpRequired, setTotpRequired] = useState(false);
  const [totpDigits, setTotpDigits] = useState<string[]>(Array(6).fill(""));
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
      const res = await fetch("/api/v1/auth/login", {
        method: "POST",
        headers: { "Content-Type": "application/json", "Accept-Language": "vi-VN" },
        body: JSON.stringify({ email, password, two_factor_code: totpCode }),
      });
      const data = await res.json().catch(() => null);
      const token =
        data && typeof data === "object" && "token" in data
          ? (data as { token?: string }).token
          : undefined;
      const refreshToken =
        data && typeof data === "object" && "refresh_token" in data
          ? (data as { refresh_token?: string }).refresh_token
          : undefined;
      if (
        data &&
        typeof data === "object" &&
        (data as { two_factor_required?: boolean }).two_factor_required
      ) {
        // Correct password but a 6-digit authenticator code is required.
        setTotpRequired(true);
        setError("Enter the 6-digit code from your authenticator app");
        return;
      }
      if (!res.ok || !token) {
        const msg =
          data && typeof data === "object" && "message" in data
            ? String((data as { message?: unknown }).message ?? "Sign in failed")
            : "Sign in failed";
        setError(msg);
        return;
      }
      localStorage.setItem("weknora_token", token);
      if (refreshToken) localStorage.setItem("weknora_refresh_token", refreshToken);
      const rawPayload = data as {
        active_tenant?: { id?: number | string };
        tenant?: { id?: number | string };
        user?: { tenant_id?: number | string };
      };
      const tenantId =
        rawPayload?.active_tenant?.id ??
        rawPayload?.tenant?.id ??
        rawPayload?.user?.tenant_id;
      if (tenantId && Number(tenantId) > 0) {
        localStorage.setItem("weknora_selected_tenant_id", String(tenantId));
      } else {
        localStorage.removeItem("weknora_selected_tenant_id");
      }
      router.push(resolveSafeNextPath(searchParams.get("next")));
    } catch {
      setError("Network error — is the backend reachable?");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="relative flex min-h-screen items-center justify-center overflow-hidden bg-canvas px-6">
      <Orb color="lavender" size={560} className="-left-40 top-[-180px]" />
      <Orb color="mint" size={480} className="bottom-[-160px] right-[-120px]" />
      <Orb color="peach" size={320} className="right-[20%] top-[-120px]" />

      <div className="relative w-full max-w-[400px]">
        <div className="mb-6 flex justify-center">
          <BrandLogo size={68} priority />
        </div>
        <p className="body-sm mb-8 text-center text-muted">Sign in to your knowledge workspace</p>

        <form className="card p-6 sm:p-8" onSubmit={submit}>
          <label className="mb-4 block">
            <span className="caption mb-1.5 block text-muted">Email</span>
            <input
              className="input"
              type="email"
              required
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="you@company.com"
            />
          </label>
          <label className="mb-6 block">
            <span className="caption mb-1.5 block text-muted">Password</span>
            <input
              className="input"
              type="password"
              required
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="••••••••"
            />
          </label>
          {totpRequired && (
            <div className="mb-6">
              <span className="caption mb-1.5 block text-muted">Authenticator code</span>
              <div className="flex justify-between gap-2" onPaste={onTotpPaste}>
                {totpDigits.map((digit, i) => (
                  <input
                    key={i}
                    ref={(el) => {
                      totpRefs.current[i] = el;
                    }}
                    className="h-12 w-full max-w-[48px] rounded-[10px] border border-hairline bg-surface-card text-center text-[20px] font-semibold tracking-normal text-ink outline-none transition-colors focus:border-ink"
                    type="text"
                    inputMode="numeric"
                    autoComplete={i === 0 ? "one-time-code" : "off"}
                    maxLength={1}
                    autoFocus={i === 0}
                    required
                    aria-label={`Digit ${i + 1}`}
                    value={digit}
                    onChange={(e) => setDigit(i, e.target.value)}
                    onKeyDown={(e) => onTotpKeyDown(i, e)}
                  />
                ))}
              </div>
            </div>
          )}
          {error && <p className="body-sm mb-4 text-error">{error}</p>}
          <button type="submit" className="btn btn-primary w-full" disabled={busy}>
            {busy ? "Signing in…" : "Sign in"}
          </button>
          <div className="caption mt-5 flex items-center justify-between text-muted">
            <Link href="/register" className="hover:text-ink">
              Register by invite
            </Link>
            <Link href="/onboarding/workspace" className="hover:text-ink">
              SSO / OIDC
            </Link>
          </div>
        </form>

        <p className="caption mt-8 text-center text-muted-soft">
          Tra cứu tài liệu — retrieval-augmented knowledge platform
        </p>
      </div>
    </div>
  );
}

export default function Login() {
  return (
    <Suspense>
      <LoginForm />
    </Suspense>
  );
}
