import type { Metadata } from "next";
import { LegalPage } from "@/components/legal/legal-page";

export const metadata: Metadata = { title: "Hỗ trợ — weRAG" };

export default function Page() {
  return <LegalPage page="support" />;
}
