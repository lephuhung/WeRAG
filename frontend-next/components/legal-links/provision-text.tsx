"use client";

/* The wording of one provision on the link review page: fetched once per
 * document + provision and kept for the page's life, so hovering the same
 * node again (or opening its link) shows it at once. When no text can be
 * shown the box says why in plain words, and still shows the source's own
 * note with a link to its page. */

import { useEffect, useState } from "react";
import { IconExternal } from "@/components/icons";
import { useT } from "@/lib/i18n";
import { userErrorMessage } from "@/lib/user-error";
import { provisionLabel } from "@/lib/legal-provision-label";
import { getLegalProvisionText, type LegalProvisionText } from "@/lib/api/legal-link-candidates";

const cache = new Map<string, Promise<LegalProvisionText>>();

function fetchText(docId: string, path: string): Promise<LegalProvisionText> {
  const key = `${docId}|${path}`;
  let p = cache.get(key);
  if (!p) {
    p = getLegalProvisionText(docId, path).then((r) => r.data);
    p.catch(() => cache.delete(key)); // a failure is asked again next time
    cache.set(key, p);
  }
  return p;
}

/** Forget every fetched text (after a correction changed what is held). */
export function clearProvisionTextCache() {
  cache.clear();
}

export function useProvisionText(docId: string, path: string, enabled = true) {
  const [state, setState] = useState<{ key: string; data?: LegalProvisionText; error?: unknown }>({ key: "" });
  const key = `${docId}|${path}`;
  useEffect(() => {
    if (!enabled || !docId) return;
    let alive = true;
    fetchText(docId, path).then(
      (data) => alive && setState({ key, data }),
      (error) => alive && setState({ key, error }),
    );
    return () => {
      alive = false;
    };
  }, [docId, path, key, enabled]);
  const current = state.key === key ? state : { key };
  return { data: current.data, error: current.error, loading: enabled && !!docId && !current.data && !current.error };
}

/** "Nghị định 63/2023/NĐ-CP" from a catalogue row, else the bare number. */
export function docName(doc: { doc_type?: string; document_number?: string } | null | undefined, fallback = ""): string {
  const n = doc?.document_number?.trim() || fallback;
  const type = doc?.doc_type?.trim();
  return type && n && !n.toLowerCase().startsWith(type.toLowerCase()) ? `${type} ${n}` : n;
}

export function ProvisionTextBox({
  docId,
  path,
  docLabel,
  missingNumber,
  note,
  feedUrl,
  maxHeight = "max-h-64",
}: {
  /** "" when the document is not in the catalogue (missingNumber says which). */
  docId: string;
  path: string;
  docLabel?: string;
  missingNumber?: string;
  note?: string;
  feedUrl?: string;
  maxHeight?: string;
}) {
  const { t, locale } = useT();
  const { data, error, loading } = useProvisionText(docId, path, !!docId);
  const name = docLabel || docName(data?.document, missingNumber);
  const prov = path ? provisionLabel(path, locale) : t("llink.noPath");

  let body: React.ReactNode;
  if (!docId) {
    body = <p className="caption text-muted">{t("llink.text.notInCatalogue", { doc: missingNumber || "—" })}</p>;
  } else if (loading) {
    body = <p className="caption text-muted">{t("common.loading")}</p>;
  } else if (error) {
    body = <p className="caption text-error">{userErrorMessage(error, t, "llink.text.loadFailed")}</p>;
  } else if (data && data.found) {
    body = (
      <>
        {data.whole_article && <p className="caption mb-1 text-muted">{t("llink.text.wholeArticle")}</p>}
        <div className={`${maxHeight} overflow-y-auto whitespace-pre-wrap text-[13px] leading-relaxed text-ink`}>{data.text}</div>
        {data.truncated && <p className="caption mt-1 text-muted">{t("llink.text.truncated")}</p>}
      </>
    );
  } else {
    const reason = data?.reason ?? "not_found";
    body = <p className="caption text-warning">{t(`llink.text.reason.${reason}`, { doc: name || "—", prov })}</p>;
  }

  return (
    <div className="flex min-w-0 flex-col gap-2">
      {body}
      {note && (
        <div className="rounded-[10px] border border-hairline bg-canvas-soft px-2.5 py-2">
          <div className="caption font-medium text-muted">{t("llink.col.note")}</div>
          <div className="caption mt-0.5 whitespace-pre-wrap text-body">{note}</div>
        </div>
      )}
      {feedUrl && (
        <a href={feedUrl} target="_blank" rel="noreferrer" className="caption inline-flex items-center gap-1 text-link hover:underline">
          {t("llink.text.sourcePage")}
          <IconExternal className="h-3 w-3" />
        </a>
      )}
    </div>
  );
}
