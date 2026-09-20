"use client";

import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import { apiGet, clearTokens, getTokens, setTokens } from "@/lib/api-client";
import { useT } from "@/lib/i18n";

/* Mirrors frontend/src/api/auth/index.ts LoginResponse + UserInfo shapes (subset). */

export type UserInfo = {
  id: string;
  username: string;
  email: string;
  avatar?: string;
  tenant_id?: number;
  can_access_all_tenants?: boolean;
  is_system_admin?: boolean;
  preferences?: { language?: string | null };
};

export type TenantInfo = {
  id: number | string;
  name: string;
  description?: string;
};

type AuthState = {
  user: UserInfo | null;
  tenant: TenantInfo | null;
  memberships: Array<{ tenant_id: number; tenant_name?: string; role: string }>;
  isLoggedIn: boolean;
  ready: boolean;
};

const AuthContext = createContext<
  AuthState & {
    login: (email: string, password: string) => Promise<string | null>;
    logout: () => Promise<void>;
    refreshMe: () => Promise<boolean>;
    selectedTenantId: string | null;
    setSelectedTenant: (id: string | null) => void;
  }
>({
  user: null,
  tenant: null,
  memberships: [],
  isLoggedIn: false,
  ready: false,
  login: async () => null,
  logout: async () => {},
  refreshMe: async () => false,
  selectedTenantId: null,
  setSelectedTenant: () => {},
});

const TENANT_KEY = "weknora_selected_tenant_id";

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<UserInfo | null>(null);
  const [tenant, setTenant] = useState<TenantInfo | null>(null);
  const [memberships, setMemberships] = useState<AuthState["memberships"]>([]);
  const [ready, setReady] = useState(false);
  const [selectedTenantId, setSelectedTenantId] = useState<string | null>(null);
  const { setLocale } = useT();

  useEffect(() => {
    try {
      setSelectedTenantId(localStorage.getItem(TENANT_KEY));
    } catch {
      /* ignore */
    }
    void refreshMe();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const refreshMe = useCallback(async () => {
    if (!getTokens().token) {
      setReady(true);
      return false;
    }
    try {
      const res = await apiGet<{
        success: boolean;
        data?: {
          user: UserInfo;
          tenant?: TenantInfo | null;
          memberships?: AuthState["memberships"];
        };
      }>("/api/v1/auth/me");
      if (res.success && res.data?.user) {
        setUser(res.data.user);
        setTenant(res.data.tenant ?? null);
        setMemberships(res.data.memberships ?? []);
        /* The account's saved UI language wins over this browser's
         * localStorage; persist=false so applying it doesn't echo a
         * preferences write back to the server. */
        const savedLang = res.data.user.preferences?.language;
        if (savedLang === "en" || savedLang === "vi") setLocale(savedLang, false);
        setReady(true);
        return true;
      }
    } catch {
      /* fall through */
    }
    clearTokens();
    setUser(null);
    setReady(true);
    return false;
  }, [setLocale]);

  const login = useCallback(
    async (email: string, password: string) => {
      const res = await fetch("/api/v1/auth/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, password }),
      });
      const data = (await res.json()) as {
        success: boolean;
        message?: string;
        token?: string;
        refresh_token?: string;
      };
      if (!res.ok || !data.success || !data.token) {
        return data.message ?? "Sign in failed";
      }
      setTokens(data.token, data.refresh_token ?? "");
      await refreshMe();
      return null;
    },
    [refreshMe],
  );

  const logout = useCallback(async () => {
    try {
      const { token } = getTokens();
      if (token) {
        await fetch("/api/v1/auth/logout", {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            Authorization: `Bearer ${token}`,
          },
          body: "{}",
        });
      }
    } catch {
      /* best effort */
    }
    clearTokens();
    setUser(null);
    setTenant(null);
    setMemberships([]);
  }, []);

  const setSelectedTenant = useCallback((id: string | null) => {
    setSelectedTenantId(id);
    try {
      if (id) localStorage.setItem(TENANT_KEY, id);
      else localStorage.removeItem(TENANT_KEY);
    } catch {
      /* ignore */
    }
  }, []);

  const value = useMemo(
    () => ({
      user,
      tenant,
      memberships,
      isLoggedIn: Boolean(user),
      ready,
      login,
      logout,
      refreshMe,
      selectedTenantId,
      setSelectedTenant,
    }),
    [user, tenant, memberships, ready, login, logout, refreshMe, selectedTenantId, setSelectedTenant],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  return useContext(AuthContext);
}
