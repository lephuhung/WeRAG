"use client";

import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import { apiGet, clearTokens, getTokens, setTokens } from "@/lib/api-client";
import { loadWithStaleTenantRecovery } from "@/lib/auth-recovery";
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
    /* A stored tenant id can go stale while the session is still valid —
     * the loader below retries once without X-Tenant-ID, so a stale
     * selection (or a transient blip) no longer signs the user out. */
    let tenantStored = false;
    try {
      tenantStored = localStorage.getItem(TENANT_KEY) != null;
    } catch {
      /* private mode */
    }
    const outcome = await loadWithStaleTenantRecovery(
      (skipTenant) =>
        apiGet<{
          success: boolean;
          data?: {
            user: UserInfo;
            tenant?: TenantInfo | null;
            memberships?: AuthState["memberships"];
          };
        }>("/api/v1/auth/me", skipTenant ? { skipTenant: true } : undefined),
      tenantStored,
    );
    if (outcome.kind === "transient") {
      /* Network blip / 5xx: retain tokens and session state — the next
       * refreshMe retries with the same credentials. */
      setReady(true);
      return false;
    }
    if (outcome.kind === "ok") {
      const res = outcome.value;
      if (res.success && res.data?.user) {
        setUser(res.data.user);
        setTenant(res.data.tenant ?? null);
        setMemberships(res.data.memberships ?? []);
        /* Seed the X-Tenant-ID override from the session tenant when nothing
         * is stored. Without this, weknora_selected_tenant_id stays empty
         * after login and the header is never sent — every request silently
         * scopes to the JWT tenant even after the user "switches". Matches
         * Vue's effective-tenant fallback (selectedTenantId || tenant.id).
         * After a stale-tenant recovery the stored id is known-bad: drop it
         * first so the session tenant below replaces it instead of the
         * stale value surviving. */
        if (outcome.recoveredFromStaleTenant) {
          try {
            localStorage.removeItem(TENANT_KEY);
          } catch {
            /* private mode */
          }
          setSelectedTenantId(null);
        }
        const sessionTenantId = res.data.tenant?.id ?? res.data.user?.tenant_id;
        if (sessionTenantId != null && Number(sessionTenantId) > 0) {
          try {
            const current = localStorage.getItem(TENANT_KEY);
            if (!current || current === "undefined" || current === "null") {
              const sid = String(sessionTenantId);
              localStorage.setItem(TENANT_KEY, sid);
              setSelectedTenantId(sid);
            }
          } catch {
            /* private mode */
          }
        }
        /* The account's saved UI language wins over this browser's
         * localStorage; persist=false so applying it doesn't echo a
         * preferences write back to the server. */
        const savedLang = res.data.user.preferences?.language;
        if (savedLang === "en" || savedLang === "vi") setLocale(savedLang, false);
        setReady(true);
        return true;
      }
    }
    /* Confirmed invalid authentication (401/403 even without the tenant
     * header) or a success envelope with no user: the session is unusable. */
    clearTokens();
    setUser(null);
    setReady(true);
    return false;
  }, [setLocale]);

  const login = useCallback(
    async (email: string, password: string) => {
      const res = await fetch("/api/v1/auth/login", {
        method: "POST",
        headers: { "Content-Type": "application/json", "Accept-Language": "zh-CN" },
        body: JSON.stringify({ email, password }),
      });
      const data = (await res.json()) as {
        success: boolean;
        message?: string;
        token?: string;
        refresh_token?: string;
        active_tenant?: { id?: number | string };
        tenant?: { id?: number | string };
        user?: { tenant_id?: number | string };
      };
      if (!res.ok || !data.success || !data.token) {
        return data.message ?? "Sign in failed";
      }
      setTokens(data.token, data.refresh_token ?? "");
      const tenantId =
        data.active_tenant?.id ?? data.tenant?.id ?? data.user?.tenant_id;
      if (tenantId && Number(tenantId) > 0) {
        localStorage.setItem(TENANT_KEY, String(tenantId));
        setSelectedTenantId(String(tenantId));
      }
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
            "Accept-Language": "zh-CN",
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
    setSelectedTenantId(null);
    try {
      localStorage.removeItem(TENANT_KEY);
    } catch {
      /* ignore */
    }
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

/* Role of the current user inside the active workspace, plus the common
 * permission booleans. The human role model is Admin | Member at tenant
 * scope plus platform SuperAdmin (user.is_system_admin); "owner" is a
 * legacy backend alias treated as admin until the reviewed migration.
 * A missing/unknown role means member-level (fail closed). */
export function useTenantRole() {
  const auth = useAuth();
  const role =
    auth.memberships.find(
      (m) => String(m.tenant_id) === String(auth.selectedTenantId ?? auth.tenant?.id ?? ""),
    )?.role ?? "";
  const isSystemAdmin = auth.user?.is_system_admin === true;
  const isOwner = isSystemAdmin || role === "owner";
  const isTenantAdmin = isSystemAdmin || role === "admin" || role === "owner";
  return { role, isSystemAdmin, isOwner, isAdminOrOwner: isOwner || role === "admin", isTenantAdmin };
}
