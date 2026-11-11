/* A one-line validity lookup box (guest start page): submitting opens the
 * lookup page with the query, so the guest gets an answer without spending
 * a question of the daily allowance. */
"use client";

import { useRouter } from "next/navigation";
import { useState, type FormEvent } from "react";
import { IconFileSearch } from "@/components/icons";
import { useT } from "@/lib/i18n";
import { legalLookupHref } from "@/lib/legal-status";
import { LEGAL_LOOKUP_MAX_QUERY } from "@/components/legal-lookup/legal-lookup-view";

export function LegalLookupBox({ className = "" }: { className?: string }) {
  const { t } = useT();
  const router = useRouter();
  const [q, setQ] = useState("");
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (q.trim()) router.push(legalLookupHref(q));
  };
  return (
    <form onSubmit={submit} role="search" className={`flex flex-col gap-1.5 ${className}`}>
      <label htmlFor="legal-lookup-box" className="caption text-muted">
        {t("llookup.boxLabel")}
      </label>
      <div className="flex gap-2">
        <div className="card flex flex-1 items-center gap-2 px-3 py-1.5">
          <IconFileSearch className="h-4 w-4 shrink-0 text-muted" />
          <input
            id="legal-lookup-box"
            value={q}
            maxLength={LEGAL_LOOKUP_MAX_QUERY}
            onChange={(e) => setQ(e.target.value)}
            placeholder={t("llookup.placeholder")}
            className="min-w-0 flex-1 bg-transparent text-[14px] text-ink outline-none placeholder:text-muted"
          />
        </div>
        <button type="submit" className="btn btn-outline btn-sm" disabled={!q.trim()}>
          {t("llookup.submit")}
        </button>
      </div>
    </form>
  );
}
