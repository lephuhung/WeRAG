"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { listKnowledgeBases, type KnowledgeBaseRow } from "@/lib/api/knowledge";
import { useTenantRole } from "@/lib/auth";
import { Orb } from "@/components/orb";
import { IconBook, IconDoc, IconPlus, IconSearch } from "@/components/icons";

export default function KnowledgeBaseList() {
  const { isOwner } = useTenantRole();
  const [kbs, setKbs] = useState<KnowledgeBaseRow[] | null>(null);
  const [error, setError] = useState("");
  const [q, setQ] = useState("");

  useEffect(() => {
    let alive = true;
    listKnowledgeBases()
      .then((rows) => {
        if (alive) setKbs(rows);
      })
      .catch((e) => {
        if (alive) {
          setKbs([]);
          setError(e instanceof Error ? e.message : "Failed to load knowledge bases");
        }
      });
    return () => {
      alive = false;
    };
  }, []);

  const ORBS = ["mint", "lavender", "peach", "sky", "rose"] as const;
  const rows = (kbs ?? [])
    .filter((k) => !q || k.name.toLowerCase().includes(q.toLowerCase()))
    .map((k, i) => ({
      id: k.id,
      name: k.name,
      description: k.description ?? "",
      docs: k.knowledge_count ?? k.document_count ?? 0,
      updatedAt: k.updated_at ?? "",
      orb: ORBS[i % ORBS.length],
    }));

  return (
    <div className="relative flex-1 overflow-y-auto">
      <Orb color="mint" size={520} className="-top-40 right-[-120px]" />
      <Orb color="lavender" size={420} className="bottom-[-160px] left-[-100px]" />

      <div className="relative mx-auto w-full max-w-[1200px] px-12 py-12">
        <div className="mb-10 flex items-end justify-between gap-6">
          <div>
            <div className="caption-uppercase mb-3 text-muted">Workspace</div>
            <h1 className="display-xl">Knowledge bases</h1>
            <p className="mt-3 max-w-[520px] text-body">
              Collections of documents indexed for retrieval-augmented chat.
            </p>
          </div>
          {isOwner && (
            <Link href="/platform/knowledge-bases/new" className="btn btn-primary">
              <IconPlus className="h-4 w-4" />
              New knowledge base
            </Link>
          )}
        </div>

        <div className="relative mb-8 max-w-[420px]">
          <IconSearch className="pointer-events-none absolute left-4 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-soft" />
          <input
            className="input pl-10"
            placeholder="Search knowledge bases…"
            value={q}
            onChange={(e) => setQ(e.target.value)}
          />
        </div>

        {kbs === null && (
          <p className="caption mb-6 text-muted-soft">Loading knowledge bases…</p>
        )}
        {kbs !== null && kbs.length === 0 && !error && (
          <p className="caption mb-6 text-muted-soft">No knowledge bases yet.</p>
        )}

        <div className="grid grid-cols-1 gap-5 md:grid-cols-2 xl:grid-cols-3">
          {rows.map((kb) => (
            <Link
              key={kb.id}
              href={`/platform/knowledge-bases/${kb.id}`}
              className="card card-hover group relative min-w-0 overflow-hidden p-6"
            >
              <Orb color={kb.orb} size={220} className="-right-16 -top-16 opacity-50" />
              <div className="relative min-w-0">
                <div className="mb-4 flex h-10 w-10 items-center justify-center rounded-full bg-surface-strong text-ink">
                  <IconBook className="h-5 w-5" />
                </div>
                <h2 className="title-md truncate">{kb.name}</h2>
                <p className="body-sm mt-1.5 line-clamp-2 text-body">{kb.description}</p>
                <div className="caption mt-5 flex items-center gap-4 text-muted">
                  <span className="flex items-center gap-1.5">
                    <IconDoc className="h-3.5 w-3.5" />
                    {kb.docs} documents
                  </span>
                  <span className="ml-auto whitespace-nowrap">{fmtShortDate(kb.updatedAt)}</span>
                </div>
              </div>
            </Link>
          ))}

          {isOwner && (
            <Link
              href="/platform/knowledge-bases/new"
              className="flex min-h-[190px] items-center justify-center rounded-[16px] border border-dashed border-hairline-strong text-muted transition-colors hover:border-ink hover:text-ink"
            >
              <span className="flex items-center gap-2 text-[15px] font-medium">
                <IconPlus className="h-4 w-4" /> Create knowledge base
              </span>
            </Link>
          )}
        </div>
      </div>
    </div>
  );
}

/* Compact card timestamp: locale date for recent items, ISO date otherwise.
 * The raw RFC3339 string from the API is too long for the card footer. */
function fmtShortDate(v?: string): string {
  if (!v) return "";
  const d = new Date(v);
  if (Number.isNaN(d.getTime())) return v;
  return d.toLocaleDateString(undefined, { year: "2-digit", month: "short", day: "numeric" });
}
