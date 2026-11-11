"use client";

import { Suspense } from "react";
import { LegalLookupView } from "@/components/legal-lookup/legal-lookup-view";

export default function LegalLookupPage() {
  return (
    <Suspense>
      <LegalLookupView />
    </Suspense>
  );
}
