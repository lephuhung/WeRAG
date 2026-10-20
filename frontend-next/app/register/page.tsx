"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useEffect, useState } from "react";
import { apiGet, apiPost, getTokens } from "@/lib/api-client";
import { getAuthConfig, getInvitationByToken } from "@/lib/api/auth";
import { acceptInvitationByToken } from "@/lib/api/tenants";
import Link from "next/link";
import { AuthShell, authStyles as styles } from "@/components/auth/auth-shell";

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
      if (!res.success) throw new Error(res.message ?? "Đăng ký không thành công");
      // Self-serve registration answers with the account only (the invite
      // path also signs in): sign in with what was just typed.
      const session = res.token
        ? res
        : await apiPost<{ success: boolean; message?: string; token?: string; refresh_token?: string }>(
            "/api/v1/auth/login",
            { email, password },
          );
      if (!session.success || !session.token) {
        router.push("/login");
        return;
      }
      localStorage.setItem("weknora_token", session.token);
      if (session.refresh_token) localStorage.setItem("weknora_refresh_token", session.refresh_token);
      // Mirror persistLoginResponse: probe /auth/me, then onboard only when
      // this deployment lets users create organizations (onboarding offers
      // to skip to the chat). Otherwise a new account stays a Guest until a
      // SuperAdmin or Tenant Admin adds it to an organization, and meanwhile
      // chats with the public-lookup assistant.
      const me = await apiGet<{
        success: boolean;
        data?: { tenant?: unknown; capabilities?: { can_create_tenant?: boolean } };
      }>(`/api/v1/auth/me`).catch(() => null);
      router.push(
        me?.data?.tenant
          ? "/platform/knowledge-bases"
          : me?.data?.capabilities?.can_create_tenant
            ? "/onboarding/workspace"
            : "/platform/creatChat",
      );
    } catch (err) {
      setError(err instanceof Error ? err.message : "Đăng ký không thành công");
    } finally {
      setBusy(false);
    }
  };

  const joinWithSession = async () => {
    setBusy(true);
    setError(null);
    try {
      const res = await acceptInvitationByToken(token);
      if (!res.success) throw new Error(res.message ?? "Tham gia không thành công");
      router.push("/platform/knowledge-bases");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Tham gia không thành công");
    } finally {
      setBusy(false);
    }
  };

  return (
    <AuthShell mode="register">
        <p className={styles.kicker}>BẮT ĐẦU CÙNG WERAG</p>
        <h2 className={styles.title}>{token ? "Tham gia tổ chức" : "Tạo tài khoản"}</h2>
        <p className={styles.subtitle}>
          {token
            ? showJoinCard
              ? `Bạn được mời tham gia ${tenantName ?? "tổ chức"}. Tiếp tục với tài khoản hiện tại hoặc tạo tài khoản mới.`
              : `Tạo tài khoản để tham gia ${tenantName ?? "tổ chức"}.`
            : "Mở lối đến tri thức pháp luật. Bắt đầu không gian tra cứu và hỏi đáp của bạn."}
        </p>
        {inviteOnly && !token ? (
          <div role="status" className={styles.error}>Hệ thống chỉ cho phép đăng ký qua lời mời. Vui lòng liên hệ quản trị viên để nhận liên kết mời.
            <div className={styles.links}><Link href="/login">Quay lại đăng nhập →</Link></div>
          </div>
        ) : (
        <>
        {showJoinCard ? (
          <div className={styles.form}>
            {error && <p role="alert" className={styles.error}>{error}</p>}
            <button
              type="button"
              className={styles.primary}
              disabled={busy}
              onClick={() => void joinWithSession()}
            >
              {busy ? "Đang tham gia…" : "Tham gia tổ chức →"}
            </button>
            <button
              type="button"
              className={styles.secondary}
              onClick={() => setUseNewAccount(true)}
            >
              Tạo tài khoản mới
            </button>
          </div>
        ) : (
        <form className={styles.form} onSubmit={submit}>
          <label className="mb-4 block">
            <span className="caption mb-1.5 block text-muted">Tên người dùng</span>
            <input className="input" autoComplete="username" placeholder="Tên hiển thị của bạn" required value={username} onChange={(e) => setUsername(e.target.value)} />
          </label>
          <label className="mb-4 block">
            <span className="caption mb-1.5 block text-muted">Email</span>
            <input
              className="input"
              type="email"
              autoComplete="email"
              placeholder="ban@donvi.vn"
              required
              value={email}
              onChange={(e) => setEmail(e.target.value)}
            />
          </label>
          <label className="mb-6 block">
            <span className="caption mb-1.5 block text-muted">Mật khẩu</span>
            <input
              className="input"
              type="password"
              autoComplete="new-password"
              placeholder="Tạo mật khẩu của bạn"
              required
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </label>
          {error && <p role="alert" className={styles.error}>{error}</p>}
          <button type="submit" className={styles.primary} disabled={busy}>
            {busy ? "Đang tạo tài khoản…" : token ? "Tạo tài khoản và tham gia →" : "Tạo tài khoản →"}
          </button>
        </form>
        )}
        </>
        )}
        <p className={styles.note}>Tra cứu thông minh · Hỏi đáp tài liệu · Đối chiếu nguồn</p>
    </AuthShell>
  );
}

export default function Register() {
  return (
    <Suspense>
      <RegisterForm />
    </Suspense>
  );
}
