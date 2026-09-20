/* Ported from frontend/src/views/artifacts/ArtifactLibrary.vue (core list):
 * search, pagination and per-row download through the session artifact
 * endpoint. The Vue app's preview/viewer stays out of scope here.
 */
"use client";

import { useCallback, useEffect, useState } from "react";
import { IconArtifact, IconSearch } from "@/components/icons";
import {
  listArtifactLibrary,
  downloadArtifact,
  type ArtifactLibraryItem,
} from "@/lib/api/chat";
import { useT } from "@/lib/i18n";

function fmtBytes(bytes: number): string {
  if (!bytes) return "0 B";
  const k = 1024;
  const sizes = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(k)), sizes.length - 1);
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(2))} ${sizes[i]}`;
}

const PAGE_SIZE = 24;

export default function Artifacts() {
  const { t } = useT();
  const [items, setItems] = useState<ArtifactLibraryItem[] | null>(null);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [keyword, setKeyword] = useState("");
  const [error, setError] = useState("");

  const load = useCallback(async (nextPage: number) => {
    setError("");
    try {
      const res = await listArtifactLibrary({
        keyword: keyword || undefined,
        page: nextPage,
        pageSize: PAGE_SIZE,
      });
      setItems(res.data);
      setTotal(res.total);
    } catch (e) {
      if (e instanceof Error) setError(e.message);
      setItems([]);
    }
  }, [keyword]);

  useEffect(() => {
    void load(page);
  }, [load, page]);

  const save = async (a: ArtifactLibraryItem) => {
    try {
      const blob = await downloadArtifact(a.session_id, a.message_id, a.index);
      const url = URL.createObjectURL(blob);
      const el = document.createElement("a");
      el.href = url;
      el.download = a.file_name;
      el.click();
      URL.revokeObjectURL(url);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Download failed");
    }
  };

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  return (
    <div className="flex-1 overflow-y-auto">
      <div className="mx-auto w-full max-w-[1200px] px-12 py-12">
        <div className="mb-10 flex items-end justify-between gap-6">
          <div>
            <div className="caption-uppercase mb-3 text-muted">Workspace</div>
            <h1 className="display-xl">{t("nav.artifacts")}</h1>
            <p className="mt-3 max-w-[520px] text-body">
              Generated documents, tables and files produced by your chats.
            </p>
          </div>
          <div className="relative w-[280px]">
            <IconSearch className="pointer-events-none absolute left-4 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-soft" />
            <input
              className="input h-10 pl-10 text-[14px]"
              placeholder={t("agent.artifactSearch")}
              value={keyword}
              onChange={(e) => {
                setPage(1);
                setKeyword(e.target.value);
              }}
            />
          </div>
        </div>

        {error && <p className="caption mb-6 text-error">{error}</p>}

        {items === null ? (
          <p className="caption text-muted">Loading…</p>
        ) : items.length === 0 ? (
          <div className="card flex flex-col items-center justify-center px-8 py-20 text-center">
            <div className="mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-surface-strong text-ink">
              <IconArtifact className="h-6 w-6" />
            </div>
            <h2 className="title-md">No artifacts yet</h2>
            <p className="body-sm mt-2 max-w-[380px] text-muted">
              Artifacts created during chat sessions will appear here, searchable and exportable.
            </p>
          </div>
        ) : (
          <>
            <div className="card overflow-hidden">
              {items.map((a, i) => (
                <div
                  key={`${a.session_id}:${a.message_id}:${a.index}`}
                  className={`flex items-center gap-4 px-5 py-4 ${i > 0 ? "border-t border-hairline" : ""}`}
                >
                  <div className="flex h-9 w-9 items-center justify-center rounded-full bg-surface-strong text-ink">
                    <IconArtifact className="h-4 w-4" />
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-[15px] font-medium text-ink">{a.file_name}</div>
                    <div className="caption truncate text-muted">
                      {a.session_title ?? ""}
                      {a.version_count > 1 ? ` · v${a.version_count}` : ""}
                    </div>
                  </div>
                  <span className="caption shrink-0 text-muted">{fmtBytes(a.file_size)}</span>
                  <button
                    className="btn btn-outline btn-sm shrink-0"
                    onClick={() => void save(a)}
                  >
                    Download
                  </button>
                </div>
              ))}
            </div>
            {totalPages > 1 && (
              <div className="caption mt-4 flex items-center justify-center gap-4 text-muted">
                <button
                  className="btn btn-outline btn-sm"
                  disabled={page <= 1}
                  onClick={() => setPage((p) => Math.max(1, p - 1))}
                >
                  ‹
                </button>
                <span>
                  {page} / {totalPages} · {total} files
                </span>
                <button
                  className="btn btn-outline btn-sm"
                  disabled={page >= totalPages}
                  onClick={() => setPage((p) => p + 1)}
                >
                  ›
                </button>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}
