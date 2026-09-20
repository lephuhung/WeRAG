"use client";

import { useEffect, useState } from "react";
import { IconArtifact } from "@/components/icons";
import { apiGet } from "@/lib/api-client";

type ArtifactItem = {
  message_id: string;
  index: number;
  file_name: string;
  file_size?: number;
  session_title?: string;
};

export default function Artifacts() {
  const [items, setItems] = useState<ArtifactItem[] | null>(null);

  useEffect(() => {
    let alive = true;
    apiGet<{ success: boolean; data?: ArtifactItem[] }>(`/api/v1/artifacts`)
      .then((res) => {
        if (alive) setItems(res.data ?? []);
      })
      .catch(() => {
        if (alive) setItems([]);
      });
    return () => {
      alive = false;
    };
  }, []);

  return (
    <div className="flex-1 overflow-y-auto">
      <div className="mx-auto w-full max-w-[1200px] px-12 py-12">
        <div className="mb-10 flex items-end justify-between gap-6">
          <div>
            <div className="caption-uppercase mb-3 text-muted">Workspace</div>
            <h1 className="display-xl">Artifacts</h1>
            <p className="mt-3 max-w-[520px] text-body">
              Generated documents, tables and files produced by your chats.
            </p>
          </div>
        </div>

        {items === null || items.length === 0 ? (
          <div className="card flex flex-col items-center justify-center px-8 py-20 text-center">
            <div className="mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-surface-strong text-ink">
              <IconArtifact className="h-6 w-6" />
            </div>
            <h2 className="title-md">{items === null ? "Loading…" : "No artifacts yet"}</h2>
            <p className="body-sm mt-2 max-w-[380px] text-muted">
              {items === null
                ? "Fetching GET /api/v1/artifacts…"
                : "Artifacts created during chat sessions will appear here, searchable and exportable."}
            </p>
          </div>
        ) : (
          <div className="card overflow-hidden">
            {items.map((a, i) => (
              <div
                key={`${a.message_id}:${a.index}`}
                className={`flex items-center gap-4 px-5 py-4 ${i > 0 ? "border-t border-hairline" : ""}`}
              >
                <div className="flex h-9 w-9 items-center justify-center rounded-full bg-surface-strong text-ink">
                  <IconArtifact className="h-4 w-4" />
                </div>
                <div className="min-w-0 flex-1">
                  <div className="truncate text-[15px] font-medium text-ink">{a.file_name}</div>
                  <div className="caption truncate text-muted">{a.session_title ?? ""}</div>
                </div>
                {typeof a.file_size === "number" && (
                  <span className="caption text-muted">{a.file_size} B</span>
                )}
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
