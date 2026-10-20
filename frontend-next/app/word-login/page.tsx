"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useEffect, useState } from "react";
import { AuthShell } from "@/components/auth/auth-shell";
import { AuthCard } from "@/components/auth/auth-card";
import { AuthProvider, useAuth } from "@/lib/auth";
import { useT } from "@/lib/i18n";
import { ApiError, apiGet, apiPost } from "@/lib/api-client";

/* Approval page of the Word add-in's browser sign-in. The add-in opens
 * /word-login?code=XXXX-XXXX in the user's browser; the signed-in user
 * confirms here and the add-in, polling with its secret device code, gets
 * its own token pair. The password never reaches the add-in.
 *
 * The screen shares the auth shell with the sign-in and sign-up pages —
 * same frame, same inputs, same palette. */

type Lookup = { status: string; user_code?: string; client?: string };
type Phase = "loading" | "enter" | "ask" | "approved" | "denied" | "expired";

function signInHref(code: string): string {
  const back = code ? `/word-login?code=${encodeURIComponent(code)}` : "/word-login";
  return `/login?next=${encodeURIComponent(back)}`;
}

function WordLoginApproval() {
  const { t } = useT();
  const router = useRouter();
  const params = useSearchParams();
  const auth = useAuth();
  const [code, setCode] = useState(() => (params.get("code") ?? "").trim().toUpperCase());
  const [typed, setTyped] = useState("");
  const [phase, setPhase] = useState<Phase>("loading");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!auth.ready) return;
    if (!auth.isLoggedIn) {
      router.replace(signInHref(code));
      return;
    }
    if (!code) {
      setPhase("enter");
      return;
    }
    let cancelled = false;
    setPhase("loading");
    apiGet<Lookup>(`/api/v1/auth/device/lookup?user_code=${encodeURIComponent(code)}`)
      .then((res) => {
        if (cancelled) return;
        if (res.status === "pending") {
          if (res.user_code) setCode(res.user_code);
          setPhase("ask");
        } else setPhase("expired");
      })
      .catch(() => !cancelled && setPhase("expired"));
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [auth.ready, auth.isLoggedIn, code]);

  const answer = async (approve: boolean) => {
    if (busy) return;
    setBusy(true);
    setError(null);
    try {
      await apiPost("/api/v1/auth/device/approve", { user_code: code, approve });
      setPhase(approve ? "approved" : "denied");
    } catch (e) {
      if (e instanceof ApiError && (e.status === 404 || e.status === 409)) setPhase("expired");
      else setError(t("wordLogin.failed", { error: e instanceof Error ? e.message : String(e) }));
    } finally {
      setBusy(false);
    }
  };

  const switchAccount = async () => {
    await auth.logout();
    router.replace(signInHref(code));
  };

  const name = auth.user?.username || auth.user?.email || "";

  return (
    <AuthShell mode="word" showcase={false}>
      <AuthCard kicker={t("wordLogin.eyebrow")} title={t("wordLogin.title")}>
        {phase === "loading" && (
          <p className="body-sm animate-pulse text-muted">{t("workspace.loading")}</p>
        )}

        {phase === "enter" && (
          <form
            onSubmit={(e) => {
              e.preventDefault();
              setCode(typed.trim().toUpperCase());
            }}
            className="flex flex-col gap-3"
          >
            <label className="caption text-muted" htmlFor="word-login-code">
              {t("wordLogin.enterCode")}
            </label>
            <input
              id="word-login-code"
              className="input text-center font-mono text-lg tracking-[0.3em]"
              value={typed}
              onChange={(e) => setTyped(e.target.value.toUpperCase().slice(0, 9))}
              placeholder="XXXX-XXXX"
              autoFocus
            />
            <button
              className="btn btn-primary h-11 w-full"
              type="submit"
              disabled={typed.replace(/[^A-Z]/g, "").length !== 8}
            >
              {t("wordLogin.continue")}
            </button>
          </form>
        )}

        {phase === "ask" && (
          <div className="flex flex-col gap-4">
            <p className="body-sm">{t("wordLogin.prompt", { name })}</p>
            <div className="rounded-lg border border-hairline bg-surface-strong py-3 text-center font-mono text-2xl tracking-[0.3em]">
              {code}
            </div>
            <p className="caption text-muted">{t("wordLogin.codeHint")}</p>
            {error && <p className="caption text-error">{error}</p>}
            <div className="flex gap-2">
              <button
                className="btn btn-primary flex-1"
                disabled={busy}
                onClick={() => void answer(true)}
              >
                {t("wordLogin.approve")}
              </button>
              <button
                className="btn btn-outline"
                disabled={busy}
                onClick={() => void answer(false)}
              >
                {t("wordLogin.deny")}
              </button>
            </div>
            <button
              className="caption self-center text-muted underline"
              onClick={() => void switchAccount()}
            >
              {t("wordLogin.switchAccount")}
            </button>
          </div>
        )}

        {phase === "approved" && <p className="body-sm text-success">{t("wordLogin.approved")}</p>}
        {phase === "denied" && <p className="body-sm">{t("wordLogin.denied")}</p>}
        {phase === "expired" && (
          <div className="flex flex-col gap-3">
            <p className="body-sm text-error">{t("wordLogin.expired")}</p>
            <button
              className="btn btn-outline self-center"
              onClick={() => {
                setTyped("");
                setCode("");
              }}
            >
              {t("wordLogin.enterCode")}
            </button>
          </div>
        )}
      </AuthCard>
    </AuthShell>
  );
}

export default function WordLoginPage() {
  return (
    <AuthProvider>
      <Suspense>
        <WordLoginApproval />
      </Suspense>
    </AuthProvider>
  );
}
