"use client";

import { createContext, useCallback, useContext, useEffect, useState } from "react";
import { getTokens } from "@/lib/api-client";
import { updateMyPreferences } from "@/lib/api/auth";

export type Locale = "en" | "vi";

const en = {
  "nav.chat": "Chat",
  "nav.search": "Search",
  "nav.newChat": "New chat",
  "nav.knowledgeBases": "Knowledge bases",
  "nav.artifacts": "Artifacts",
  "nav.agents": "Agents",
  "nav.organizations": "Organizations",
  "nav.system": "System",
  "nav.settings": "Settings",
  "nav.admin": "Admin",
  "hdr.workspace": "Workspace",
  "hdr.administration": "Administration",
  "user.settings": "Settings",
  "user.designSystem": "Design system",
  "user.signOut": "Sign out",
  "user.language": "Language",
  "userSettings.title": "User settings",
  "userSettings.profile": "Profile",
  "userSettings.security": "Security",
  "userSettings.connections": "Connections",
  "userSettings.changePhoto": "Change photo",
  "userSettings.remove": "Remove",
  "userSettings.displayName": "Display name",
  "userSettings.email": "Email",
  "userSettings.bio": "Bio",
  "userSettings.saveProfile": "Save profile",
  "userSettings.changePassword": "Change password",
  "userSettings.currentPassword": "Current password",
  "userSettings.newPassword": "New password",
  "userSettings.confirmPassword": "Confirm new password",
  "userSettings.updatePassword": "Update password",
  "userSettings.pwMismatch": "Passwords do not match",
  "userSettings.pwUpdated": "Password updated",
  "userSettings.2fa": "Two-factor authentication",
  "userSettings.2faDesc": "Require a TOTP code at sign-in for extra security.",
  "userSettings.2faEnabled": "Enabled",
  "userSettings.2faDisabled": "Disabled",
  "userSettings.enable2fa": "Enable 2FA",
  "userSettings.verify": "Verify",
  "userSettings.disable": "Disable",
  "userSettings.2faLinked": "Authenticator app linked",
  "userSettings.connectionsDesc": "Link external accounts for notifications and sign-in.",
  "userSettings.telegram": "Telegram",
  "userSettings.telegramLinked": "receives alerts & chat digests",
  "userSettings.telegramUnlinked": "Get alerts and chat digests via the WeRAG bot",
  "userSettings.link": "Link",
  "userSettings.unlink": "Unlink",
  "userSettings.linked": "Linked",
  "common.save": "Save",
  "common.cancel": "Cancel",
  "common.edit": "Edit",
  "common.delete": "Delete",
  "common.all": "All",
  "system.overview": "Overview",
  "system.services": "Services",
  "system.models": "Models",
  "system.users": "Users",
  "system.logs": "Logs",
  "users.addUser": "Add user",
  "users.editUser": "Edit user",
  "users.deleteUser": "Delete user",
  "users.role": "Role",
  "users.org": "Organization",
  "users.lastActive": "Last active",
  "users.actions": "Actions",
  "users.member": "Member",
  "users.admin": "Admin",
  "users.superadmin": "Superadmin",
  "users.fullName": "Full name",
  "users.adminScope": "Admin scope",
  "users.tenant": "Tenant",
  "users.adminOfOrgs": "Admin of organizations",
  "users.promote": "Make admin",
  "users.demote": "Make member",
  "users.scopeTenant": "tenant",
  "users.scopeOrgs": "orgs",
  "users.scopeAll": "all orgs",
};

const vi: Record<keyof typeof en, string> = {
  "nav.chat": "Chat",
  "nav.search": "Tìm kiếm",
  "nav.newChat": "Chat mới",
  "nav.knowledgeBases": "Kho tri thức",
  "nav.artifacts": "Thành phẩm",
  "nav.agents": "Trợ lý",
  "nav.organizations": "Tổ chức",
  "nav.system": "Hệ thống",
  "nav.settings": "Cài đặt",
  "nav.admin": "Quản trị",
  "hdr.workspace": "Không gian làm việc",
  "hdr.administration": "Quản trị",
  "user.settings": "Cài đặt",
  "user.designSystem": "Hệ thống thiết kế",
  "user.signOut": "Đăng xuất",
  "user.language": "Ngôn ngữ",
  "userSettings.title": "Cài đặt người dùng",
  "userSettings.profile": "Hồ sơ",
  "userSettings.security": "Bảo mật",
  "userSettings.connections": "Liên kết",
  "userSettings.changePhoto": "Đổi ảnh",
  "userSettings.remove": "Xoá",
  "userSettings.displayName": "Tên hiển thị",
  "userSettings.email": "Email",
  "userSettings.bio": "Giới thiệu",
  "userSettings.saveProfile": "Lưu hồ sơ",
  "userSettings.changePassword": "Đổi mật khẩu",
  "userSettings.currentPassword": "Mật khẩu hiện tại",
  "userSettings.newPassword": "Mật khẩu mới",
  "userSettings.confirmPassword": "Xác nhận mật khẩu mới",
  "userSettings.updatePassword": "Cập nhật mật khẩu",
  "userSettings.pwMismatch": "Mật khẩu không khớp",
  "userSettings.pwUpdated": "Đã cập nhật mật khẩu",
  "userSettings.2fa": "Xác thực hai lớp",
  "userSettings.2faDesc": "Yêu cầu mã TOTP khi đăng nhập để tăng bảo mật.",
  "userSettings.2faEnabled": "Đã bật",
  "userSettings.2faDisabled": "Đã tắt",
  "userSettings.enable2fa": "Bật 2FA",
  "userSettings.verify": "Xác minh",
  "userSettings.disable": "Tắt",
  "userSettings.2faLinked": "Đã liên kết ứng dụng xác thực",
  "userSettings.connectionsDesc": "Liên kết tài khoản ngoài để nhận thông báo và đăng nhập.",
  "userSettings.telegram": "Telegram",
  "userSettings.telegramLinked": "nhận cảnh báo & bản tin chat",
  "userSettings.telegramUnlinked": "Nhận cảnh báo và bản tin qua bot WeRAG",
  "userSettings.link": "Liên kết",
  "userSettings.unlink": "Huỷ liên kết",
  "userSettings.linked": "Đã liên kết",
  "common.save": "Lưu",
  "common.cancel": "Huỷ",
  "common.edit": "Sửa",
  "common.delete": "Xoá",
  "common.all": "Tất cả",
  "system.overview": "Tổng quan",
  "system.services": "Dịch vụ",
  "system.models": "Mô hình",
  "system.users": "Người dùng",
  "system.logs": "Nhật ký",
  "users.addUser": "Thêm người dùng",
  "users.editUser": "Sửa người dùng",
  "users.deleteUser": "Xoá người dùng",
  "users.role": "Vai trò",
  "users.org": "Tổ chức",
  "users.lastActive": "Hoạt động",
  "users.actions": "Thao tác",
  "users.member": "Thành viên",
  "users.admin": "Quản trị",
  "users.superadmin": "Superadmin",
  "users.fullName": "Họ tên",
  "users.adminScope": "Phạm vi quản trị",
  "users.tenant": "Tenant",
  "users.adminOfOrgs": "Admin của các tổ chức",
  "users.promote": "Nâng admin",
  "users.demote": "Hạ thành viên",
  "users.scopeTenant": "tenant",
  "users.scopeOrgs": "tổ chức",
  "users.scopeAll": "mọi tổ chức",
};

const dicts: Record<Locale, Record<string, string>> = { en, vi };
const STORAGE_KEY = "werag_locale";

export type LocaleKey = keyof typeof en;
type I18n = {
  locale: Locale;
  /* persist=false skips the server write — used when applying the locale
   * the server itself returned, which would otherwise echo it back. */
  setLocale: (l: Locale, persist?: boolean) => void;
  t: (k: LocaleKey) => string;
};
const I18nContext = createContext<I18n>({
  locale: "en",
  setLocale: () => {},
  t: (k) => en[k],
});

export function I18nProvider({ children }: { children: React.ReactNode }) {
  const [locale, setLocaleState] = useState<Locale>("en");

  useEffect(() => {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (saved === "en" || saved === "vi") {
      setLocaleState(saved);
      document.documentElement.lang = saved;
    }
  }, []);

  const setLocale = useCallback((l: Locale, persist = true) => {
    setLocaleState(l);
    localStorage.setItem(STORAGE_KEY, l);
    document.documentElement.lang = l;
    /* Server copy makes the choice follow the account across devices;
     * skipped when logged out (no token) or when applying a server value. */
    if (persist && getTokens().token) {
      void updateMyPreferences({ language: l });
    }
  }, []);

  const t = (k: keyof typeof en) => dicts[locale][k] ?? en[k] ?? k;

  return <I18nContext.Provider value={{ locale, setLocale, t }}>{children}</I18nContext.Provider>;
}

export function useT() {
  return useContext(I18nContext);
}
