import { SystemTabLayout } from "@/components/system/tab-layout";

export default function EnginesLayout({ children }: { children: React.ReactNode }) {
  return <SystemTabLayout minRole="system">{children}</SystemTabLayout>;
}
