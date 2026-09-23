"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { useAuth } from "@/lib/auth";
import { Modal } from "@/components/modal";
import { IconChevronRight } from "@/components/icons";
import { canSeeSection, type SettingsRoleKey } from "@/components/settings/nav-config";

/* Card hub for a /platform/system tab. Compact cards replace the old left
 * sub-nav: cards with `href` navigate to a full management page, cards with
 * `content` open the settings panel in a modal (mirroring the engine-config
 * pattern) — the underlying component still has its own route for
 * deep-linking and command-palette search. */
export interface SectionCard {
  key: string;
  title: string;
  desc: string;
  icon: React.ReactNode;
  minRole?: SettingsRoleKey;
  href?: string;
  content?: React.ReactNode;
  modalWidth?: string;
}

export function SectionCardGrid({ cards }: { cards: SectionCard[] }) {
  const router = useRouter();
  const auth = useAuth();
  const [open, setOpen] = useState<string | null>(null);

  const isSystemAdmin = auth.user?.is_system_admin === true;
  const currentRole =
    auth.memberships.find(
      (m) => String(m.tenant_id) === String(auth.selectedTenantId ?? auth.tenant?.id ?? ""),
    )?.role ?? "";

  const visible = cards.filter((c) =>
    canSeeSection(
      { key: c.key, fallbackLabel: c.title, minRole: c.minRole ?? "member" },
      currentRole,
      isSystemAdmin,
    ),
  );

  const active = visible.find((c) => c.key === open && c.content);

  return (
    <>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
        {visible.map((c) => (
          <button
            key={c.key}
            type="button"
            onClick={() => (c.href ? router.push(c.href) : setOpen(c.key))}
            className="card card-hover group flex items-start gap-3.5 p-5 text-left transition-all duration-150 hover:border-hairline-strong"
          >
            <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl border border-hairline bg-surface-strong text-muted transition-colors group-hover:text-ink">
              {c.icon}
            </div>
            <div className="min-w-0 flex-1">
              <div className="flex items-center justify-between gap-2">
                <span className="truncate text-[14px] font-semibold text-ink">{c.title}</span>
                <IconChevronRight className="h-4 w-4 shrink-0 text-muted-soft transition-colors group-hover:text-ink" />
              </div>
              <p className="caption mt-1 line-clamp-2 text-muted">{c.desc}</p>
            </div>
          </button>
        ))}
      </div>

      <Modal
        open={active != null}
        title={active?.title ?? ""}
        onClose={() => setOpen(null)}
        width={active?.modalWidth ?? "w-[720px]"}
      >
        {active?.content}
      </Modal>
    </>
  );
}
