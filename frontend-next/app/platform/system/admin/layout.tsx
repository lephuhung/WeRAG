import { SystemTabLayout } from "@/components/system/tab-layout";

export default function AdminLayout({ children }: { children: React.ReactNode }) {
  return <SystemTabLayout minRole="system">{children}</SystemTabLayout>;
}
