"use client";

/* Header actions of the landing page: "Đăng nhập" for a visitor; the
 * signed-in person's name and avatar when this browser still holds a
 * session. The page lives outside AuthProvider (and must never bounce a
 * visitor to /login), so it reads /auth/me itself and refreshes an expired
 * access token once without the app's sign-in redirect. */

import Link from "next/link";
import { useEffect, useState } from "react";
import { getTokens, setTokens } from "@/lib/api-client";
import { useAvatarUrl } from "@/lib/avatar";
import type { UserInfo } from "@/lib/auth";
import styles from "./page.module.css";

async function fetchMe(token: string): Promise<Response> {
  return fetch("/api/v1/auth/me", { headers: { Authorization: `Bearer ${token}` }, cache: "no-store" });
}

async function refreshOnce(refreshToken: string): Promise<string | null> {
  const res = await fetch("/api/v1/auth/refresh", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ refreshToken }),
  });
  const data = await res.json().catch(() => null);
  const token: string | undefined = data?.access_token ?? data?.data?.token;
  if (!res.ok || !data?.success || !token) return null;
  setTokens(token, data?.refresh_token ?? data?.data?.refreshToken ?? refreshToken);
  return token;
}

async function loadSignedInUser(signal: AbortSignal): Promise<UserInfo | null> {
  const { token, refreshToken } = getTokens();
  if (!token && !refreshToken) return null;
  let res = token ? await fetchMe(token) : null;
  if ((!res || res.status === 401) && refreshToken && !signal.aborted) {
    const fresh = await refreshOnce(refreshToken);
    res = fresh ? await fetchMe(fresh) : null;
  }
  if (!res?.ok) return null;
  const body = await res.json().catch(() => null);
  return body?.success && body.data?.user ? (body.data.user as UserInfo) : null;
}

export function LandingAccount() {
  const [user, setUser] = useState<UserInfo | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    loadSignedInUser(controller.signal)
      .then((u) => { if (!controller.signal.aborted) setUser(u); })
      .catch(() => {});
    return () => controller.abort();
  }, []);

  const avatarUrl = useAvatarUrl(user?.avatar);
  const name = user?.username || user?.email || "";

  return <div className={styles.headerActions}>
    {user
      ? <Link href="/platform/creatChat" className={styles.account} title={user.email}>
        <span className={styles.avatar} aria-hidden="true">
          {/* eslint-disable-next-line @next/next/no-img-element -- authenticated blob URL, not a Next asset */}
          {avatarUrl ? <img src={avatarUrl} alt="" /> : name.slice(0, 1).toUpperCase()}
        </span>
        <span className={styles.accountName}>{name}</span>
      </Link>
      : <Link href="/login" className={styles.login}>Đăng nhập</Link>}
    <Link href="/platform/creatChat" className={styles.primary}>Vào ứng dụng <span aria-hidden="true">↗</span></Link>
  </div>;
}
