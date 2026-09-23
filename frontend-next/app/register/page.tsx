"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useEffect, useState } from "react";
import { apiGet, apiPost, getTokens } from "@/lib/api-client";
import { getAuthConfig, getInvitationByToken } from "@/lib/api/auth";
import { acceptInvitationByToken } from "@/lib/api/tenants";
import { Orb } from "@/components/orb";

function RegisterForm() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const token = searchParams.get("token") ?? "";
  const [username, setUsername] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [tenantName, setTenantName] = useState<string | null>(null);
  const [inviteOnly, setInviteOnly] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  /* Signed-in visitors landing on an invite link can join with their
   * current account (auth/me/invitations/accept-by-token) instead of
   * registering a second one — they can still opt into the form below. */
  const [hasSession] = useState(() => !!getTokens().token);
  const [useNewAccount, setUseNewAccount] = useState(false);
  const showJoinCard = !!token && hasSession && !useNewAccount;

  useEffect(() => {
    let alive = true;
    getAuthConfig()
      .then((c) => {
        if (alive) setInviteOnly(c.registration_mode === "invite_only");
      })
      .catch(() => {});
    if (token) {
      getInvitationByToken(token)
        .then((r) => {
          if (alive && r.data?.tenant_name) setTenantName(r.data.tenant_name);
        })
        .catch(() => {});
    }
    return () => {
      alive = false;
    };
  }, [token]);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const path = token ? "/api/v1/auth/register-by-invite" : "/api/v1/auth/register";
      const res = await apiPost<{
        success: boolean;
        message?: string;
        token?: string;
        refresh_token?: string;
      }>(path, token ? { token, username, email, password } : { username, email, password });
      if (!res.success || !res.token) throw new Error(res.message ?? "Register failed");
      localStorage.setItem("weknora_token", res.token);
      if (res.refresh_token) localStorage.setItem("weknora_refresh_token", res.refresh_token);
      // Mirror persistLoginResponse: probe /auth/me then enter or onboard.
      const me = await apiGet<{ success: boolean; data?: { tenant?: unknown } }>(`/api/v1/auth/me`).catch(
        () => null,
      );
      router.push(me?.data?.tenant ? "/platform/knowledge-bases" : "/onboarding/workspace");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Register failed");
    } finally {
      setBusy(false);
    }
  };

  const joinWithSession = async () => {
    setBusy(true);
    setError(null);
    try {
      const res = await acceptInvitationByToken(token);
      if (!res.success) throw new Error(res.message ?? "Join failed");
      router.push("/platform/knowledge-bases");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Join failed");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="relative flex min-h-screen items-center justify-center overflow-hidden bg-canvas px-6">
      <Orb color="peach" size={520} className="-top-40 right-[-100px]" />
      <div className="relative w-full max-w-[400px]">
        <div className="display-lg mb-2 text-center">
          {token ? `Join ${tenantName ?? "workspace"}` : "Create account"}
        </div>
        <p className="body-sm mb-10 text-center text-muted">
          {token
            ? showJoinCard
              ? "You were invited — join with your current account or create a new one."
              : "You were invited — pick a username to join."
            : "Self-serve registration."}
          {inviteOnly && !token ? " This deployment is invite-only." : ""}
        </p>
        {showJoinCard ? (
          <div className="card p-6 sm:p-8">
            {error && <p className="body-sm mb-4 text-error">{error}</p>}
            <button
              type="button"
              className="btn btn-primary w-full"
              disabled={busy}
              onClick={() => void joinWithSession()}
            >
              {busy ? "Joining…" : "Join workspace"}
            </button>
            <button
              type="button"
              className="btn btn-outline mt-3 w-full"
              onClick={() => setUseNewAccount(true)}
            >
              Create a new account instead
            </button>
          </div>
        ) : (
        <form className="card p-6 sm:p-8" onSubmit={submit}>
          <label className="mb-4 block">
            <span className="caption mb-1.5 block text-muted">Username</span>
            <input className="input" required value={username} onChange={(e) => setUsername(e.target.value)} />
          </label>
          <label className="mb-4 block">
            <span className="caption mb-1.5 block text-muted">Email</span>
            <input
              className="input"
              type="email"
              required
              value={email}
              onChange={(e) => setEmail(e.target.value)}
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
            />
          </label>
          {error && <p className="body-sm mb-4 text-error">{error}</p>}
          <button type="submit" className="btn btn-primary w-full" disabled={busy}>
            {busy ? "Creating…" : token ? "Join workspace" : "Register"}
          </button>
        </form>
        )}
      </div>
    </div>
  );
}

export default function Register() {
  return (
    <Suspense>
      <RegisterForm />
    </Suspense>
  );
}
