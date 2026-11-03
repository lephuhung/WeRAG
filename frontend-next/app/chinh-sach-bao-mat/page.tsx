import type { Metadata } from "next";
import { LegalPage } from "@/components/legal/legal-page";

export const metadata: Metadata = { title: "Chính sách quyền riêng tư — weRAG" };

export default function Page() {
  return <LegalPage page="privacy" />;
}
