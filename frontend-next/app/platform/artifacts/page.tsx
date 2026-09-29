/* Ported from frontend/src/views/artifacts/ArtifactLibrary.vue:
 * category tabs, search (debounced), date-grouped infinite list,
 * per-row download, session link, and preview via DocPreviewModal.
 */
"use client";

import { Suspense, useCallback, useEffect, useMemo, useRef, useState } from "react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { IconArtifact, IconDownload, IconExternal, IconSearch } from "@/components/icons";
import { renderFileIconSvg } from "@/components/files/file-icon";
import {
  listArtifactLibrary,
  downloadArtifact,
  type ArtifactLibraryItem,
} from "@/lib/api/chat";
import { useT } from "@/lib/i18n";
import {
  ARTIFACT_CATEGORIES,
  artifactCategoryExtensions,
  formatArtifactDateTime,
  formatArtifactSize,
  groupArtifactsByDate,
  parseArtifactCategory,
  type ArtifactCategory,
} from "@/lib/artifact-library";
import { DocPreviewModal, type DocPreviewSource } from "@/components/doc-preview-modal";

const PAGE_SIZE = 30;
const SEARCH_DEBOUNCE_MS = 300;

function itemKey(a: ArtifactLibraryItem): string {
  return `${a.message_id}:${a.index}`;
}

const IMAGE_EXT_RE = /\.(png|jpe?g|gif|webp|bmp|avif)$/i;
const isImageArtifact = (a: ArtifactLibraryItem) =>
  IMAGE_EXT_RE.test(a.file_name) || (a.file_type || "").toLowerCase().startsWith("image/");

/* Thumbnail cache shared across re-renders: one low-res blob per artifact. */
const thumbCache = new Map<string, string>();

function ArtifactThumb({ item }: { item: ArtifactLibraryItem }) {
  const key = `${item.session_id}\u0000${item.message_id}\u0000${item.index}`;
  const [src, setSrc] = useState<string | null>(thumbCache.get(key) ?? null);
  const holderRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (src) return;
    let cancelled = false;
    let io: IntersectionObserver | null = null;
    const load = async () => {
      try {
        const blob = await downloadArtifact(item.session_id, item.message_id, item.index, { width: 512 });
        const url = URL.createObjectURL(blob);
        thumbCache.set(key, url);
        if (!cancelled) setSrc(url);
      } catch {
        /* keep the icon fallback */
      }
    };
    if (typeof IntersectionObserver !== "undefined" && holderRef.current) {
      io = new IntersectionObserver(
        (entries) => {
          if (entries.some((e) => e.isIntersecting)) {
            io?.disconnect();
            void load();
          }
        },
        { rootMargin: "300px 0px" },
      );
      io.observe(holderRef.current);
      return () => {
        cancelled = true;
        io?.disconnect();
      };
    }
    void load();
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);

  if (src) {
    return <img src={src} alt={item.file_name} className="h-full w-full object-cover" loading="lazy" />;
  }
  return (
    <div ref={holderRef} className="flex h-full w-full items-center justify-center">
      <span
        className="h-12 w-11 opacity-70"
        dangerouslySetInnerHTML={{ __html: renderFileIconSvg(item.file_name) }}
      />
    </div>
  );
}

function ArtifactsInner() {
  const { t } = useT();
  const router = useRouter();
  const searchParams = useSearchParams();
  const [category, setCategory] = useState<ArtifactCategory>("all");
  const [keyword, setKeyword] = useState("");
  const [hydrated, setHydrated] = useState(false);
  const [items, setItems] = useState<ArtifactLibraryItem[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(0);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState(false);
  const [downloading, setDownloading] = useState<Record<string, boolean>>({});
  const [previewItem, setPreviewItem] = useState<ArtifactLibraryItem | null>(null);
  const scrollRootRef = useRef<HTMLDivElement | null>(null);
  const sentinelRef = useRef<HTMLDivElement | null>(null);
  const ticketRef = useRef(0);
  const stateRef = useRef({ keyword: "", category: "all" as ArtifactCategory, loading: false, total: 0, page: 0, loadError: false });
  stateRef.current = { keyword, category, loading, total, page, loadError };

  const sections = useMemo(() => groupArtifactsByDate(items), [items]);
  const hasMore = items.length < total;
  const isFiltered = category !== "all" || keyword.trim() !== "";

  const fetchPage = useCallback(async (nextPage: number, kw: string, cat: ArtifactCategory) => {
    const mine = ++ticketRef.current;
    setLoading(true);
    setLoadError(false);
    try {
      const res = await listArtifactLibrary({
        keyword: kw.trim() || undefined,
        fileTypes: artifactCategoryExtensions(cat),
        page: nextPage,
        pageSize: PAGE_SIZE,
      });
      if (mine !== ticketRef.current) return;
      const rows = Array.isArray(res.data) ? res.data : [];
      setItems((prev) => (nextPage === 1 ? rows : [...prev, ...rows]));
      setTotal(typeof res.total === "number" ? res.total : rows.length);
      setPage(nextPage);
    } catch {
      if (mine !== ticketRef.current) return;
      setLoadError(true);
    } finally {
      if (mine === ticketRef.current) setLoading(false);
    }
  }, []);

  const reload = useCallback((kw: string, cat: ArtifactCategory) => {
    setItems([]);
    setTotal(0);
    setPage(0);
    void fetchPage(1, kw, cat);
  }, [fetchPage]);

  // Hydrate from URL once.
  useEffect(() => {
    setCategory(parseArtifactCategory(searchParams.get("type")));
    const q = searchParams.get("q") ?? "";
    setKeyword(q);
    setHydrated(true);
    void fetchPage(1, q, parseArtifactCategory(searchParams.get("type")));
    return () => { ticketRef.current++; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Debounced search (skip first hydration render).
  const firstSearch = useRef(true);
  useEffect(() => {
    if (!hydrated) return;
    if (firstSearch.current) { firstSearch.current = false; return; }
    const timer = setTimeout(() => {
      const params = new URLSearchParams(searchParams.toString());
      const q = keyword.trim();
      if (q) params.set("q", q);
      else params.delete("q");
      const qs = params.toString();
      router.replace(qs ? `?${qs}` : "?", { scroll: false });
      reload(keyword, category);
    }, SEARCH_DEBOUNCE_MS);
    return () => clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [keyword, hydrated]);

  const pickCategory = (next: ArtifactCategory) => {
    if (next === category) return;
    setCategory(next);
    const params = new URLSearchParams(searchParams.toString());
    if (next === "all") params.delete("type");
    else params.set("type", next);
    const qs = params.toString();
    router.replace(qs ? `?${qs}` : "?", { scroll: false });
    reload(keyword, next);
  };

  const clearFilters = () => {
    setCategory("all");
    setKeyword("");
    firstSearch.current = false;
    router.replace("?", { scroll: false } as never);
    reload("", "all");
  };

  // Infinite scroll.
  useEffect(() => {
    const el = sentinelRef.current;
    const root = scrollRootRef.current;
    if (!el || typeof IntersectionObserver === "undefined") return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (!entries.some((e) => e.isIntersecting)) return;
        const s = stateRef.current;
        if (s.loading || s.loadError || items.length >= s.total || s.page < 1) return;
        void fetchPage(s.page + 1, s.keyword, s.category);
      },
      { root, rootMargin: "200px" },
    );
    observer.observe(el);
    return () => observer.disconnect();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fetchPage, items.length]);

  const handleDownload = async (a: ArtifactLibraryItem) => {
    const key = itemKey(a);
    if (downloading[key]) return;
    setDownloading((d) => ({ ...d, [key]: true }));
    try {
      const blob = await downloadArtifact(a.session_id, a.message_id, a.index);
      const url = URL.createObjectURL(blob);
      const el = document.createElement("a");
      el.href = url;
      el.download = a.file_name || "artifact";
      document.body.appendChild(el);
      el.click();
      document.body.removeChild(el);
      setTimeout(() => URL.revokeObjectURL(url), 1000);
    } catch {
      alert(t("artifactLibrary.downloadFailed"));
    } finally {
      setDownloading((d) => ({ ...d, [key]: false }));
    }
  };

  const previewSource: DocPreviewSource | null = previewItem
    ? {
        title: previewItem.file_name,
        fileName: previewItem.file_name,
        fileType: previewItem.file_type,
        sizeBytes: previewItem.file_size,
        fetchBlob: () => downloadArtifact(previewItem.session_id, previewItem.message_id, previewItem.index),
      }
    : null;

  return (
    <div ref={scrollRootRef} className="flex-1 overflow-y-auto">
      <div className="mx-auto w-full max-w-[1200px] px-4 py-6 sm:px-8 sm:py-10 lg:px-12">
        <div className="mb-6">
          <div className="caption-uppercase mb-3 text-muted">Workspace</div>
          <h1 className="display-xl">{t("nav.artifacts")}</h1>
          <p className="mt-3 max-w-[520px] text-body">{t("artifactLibrary.subtitle")}</p>
        </div>

        <div className="mb-6 flex flex-wrap items-center gap-3">
          <div className="flex flex-wrap gap-1.5" role="tablist" aria-label={t("artifactLibrary.typeFilter")}>
            {ARTIFACT_CATEGORIES.map((c) => (
              <button
                key={c}
                role="tab"
                aria-selected={category === c}
                onClick={() => pickCategory(c)}
                className={`btn btn-sm ${category === c ? "" : "btn-outline"}`}
              >
                {t(`artifactLibrary.cat.${c}`)}
              </button>
            ))}
          </div>
          <div className="relative ml-auto w-[280px]">
            <IconSearch className="pointer-events-none absolute left-4 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-soft" />
            <input
              className="input h-10 pl-10 text-[14px]"
              placeholder={t("artifactLibrary.searchPlaceholder")}
              aria-label={t("artifactLibrary.searchPlaceholder")}
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
            />
          </div>
        </div>

        {loading && items.length === 0 ? (
          <p className="caption text-muted">Loading…</p>
        ) : loadError && items.length === 0 ? (
          <div className="card flex flex-col items-center px-4 py-16 text-center">
            <h2 className="title-md">{t("artifactLibrary.loadFailed")}</h2>
            <button className="btn btn-outline btn-sm mt-4" onClick={() => reload(keyword, category)}>
              {t("artifactLibrary.retry")}
            </button>
          </div>
        ) : items.length === 0 && isFiltered ? (
          <div className="card flex flex-col items-center px-4 py-16 text-center">
            <h2 className="title-md">{t("artifactLibrary.noMatchesTitle")}</h2>
            <p className="body-sm mt-2 text-muted">{t("artifactLibrary.noMatchesDesc")}</p>
            <button className="btn btn-outline btn-sm mt-4" onClick={clearFilters}>
              {t("artifactLibrary.clearFilters")}
            </button>
          </div>
        ) : items.length === 0 ? (
          <div className="card flex flex-col items-center justify-center px-4 py-16 text-center sm:px-8 sm:py-20">
            <div className="mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-surface-strong text-ink">
              <IconArtifact className="h-6 w-6" />
            </div>
            <h2 className="title-md">{t("artifactLibrary.emptyTitle")}</h2>
            <p className="body-sm mt-2 max-w-[380px] text-muted">{t("artifactLibrary.emptyDesc")}</p>
          </div>
        ) : (
          <>
            <p className="caption mb-4 text-muted">{t("artifactLibrary.total", { count: total })}</p>
            {sections.map((section) => (
              <section key={section.group} className="mb-6">
                <h3 className="caption-uppercase mb-2 text-muted">{t(`artifactLibrary.group.${section.group}`)}</h3>
                <div className="grid grid-cols-2 gap-5 md:grid-cols-3 xl:grid-cols-4">
                  {section.items.map((a) => {
                    const image = isImageArtifact(a);
                    return (
                      <div key={itemKey(a)} className="card card-hover group relative min-w-0 overflow-hidden">
                        <div
                          onClick={() => setPreviewItem(a)}
                          className="relative cursor-pointer aspect-square"
                          title={a.file_name}
                        >
                          {image ? (
                            <ArtifactThumb item={a} />
                          ) : (
                            <div className="flex h-full w-full items-center justify-center bg-surface-strong/40">
                              <span className="h-16 w-14 opacity-80" dangerouslySetInnerHTML={{ __html: renderFileIconSvg(a.file_name) }} />
                            </div>
                          )}
                          {a.version_count > 1 && (
                            <span className="caption absolute left-3 top-3 rounded-full bg-ink/60 px-2 py-0.5 text-[11px] font-semibold text-white">
                              ×{a.version_count}
                            </span>
                          )}
                        </div>
                        <div className="p-4">
                          <div className="truncate text-[14px] font-medium text-ink" title={a.file_name}>
                            {a.file_name}
                          </div>
                          <div className="caption mt-0.5 truncate text-muted">
                            {formatArtifactSize(a.file_size)} · {formatArtifactDateTime(a.created_at)}
                          </div>
                          <Link
                            href={`/platform/chat/${a.session_id}`}
                            title={t("artifactLibrary.openSession")}
                            onClick={(e) => e.stopPropagation()}
                            className="caption mt-1 block max-w-full truncate text-muted hover:text-ink"
                          >
                            {a.session_title || t("artifactLibrary.untitledSession")}
                          </Link>
                          <div className="mt-3 flex items-center gap-2 overflow-hidden">
                            <button
                              className="btn btn-outline btn-sm min-w-0 flex-1 truncate px-2.5"
                              title={t("artifactLibrary.preview")}
                              onClick={(e) => { e.stopPropagation(); setPreviewItem(a); }}
                            >
                              <IconExternal className="h-3.5 w-3.5 shrink-0" /> <span className="truncate">{t("artifactLibrary.preview")}</span>
                            </button>
                            <button
                              className="btn btn-outline btn-sm min-w-0 flex-1 truncate px-2.5"
                              title={t("artifactLibrary.download")}
                              disabled={!!downloading[itemKey(a)]}
                              onClick={(e) => { e.stopPropagation(); void handleDownload(a); }}
                            >
                              <IconDownload className="h-3.5 w-3.5 shrink-0" /> <span className="truncate">{t("artifactLibrary.download")}</span>
                            </button>
                          </div>
                        </div>
                      </div>
                    );
                  })}
                </div>
              </section>
            ))}
            <div ref={sentinelRef} className="flex items-center justify-center py-4">
              {loading ? (
                <span className="caption text-muted">Loading…</span>
              ) : (
                hasMore && (
                  <button className="btn btn-outline btn-sm" onClick={() => fetchPage(page + 1, keyword, category)}>
                    {t("artifactLibrary.loadMore")}
                  </button>
                )
              )}
            </div>
          </>
        )}
      </div>

      <DocPreviewModal source={previewSource} onClose={() => setPreviewItem(null)} />
    </div>
  );
}

export default function Artifacts() {
  return (
    <Suspense>
      <ArtifactsInner />
    </Suspense>
  );
}
