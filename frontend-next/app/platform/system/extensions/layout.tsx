import { SystemTabLayout } from "@/components/system/tab-layout";

/* Extensions is member-visible: workspace tooling configs (env vars, browser
 * connection, abbreviations) live here too. Platform items are gated per
 * page. */
export default function ExtensionsLayout({ children }: { children: React.ReactNode }) {
  return <SystemTabLayout>{children}</SystemTabLayout>;
}
