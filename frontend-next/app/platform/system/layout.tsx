import { SystemTabs } from "@/components/system-tabs";

export default function SystemLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
      <div className="flex shrink-0 items-end justify-between px-10 pt-8 pb-6">
        <div>
          <div className="caption-uppercase mb-3 text-muted">Administration</div>
          <h1 className="display-lg">System</h1>
        </div>
        <SystemTabs />
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-10 pb-10">{children}</div>
    </div>
  );
}
