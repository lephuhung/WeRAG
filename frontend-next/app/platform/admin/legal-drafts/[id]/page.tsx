import { Suspense } from "react";
import { RequireSystemAccess } from "@/components/require-system-access";
import { LegalDraftReview } from "@/components/legal-drafts/legal-draft-review";

/* Review of one legal OCR draft (SuperAdmin): the original PDF scan beside
 * the OCRed text in the editor, the structure check and approval. */
export default async function LegalDraftPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return (
    <RequireSystemAccess>
      <Suspense fallback={null}>
        <LegalDraftReview id={id} />
      </Suspense>
    </RequireSystemAccess>
  );
}
