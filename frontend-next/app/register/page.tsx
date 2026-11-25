"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useEffect, useState } from "react";
import { apiGet, apiPost, getTokens, setTokens } from "@/lib/api-client";
import { getAuthConfig, getInvitationByToken } from "@/lib/api/auth";
import { acceptInvitationByToken } from "@/lib/api/tenants";
import Link from "next/link";
import { AuthShell, authStyles as styles } from "@/components/auth/auth-shell";
import { AuthCard } from "@/components/auth/auth-card";
import { useT } from "@/lib/i18n";
import { IconEye } from "@/components/icons";
import { registerErrorKey, userErrorMessage } from "@/lib/user-error";
import { DEFAULT_POST_LOGIN_ROUTE, resolveOptionalNextPath } from "@/lib/safe-next";
import { withNext } from "@/lib/plan-checkout";

function RegisterForm() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const { t } = useT();
  const token = searchParams.get("token") ?? "";
  /* Where to go once signed in (a plan picked on the pricing table brings
   * the shop link): validated like the login page's ?next=. */
  const rawNext = searchParams.get("next");
  const next = resolveOptionalNextPath(rawNext);
  const [showPassword, setShowPassword] = useState(false);
  const [username, setUsername] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [tenantName, setTenantName] = useState<string | null>(null);
  const [inviteOnly, setInviteOnly] = useState(false);
  const [complexPassword, setComplexPassword] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  /* Signed-in visitors landing on an invite link can join with their
   * current account (auth/me/invitations/accept-by-token) instead of
   * registering a second one — they can still opt into the form below. */
  const [hasSession] = useState(() => !!getTokens().token);
  const [useNewAccount, setUseNewAccount] = useState(false);
  const showJoinCard = !!token && hasSession && !useNewAccount;
  /* Someone already signed in who reaches the sign-up form (a plan clicked
   * before the landing page knew the session) goes on to where the link
   * was heading; invite links keep their join card. */
  useEffect(() => {
    if (!token && hasSession) router.replace(next || DEFAULT_POST_LOGIN_ROUTE);
  }, [token, hasSession, next, router]);

  useEffect(() => {
    let alive = true;
    getAuthConfig()
      .then((c) => {
        if (!alive) return;
        setInviteOnly(c.registration_mode === "invite_only");
        setComplexPassword(!!c.complex_password_enabled);
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
      if (!res.success) throw new Error(res.message ?? t("auth.registerFailed"));
      // Self-serve registration answers with the account only (the invite
      // path also signs in): sign in with what was just typed.
      const session = res.token
        ? res
        : await apiPost<{ success: boolean; message?: string; token?: string; refresh_token?: string }>(
            "/api/v1/auth/login",
            { email, password },
          );
      if (!session.success || !session.token) {
        router.push(withNext("/login", next));
        return;
      }
      setTokens(session.token, session.refresh_token ?? "");
      // Mirror persistLoginResponse: probe /auth/me, then onboard only when
      // this deployment lets users create organizations (onboarding offers
      // to skip to the chat). Otherwise a new account stays a Guest until a
      // SuperAdmin or Tenant Admin adds it to an organization, and meanwhile
      // chats with the basic assistants in the public portal.
      const me = await apiGet<{
        success: boolean;
        data?: { tenant?: unknown; capabilities?: { can_create_tenant?: boolean } };
      }>(`/api/v1/auth/me`).catch(() => null);
      const hasTenant = !!me?.data?.tenant;
      const canCreate = !!me?.data?.capabilities?.can_create_tenant;
      // a picked plan's shop link (a Tổ chức plan detours through
      // onboarding there) or any other page the visitor was heading to
      if (next) {
        router.push(next);
        return;
      }
      router.push(hasTenant ? "/platform/knowledge-bases" : canCreate ? "/onboarding/workspace" : "/platform/creatChat");
    } catch (err) {
      const key = registerErrorKey(err);
      setError(key ? t(key) : userErrorMessage(err, t, "auth.registerFailed"));
    } finally {
      setBusy(false);
    }
  };

  const joinWithSession = async () => {
    setBusy(true);
    setError(null);
    try {
      const res = await acceptInvitationByToken(token);
      if (!res.success) throw new Error(res.message ?? t("auth.joinFailed"));
      router.push("/platform/knowledge-bases");
    } catch (err) {
      setError(userErrorMessage(err, t, "auth.joinFailed"));
    } finally {
      setBusy(false);
    }
  };

  const tenant = tenantName ?? t("auth.orgFallback");

  return (
    <AuthShell mode="register" next={next}>
      <AuthCard
        kicker={t("auth.registerKicker")}
        title={token ? t("auth.joinOrgTitle") : t("auth.createAccountTitle")}
        subtitle={
          token
            ? showJoinCard
              ? t("auth.joinOrgSubtitleSession", { tenant })
              : t("auth.joinOrgSubtitle", { tenant })
            : t("auth.createAccountSubtitle")
        }
        note={t("auth.registerNote")}
      >
        {inviteOnly && !token ? (
          <div role="status" className={styles.error}>
            {t("auth.inviteOnly")}
            <div className={styles.links}>
              <Link href={withNext("/login", next)}>{t("auth.backToLogin")}</Link>
            </div>
          </div>
        ) : showJoinCard ? (
          <>
            {error && (
              <p role="alert" className={styles.error}>
                {error}
              </p>
            )}
            <button
              type="button"
              className="btn btn-primary h-11 w-full"
              disabled={busy}
              onClick={() => void joinWithSession()}
            >
              {busy ? t("auth.joiningBusy") : t("auth.joinOrgSubmit")}
            </button>
            <button
              type="button"
              className="btn btn-outline h-11 w-full"
              onClick={() => setUseNewAccount(true)}
            >
              {t("auth.createNewAccount")}
            </button>
          </>
        ) : (
          <form onSubmit={submit}>
            <label className="mb-4 block">
              <span className="caption mb-1.5 block text-muted">{t("auth.username")}</span>
              <input
                className="input"
                autoComplete="username"
                placeholder={t("auth.usernamePlaceholder")}
                required
                value={username}
                onChange={(e) => setUsername(e.target.value)}
              />
            </label>
            <label className="mb-4 block">
              <span className="caption mb-1.5 block text-muted">{t("auth.email")}</span>
              <input
                className="input"
                type="email"
                autoComplete="email"
                placeholder={t("auth.emailPlaceholder")}
                required
                value={email}
                onChange={(e) => setEmail(e.target.value)}
              />
            </label>
            {/* a div, not a wrapping <label>: the toggle button's name must
                not become part of the password field's label (login page
                markup) */}
            <div className="mb-6">
              <label htmlFor="register-password" className="caption mb-1.5 block text-muted">
                {t("auth.password")}
              </label>
              <div className="relative">
                <input
                  id="register-password"
                  className="input pr-12"
                  type={showPassword ? "text" : "password"}
                  autoComplete="new-password"
                  placeholder={t("auth.passwordPlaceholderNew")}
                  required
                  aria-describedby="register-password-rule"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                />
                <button
                  type="button"
                  className="absolute inset-y-0 right-1 flex w-10 items-center justify-center rounded text-muted transition-colors hover:text-ink"
                  aria-label={showPassword ? t("auth.hidePassword") : t("auth.showPassword")}
                  aria-pressed={showPassword}
                  aria-controls="register-password"
                  onClick={() => setShowPassword((visible) => !visible)}
                >
                  <span className="relative" aria-hidden="true">
                    <IconEye className="h-5 w-5" />
                    {showPassword && <span className="absolute left-0 top-1/2 h-px w-5 -rotate-45 bg-current" />}
                  </span>
                </button>
              </div>
              <p id="register-password-rule" className="caption mt-1.5 text-muted">
                {t(complexPassword ? "auth.passwordRuleComplex" : "auth.passwordRule")}
              </p>
            </div>
            {error && (
              <p role="alert" className={styles.error}>
                {error}
              </p>
            )}
            <button type="submit" className="btn btn-primary h-11 w-full" disabled={busy}>
              {busy
                ? t("auth.registerBusy")
                : token
                  ? t("auth.registerJoinSubmit")
                  : t("auth.registerSubmit")}
            </button>
          </form>
        )}
      </AuthCard>
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
