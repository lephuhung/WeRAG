"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useState } from "react";
import { Orb } from "@/components/orb";

function LoginForm() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const res = await fetch("/api/v1/auth/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, password }),
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
      router.push(searchParams.get("next") ?? "/platform/knowledge-bases");
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
        <div className="display-lg mb-2 text-center">WeRAG</div>
        <p className="body-sm mb-10 text-center text-muted">Sign in to your knowledge workspace</p>

        <form className="card p-8" onSubmit={submit}>
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
          WeRAG — retrieval-augmented knowledge platform
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
