/* The working app's frame: the chat/navigation rail, the section header and
 * the plan banner. The administration console does not use this frame — see
 * app/platform/layout.tsx for the split. */
import { Header } from "@/components/header";
import { Sidebar } from "@/components/sidebar";
import { PlanBannerBar } from "@/components/plan-banner";

export default function AppShellLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex h-dvh min-h-0 w-full bg-canvas">
      <Sidebar />
      <div className="flex min-w-0 flex-1 flex-col">
        <Header />
        <PlanBannerBar />
        <main className="flex min-h-0 flex-1 flex-col overflow-hidden">{children}</main>
      </div>
    </div>
  );
}
