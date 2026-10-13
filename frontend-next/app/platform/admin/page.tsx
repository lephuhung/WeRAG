/* SystemAdmin console — a full page instead of the cramped settings modal:
 * a left rail of sections (dashboard and administration) and a wide content
 * pane. The active section lives in ?section= so every view
 * is deep-linkable; the old /platform/system/* routes redirect here. */
"use client";

import { Suspense } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { useT } from "@/lib/i18n";
import { RequireSystemAccess } from "@/components/require-system-access";
import { Select } from "@/components/select";
import { ADMIN_GROUPS, type AdminSection } from "@/components/admin/admin-sections";
import { adminSectionHref } from "@/components/admin/admin-routes";

const ALL_SECTIONS = ADMIN_GROUPS.flatMap((g) => g.items);

export default function AdminPage() {
  return (
    <RequireSystemAccess>
      <Suspense fallback={null}>
        <AdminConsole />
      </Suspense>
    </RequireSystemAccess>
  );
}

function AdminConsole() {
  const { t } = useT();
  const router = useRouter();
  const searchParams = useSearchParams();
  const sectionParam = searchParams.get("section") ?? "dashboard";
  const active = ALL_SECTIONS.find((s) => s.key === sectionParam) ?? ALL_SECTIONS[0];

  const label = (s: AdminSection) => (s.labelKey ? t(s.labelKey) : s.fallback);
  const open = (key: string) => router.push(adminSectionHref(key), { scroll: false });

  return (
    <div className="flex min-h-0 flex-1 flex-col lg:flex-row">
      {/* left rail */}
      <aside className="shrink-0 border-b border-hairline lg:w-[248px] lg:border-b-0 lg:border-r">
        <div className="px-5 pb-1 pt-5">
          <h1 className="title-md">{t("adminPage.title")}</h1>
        </div>
        <nav className="hidden min-h-0 flex-col gap-0.5 overflow-y-auto px-3 py-3 lg:flex">
          {ADMIN_GROUPS.map((g) => (
            <div key={g.key} className="mb-1">
              <div className="caption-uppercase px-3 pb-1.5 pt-2.5 text-muted-soft">{t(g.labelKey)}</div>
              {g.items.map((s) => (
                <button
                  key={s.key}
                  type="button"
                  onClick={() => open(s.key)}
                  className={`nav-item w-full ${active.key === s.key ? "active" : ""}`}
                >
                  <s.icon className="h-[18px] w-[18px] shrink-0" />
                  <span className="min-w-0 flex-1 truncate text-left">{label(s)}</span>
                </button>
              ))}
            </div>
          ))}
        </nav>
        {/* Narrow screens: the rail collapses into one dropdown. */}
        <div className="px-5 py-3 lg:hidden">
          <Select
            value={active.key}
            onChange={open}
            options={ALL_SECTIONS.map((s) => ({ value: s.key, label: label(s) }))}
          />
        </div>
      </aside>

      {/* content pane */}
      <div className="min-h-0 min-w-0 flex-1 overflow-y-auto">
        <div className="px-5 py-6 sm:px-8">
          {active.key !== "dashboard" && <h2 className="display-sm mb-5">{label(active)}</h2>}
          <Suspense
            fallback={
              <div className="flex items-center justify-center py-16">
                <span className="caption text-muted">{t("adm.loading")}</span>
              </div>
            }
          >
            {/* key remounts the section so its local state never leaks
             * across sections. */}
            <div key={active.key}>{active.render()}</div>
          </Suspense>
        </div>
      </div>
    </div>
  );
}
