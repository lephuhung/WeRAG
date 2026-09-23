import { SystemTabLayout } from "@/components/system/tab-layout";

export default function ModelsLayout({ children }: { children: React.ReactNode }) {
  return <SystemTabLayout minRole="owner">{children}</SystemTabLayout>;
}
