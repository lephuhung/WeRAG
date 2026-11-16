/* Integrations landing: one card per integration surface, each opening the
 * Settings section that configures it (the Vue landing pages and the weknora
 * CLI were removed in October 2026). A card the user may not open is hidden,
 * using the same role rule as the Settings modal. The tenant MCP-endpoint
 * API has no UI yet, so it has no card. */
"use client";

import { useT } from "@/lib/i18n";
import { useAuth } from "@/lib/auth";
import { canSeeSection, type SettingsRoleKey } from "@/components/settings/nav-config";

type Card = {
  /** Settings modal section key (components/settings-modal.tsx). */
  section: string;
  minRole: SettingsRoleKey;
  labelEn: string;
  labelVi: string;
  descEn: string;
  descVi: string;
};

const CARDS: Card[] = [
  { section: "browserconnection", minRole: "member", labelEn: "Chrome extension", labelVi: "Tiện ích Chrome", descEn: "Ask your knowledge bases from any page.", descVi: "Hỏi kho tri thức ngay trên mọi trang web." },
  { section: "skills", minRole: "system", labelEn: "Claw skills", labelVi: "Kỹ năng Claw", descEn: "Publish skills the agent sandbox can install.", descVi: "Công bố kỹ năng để sandbox cài đặt." },
  { section: "imchannels", minRole: "system", labelEn: "Instant messaging", labelVi: "Tin nhắn tức thời", descEn: "Bring the agent into IM channels.", descVi: "Đưa trợ lý vào các kênh tin nhắn." },
  { section: "api-keys", minRole: "system", labelEn: "HTTP API", labelVi: "HTTP API", descEn: "Serve RAG answers to your own product.", descVi: "Cung cấp câu trả lời RAG cho sản phẩm của bạn." },
];

export default function Integrations() {
  const { t, locale } = useT();
  const auth = useAuth();
  const isSystemAdmin = auth.user?.is_system_admin === true;
  const currentRole =
    auth.memberships.find((m) => String(m.tenant_id) === String(auth.selectedTenantId ?? auth.tenant?.id ?? ""))?.role ?? "";
  const cards = CARDS.filter((c) =>
    canSeeSection({ key: c.section, fallbackLabel: c.labelEn, minRole: c.minRole }, currentRole, isSystemAdmin),
  );

  return (
    <div className="flex-1 overflow-y-auto">
      <div className="mx-auto w-full max-w-[900px] px-4 py-6 sm:px-8 sm:py-10 lg:px-12">
        <div className="caption-uppercase mb-3 text-muted">{t("settingsNav.groups.workspace")}</div>
        <h1 className="display-xl mb-10">{t("integrations.title")}</h1>

        <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
          {cards.map((c) => (
            <button
              key={c.section}
              onClick={() => window.dispatchEvent(new CustomEvent("weknora:open-settings", { detail: c.section }))}
              className="card card-hover p-5 text-left"
            >
              <div className="flex items-center justify-between">
                <div className="title-sm">{locale === "vi" ? c.labelVi : c.labelEn}</div>
                <span className="caption text-muted-soft">→</span>
              </div>
              <p className="body-sm mt-1.5 text-body">{locale === "vi" ? c.descVi : c.descEn}</p>
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}
