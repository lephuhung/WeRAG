"use client";

import { useEffect, useMemo, useState } from "react";
import { IconBulb } from "@/components/icons";
import {
  createAbbreviation,
  listAbbreviations,
  type Abbreviation,
} from "@/lib/api/abbreviations";

function dedupeCandidates(candidates: string[]): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const c of candidates) {
    const trimmed = c.trim();
    const key = trimmed.toLowerCase();
    if (!trimmed || seen.has(key)) continue;
    seen.add(key);
    out.push(trimmed);
  }
  return out;
}

export function AbbreviationSuggestionCard({
  candidates,
  refreshKey,
}: {
  candidates: string[];
  refreshKey: number;
}) {
  const shorts = useMemo(
    () => dedupeCandidates(candidates).slice(0, 10),
    [candidates],
  );
  const [checking, setChecking] = useState(true);
  const [pending, setPending] = useState<Record<string, Abbreviation[]>>({});
  const [active, setActive] = useState<Record<string, boolean>>({});
  const [openForm, setOpenForm] = useState<string | null>(null);
  const [fullForm, setFullForm] = useState("");
  const [description, setDescription] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [submitted, setSubmitted] = useState<Record<string, Abbreviation>>({});

  useEffect(() => {
    let alive = true;
    setChecking(true);
    setPending({});
    setActive({});
    setSubmitted({});
    setOpenForm(null);
    void Promise.all(
      shorts.map((short) =>
        listAbbreviations({ search: short, page: 1, pageSize: 100 })
          .then((res) => ({ short, rows: res.data ?? [] }))
          .catch(() => ({ short, rows: [] as Abbreviation[] })),
      ),
    ).then((results) => {
      if (!alive) return;
      const nextPending: Record<string, Abbreviation[]> = {};
      const nextActive: Record<string, boolean> = {};
      for (const { short, rows } of results) {
        const exact = rows.filter(
          (r) => r.short_form.trim().toLowerCase() === short.toLowerCase(),
        );
        if (exact.some((r) => r.is_active)) {
          nextActive[short] = true;
        } else if (exact.length > 0) {
          nextPending[short] = exact;
        }
      }
      setPending(nextPending);
      setActive(nextActive);
      setChecking(false);
    });
    return () => {
      alive = false;
    };
  }, [shorts, refreshKey]);

  const submit = async (short: string) => {
    const full = fullForm.trim();
    if (!full || submitting) return;
    setSubmitting(true);
    setError(null);
    try {
      const res = await createAbbreviation({
        short_form: short,
        full_form: full,
        description: description.trim() || undefined,
      });
      if (!res.success || !res.data) {
        setError("Gửi đề xuất thất bại");
        return;
      }
      const row = res.data;
      if (row.is_active) {
        setActive((prev) => ({ ...prev, [short]: true }));
      } else {
        setSubmitted((prev) => ({ ...prev, [short]: row }));
      }
      setOpenForm(null);
      setFullForm("");
      setDescription("");
    } catch (e) {
      setError(e instanceof Error ? e.message : "Gửi đề xuất thất bại");
    } finally {
      setSubmitting(false);
    }
  };

  if (checking) return null;
  const visible = shorts.filter((s) => !active[s]);
  if (visible.length === 0) return null;

  return (
    <div className="mt-3 space-y-3">
      {visible.map((short) => {
        const rows = pending[short] ?? [];
        const done = submitted[short];
        return (
          <div
            key={short}
            className="rounded-lg border border-amber-200 border-l-4 border-l-amber-400 bg-amber-50/70 p-4 dark:border-amber-500/30 dark:border-l-amber-500/60 dark:bg-amber-500/10"
          >
            {done ? (
              <div className="text-[13px] text-body">
                <div className="font-medium text-ink">
                  {done.short_form} → {done.full_form}
                </div>
                <div className="mt-1">
                  <span className="inline-flex items-center rounded-full bg-amber-500/10 px-2 py-0.5 text-[12px] text-amber-600 dark:text-amber-400">
                    Chờ Owner/SuperAdmin duyệt
                  </span>
                  <p className="mt-2">
                    Đã gửi đề xuất. Nghĩa này chỉ được sử dụng sau khi Owner hoặc
                    SuperAdmin kích hoạt.
                  </p>
                </div>
              </div>
            ) : (
              <>
                {rows.length > 0 ? (
                  <div className="space-y-1 text-[13px] text-body">
                    <div className="mb-1.5 flex items-center gap-1.5 font-medium text-ink">
                      <IconBulb className="h-4 w-4 shrink-0 text-amber-500" />
                      Có nghĩa đang chờ duyệt cho “{short}”
                    </div>
                    {rows.map((r) => (
                      <div key={r.id} className="flex items-center gap-2">
                        <span className="text-ink">
                          {r.short_form} → {r.full_form}
                        </span>
                        <span className="inline-flex items-center rounded-full bg-amber-500/10 px-2 py-0.5 text-[12px] text-amber-600 dark:text-amber-400">
                          Chờ Owner/SuperAdmin duyệt
                        </span>
                      </div>
                    ))}
                  </div>
                ) : (
                  <p className="flex items-center gap-1.5 text-[13px] text-body">
                    <IconBulb className="h-4 w-4 shrink-0 text-amber-500" />
                    <span>
                      Hệ thống chưa có nghĩa được duyệt cho{" "}
                      <strong className="font-semibold text-ink">“{short}”</strong>.
                    </span>
                  </p>
                )}
                {openForm !== short ? (
                  <button
                    type="button"
                    onClick={() => {
                      setOpenForm(short);
                      setError(null);
                      setFullForm("");
                      setDescription("");
                    }}
                    className="mt-2 inline-flex items-center gap-1 rounded-full bg-amber-500 px-3.5 py-1.5 text-[13px] font-medium text-white shadow-sm transition-colors hover:bg-amber-600 dark:bg-amber-600 dark:hover:bg-amber-500"
                  >
                    {rows.length > 0 ? "Đề xuất nghĩa khác" : "Bổ sung nghĩa"}
                  </button>
                ) : (
                  <form
                    className="mt-3 space-y-2"
                    onSubmit={(e) => {
                      e.preventDefault();
                      void submit(short);
                    }}
                  >
                    <div>
                      <label className="mb-1 block text-[12px] text-subtle">
                        Từ viết tắt
                      </label>
                      <input
                        value={short}
                        readOnly
                        className="w-full rounded-md border border-hairline bg-surface px-3 py-1.5 text-[13px] text-subtle"
                      />
                    </div>
                    <div>
                      <label className="mb-1 block text-[12px] text-subtle">
                        Nghĩa đầy đủ <span className="text-red-500">*</span>
                      </label>
                      <input
                        value={fullForm}
                        onChange={(e) => setFullForm(e.target.value)}
                        maxLength={255}
                        required
                        placeholder="Ví dụ: Ủy ban nhân dân"
                        className="w-full rounded-md border border-hairline bg-surface px-3 py-1.5 text-[13px] text-ink"
                      />
                    </div>
                    <div>
                      <label className="mb-1 block text-[12px] text-subtle">
                        Mô tả (không bắt buộc)
                      </label>
                      <input
                        value={description}
                        onChange={(e) => setDescription(e.target.value)}
                        maxLength={2000}
                        className="w-full rounded-md border border-hairline bg-surface px-3 py-1.5 text-[13px] text-ink"
                      />
                    </div>
                    {error && <p className="text-[12px] text-red-500">{error}</p>}
                    <div className="flex gap-2">
                      <button
                        type="submit"
                        disabled={submitting || !fullForm.trim()}
                        className="rounded-full bg-ink px-3.5 py-1.5 text-[13px] text-surface transition-opacity disabled:opacity-50"
                      >
                        {submitting ? "Đang gửi..." : "Gửi đề xuất"}
                      </button>
                      <button
                        type="button"
                        disabled={submitting}
                        onClick={() => setOpenForm(null)}
                        className="rounded-full border border-hairline px-3.5 py-1.5 text-[13px] text-body"
                      >
                        Hủy
                      </button>
                    </div>
                  </form>
                )}
              </>
            )}
          </div>
        );
      })}
    </div>
  );
}
