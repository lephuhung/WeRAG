/* Workspace tab — card hub for personal and tenant settings. Small config
 * panels open in a modal; table-style managers keep their own routes.
 * ?section=<key> URLs from the pre-routes layout redirect to the matching
 * route (including keys that moved to /platform/system/extensions). */
"use client";

import { Suspense, useEffect } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { useT } from "@/lib/i18n";
import { SectionCardGrid, type SectionCard } from "@/components/system/section-cards";
import {
  IconBookmark,
  IconClock,
  IconCode,
  IconGlobe,
  IconInfoCircle,
  IconOrg,
  IconSettings,
} from "@/components/icons";
import { GeneralSettings } from "@/components/settings/general-settings";
import { TenantInfo } from "@/components/settings/tenant-info";
import { ChatHistorySettings } from "@/components/settings/chat-history-settings";
import { MemoryWorkspaceSettings } from "@/components/settings/memory-workspace-settings";

const W = "/platform/system/workspace";
const E = "/platform/system/extensions";

const SECTION_ROUTES: Record<string, string> = {
  general: W,
  mymemory: `${W}/mymemory`,
  tenant: `${W}/tenant`,
  members: `${W}/members`,
  sharing: `${W}/sharing`,
  "api-keys": `${W}/api-keys`,
  orgs: `${W}/orgs`,
  chathistory: `${W}/chathistory`,
  memory: `${W}/memory`,
  envvars: `${E}/envvars`,
  browserconnection: `${E}/browserconnection`,
  abbreviations: `${E}/abbreviations`,
};

export default function WorkspacePage() {
  return (
    <Suspense fallback={null}>
      <WorkspaceHub />
    </Suspense>
  );
}

function WorkspaceHub() {
  const router = useRouter();
  const params = useSearchParams();
  const section = params.get("section");
  const { t } = useT();

  useEffect(() => {
    if (!section) return;
    router.replace(SECTION_ROUTES[section] ?? W);
  }, [section, router]);

  if (section) return null;

  const accountCards: SectionCard[] = [
    {
      key: "general",
      title: t("settingsNav.general"),
      desc: "Language, theme and display preferences.",
      icon: <IconSettings className="h-5 w-5" />,
      content: <GeneralSettings />,
    },
    {
      key: "mymemory",
      title: t("settingsNav.mymemory"),
      desc: "Review and manage what the agent remembers about you.",
      icon: <IconBookmark className="h-5 w-5" />,
      href: `${W}/mymemory`,
    },
  ];

  const workspaceCards: SectionCard[] = [
    {
      key: "tenant",
      title: t("settingsNav.tenant"),
      desc: "Workspace profile, storage usage and danger zone.",
      icon: <IconInfoCircle className="h-5 w-5" />,
      content: <TenantInfo />,
    },
    {
      key: "members",
      title: t("settingsNav.members"),
      desc: "Invite people and manage workspace roles.",
      icon: <IconOrg className="h-5 w-5" />,
      href: `${W}/members`,
      minRole: "admin",
    },
    {
      key: "sharing",
      title: t("settingsNav.sharing"),
      desc: "Cross-workspace knowledge-base access requests.",
      icon: <IconGlobe className="h-5 w-5" />,
      href: `${W}/sharing`,
      minRole: "admin",
    },
    {
      key: "api-keys",
      title: t("settingsNav.apiKeys"),
      desc: "Programmatic access tokens for this workspace.",
      icon: <IconCode className="h-5 w-5" />,
      href: `${W}/api-keys`,
      minRole: "admin",
    },
    {
      key: "orgs",
      title: t("settingsNav.orgs"),
      desc: "Shared teams grouping members and knowledge bases.",
      icon: <IconOrg className="h-5 w-5" />,
      href: `${W}/orgs`,
      minRole: "admin",
    },
    {
      key: "chathistory",
      title: t("systemNav.chathistory"),
      desc: "Index chat history into a knowledge base for retrieval.",
      icon: <IconClock className="h-5 w-5" />,
      content: <ChatHistorySettings />,
      minRole: "admin",
    },
    {
      key: "memory",
      title: t("settingsNav.memory"),
      desc: "Workspace-level memory extraction and retention.",
      icon: <IconBookmark className="h-5 w-5" />,
      content: <MemoryWorkspaceSettings />,
      minRole: "admin",
    },
  ];

  return (
    <div>
      <div className="caption-uppercase mb-3 text-muted-soft">
        {t("settingsNav.groups.account")}
      </div>
      <SectionCardGrid cards={accountCards} />
      <div className="caption-uppercase mb-3 mt-8 text-muted-soft">
        {t("settingsNav.groups.workspace")}
      </div>
      <SectionCardGrid cards={workspaceCards} />
    </div>
  );
}
