/* Extensions hub — compact cards for platform extensions (system-gated)
 * and workspace tooling configs moved out of the workspace tab. Small
 * config panels open in a modal; catalogs keep their own routes.
 * ?tab=mcp / ?tab=skills URLs from the old single-page layout redirect. */
"use client";

import { Suspense, useEffect } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { useT } from "@/lib/i18n";
import { SectionCardGrid, type SectionCard } from "@/components/system/section-cards";
import {
  IconAgent,
  IconArtifact,
  IconBook,
  IconCode,
  IconExternal,
  IconGraph,
} from "@/components/icons";
import { BrowserConnectionSettings } from "@/components/settings/browser-connection-settings";
import { EnvVarsSettings } from "@/components/settings/env-vars-settings";

const E = "/platform/system/extensions";

const TAB_ROUTES: Record<string, string> = {
  mcp: `${E}/mcp`,
  skills: `${E}/skills`,
  agents: `${E}/agents`,
};

export default function ExtensionsPage() {
  return (
    <Suspense fallback={null}>
      <ExtensionsHub />
    </Suspense>
  );
}

function ExtensionsHub() {
  const router = useRouter();
  const params = useSearchParams();
  const tab = params.get("tab");
  const { t } = useT();

  useEffect(() => {
    if (tab && TAB_ROUTES[tab]) router.replace(TAB_ROUTES[tab]);
  }, [tab, router]);

  if (tab && TAB_ROUTES[tab]) return null;

  const platformCards: SectionCard[] = [
    {
      key: "agents",
      title: t("systemNav.agents"),
      desc: "Custom agents, IM channels and embed widgets.",
      icon: <IconAgent className="h-5 w-5" />,
      href: `${E}/agents`,
      minRole: "system",
    },
    {
      key: "mcp",
      title: t("systemNav.mcpServers"),
      desc: "Connect MCP tool servers available to agents.",
      icon: <IconGraph className="h-5 w-5" />,
      href: `${E}/mcp`,
      minRole: "system",
    },
    {
      key: "skills",
      title: t("settingsNav.skills"),
      desc: "Installable skill catalog for agents.",
      icon: <IconArtifact className="h-5 w-5" />,
      href: `${E}/skills`,
      minRole: "system",
    },
  ];

  const toolCards: SectionCard[] = [
    {
      key: "browserconnection",
      title: t("systemNav.browserconnection"),
      desc: "Connect a local browser for agent-driven browsing.",
      icon: <IconExternal className="h-5 w-5" />,
      content: <BrowserConnectionSettings />,
      modalWidth: "w-[560px]",
    },
    {
      key: "envvars",
      title: t("settingsNav.envvars"),
      desc: "Environment variables injected into agent runs.",
      icon: <IconCode className="h-5 w-5" />,
      content: <EnvVarsSettings />,
      modalWidth: "w-[560px]",
    },
    {
      key: "abbreviations",
      title: t("settingsNav.abbreviations"),
      desc: "Terms expanded in chat and knowledge queries.",
      icon: <IconBook className="h-5 w-5" />,
      href: `${E}/abbreviations`,
    },
  ];

  return (
    <div>
      <div className="caption-uppercase mb-3 text-muted-soft">
        {t("systemNav.extensions")}
      </div>
      <SectionCardGrid cards={platformCards} />
      <div className="caption-uppercase mb-3 mt-8 text-muted-soft">
        {t("settingsNav.groups.workspace")}
      </div>
      <SectionCardGrid cards={toolCards} />
    </div>
  );
}
