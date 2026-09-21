"use client";

import { Suspense, useEffect, useMemo, useState } from "react";
import { useSearchParams } from "next/navigation";
import { ApiError } from "@/lib/api-client";
import {
  getParserEngines,
  getParserEngineConfig,
  updateParserEngineConfig,
  checkParserEngines,
  reconnectDocReader,
  getRuntimeQueues,
  getStorageEngineStatus,
  type ParserEngineConfig,
  type ParserEngineInfo,
  type QueueStat,
} from "@/lib/api/system";
import {
  IconCheck,
  IconDocReader,
  IconEdit,
  IconExternal,
  IconParserEngine,
  IconRefresh,
  IconStorageEngine,
} from "@/components/icons";
import { Modal } from "@/components/modal";
import { VectorStoreSettings } from "@/components/settings/vector-store-settings";
import { StorageSettings } from "@/components/settings/storage-settings";
import { WebSearchSettings } from "@/components/settings/web-search-settings";
import { SandboxSettings } from "@/components/settings/sandbox-settings";

const STATUS = {
  healthy: {
    label: "Healthy",
    dot: "bg-emerald-500",
    badge: "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border border-emerald-500/20",
  },
  degraded: {
    label: "Degraded",
    dot: "bg-amber-500",
    badge: "bg-amber-500/10 text-amber-600 dark:text-amber-400 border border-amber-500/20",
  },
  down: {
    label: "Down",
    dot: "bg-rose-500",
    badge: "bg-rose-500/10 text-rose-600 dark:text-rose-400 border border-rose-500/20",
  },
} as const;

type ServiceCategory = "core" | "parser" | "storage";

type ServiceRow = {
  id: string;
  engineKey: string;
  name: string;
  category: ServiceCategory;
  description?: string;
  status: keyof typeof STATUS;
  detail?: string;
  fileTypes?: string[];
  configurable: boolean;
  docLink?: string;
};

const ENGINE_DOC_LINKS: Record<string, string> = {
  weknoracloud: "https://developers.weixin.qq.com/doc/aispeech/knowledge/atomic_capability/atomic_interface.html",
  markitdown: "https://github.com/microsoft/markitdown",
  mineru: "https://github.com/opendatalab/MinerU",
  mineru_cloud: "https://mineru.net/apiManage/docs",
  paddleocr_vl: "https://github.com/PaddlePaddle/PaddleOCR",
  paddleocr_vl_cloud: "https://aistudio.baidu.com/paddleocr",
};

const CONFIGURABLE_ENGINES = new Set([
  "docreader",
  "mineru",
  "mineru_cloud",
  "paddleocr_vl",
  "paddleocr_vl_cloud",
]);

const DEFAULT_PARSER_CONFIG: ParserEngineConfig = {
  docreader_addr: "",
  docreader_transport: "grpc",
  mineru_endpoint: "",
  mineru_api_key: "",
  mineru_model: "pipeline",
  mineru_vlm_server_url: "",
  mineru_enable_formula: true,
  mineru_enable_table: true,
  mineru_parse_method: "auto",
  mineru_enable_ocr: true,
  mineru_language: "ch",
  mineru_cloud_model: "pipeline",
  mineru_cloud_enable_formula: true,
  mineru_cloud_enable_table: true,
  mineru_cloud_enable_ocr: true,
  mineru_cloud_language: "ch",
  paddleocr_vl_endpoint: "",
  paddleocr_vl_use_seal_recognition: true,
  paddleocr_vl_use_chart_recognition: false,
  paddleocr_vl_cloud_token: "",
  paddleocr_vl_cloud_model: "PaddleOCR-VL-1.6",
  paddleocr_vl_cloud_use_seal_recognition: true,
  paddleocr_vl_cloud_use_chart_recognition: false,
};

export default function SystemServicesPage() {
  return (
    <Suspense fallback={null}>
      <SystemServices />
    </Suspense>
  );
}

function SystemServices() {
  const searchParams = useSearchParams();
  const initialCategory = searchParams.get("category") ?? "all";

  const [services, setServices] = useState<ServiceRow[] | null>(null);
  const [queues, setQueues] = useState<QueueStat[] | null>(null);
  const [queuesDenied, setQueuesDenied] = useState(false);
  const [queuesUnavailable, setQueuesUnavailable] = useState(false);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);

  // Filter states
  const [categoryFilter, setCategoryFilter] = useState<string>(initialCategory);
  const [statusFilter, setStatusFilter] = useState<string>("all");

  useEffect(() => {
    const cat = searchParams.get("category");
    if (cat) {
      setCategoryFilter(cat);
    }
  }, [searchParams]);

  // Edit / Details modal state
  const [config, setConfig] = useState<ParserEngineConfig>({ ...DEFAULT_PARSER_CONFIG });
  const [selectedService, setSelectedService] = useState<ServiceRow | null>(null);
  const [modalOpen, setModalOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState("");
  const [checking, setChecking] = useState(false);
  const [checkResult, setCheckResult] = useState<{ ok: boolean; message: string } | null>(null);

  const load = async () => {
    setLoading(true);
    setError("");
    try {
      const [parsers, storage, parserCfg] = await Promise.all([
        getParserEngines(),
        getStorageEngineStatus(),
        getParserEngineConfig().catch(() => ({ data: DEFAULT_PARSER_CONFIG })),
      ]);

      if (parserCfg?.data) {
        setConfig((prev) => ({ ...prev, ...parserCfg.data }));
      }

      const rows: ServiceRow[] = [];

      // 1. DocReader (Core)
      rows.push({
        id: "docreader",
        engineKey: "docreader",
        name: "DocReader",
        category: "core",
        description: `Document parsing service (${parsers.docreader_transport ?? "grpc"})`,
        status: parsers.connected === false ? "down" : "healthy",
        detail: parsers.docreader_addr,
        configurable: true,
      });

      // 2. Parsers
      for (const p of parsers.data ?? []) {
        const engineKey = p.Name;
        rows.push({
          id: `parser-${p.Name}`,
          engineKey,
          name: `Parser: ${p.Name}`,
          category: "parser",
          description: p.Description,
          status: p.Available === false ? "degraded" : "healthy",
          detail: p.Available === false ? p.UnavailableReason : undefined,
          fileTypes: p.FileTypes,
          configurable: CONFIGURABLE_ENGINES.has(engineKey),
          docLink: ENGINE_DOC_LINKS[engineKey],
        });
      }

      // 3. Storage
      for (const e of storage.data?.engines ?? []) {
        rows.push({
          id: `storage-${e.name}`,
          engineKey: e.name,
          name: `Storage: ${e.name}`,
          category: "storage",
          description: e.description,
          status: !e.available ? "down" : e.allowed === false ? "degraded" : "healthy",
          detail: e.allowed === false ? "Not allowed in this deployment" : undefined,
          configurable: false,
        });
      }

      setServices(rows);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load services");
      setServices([]);
    } finally {
      setLoading(false);
    }

    try {
      const res = await getRuntimeQueues();
      setQueues(res.queues ?? []);
      setQueuesUnavailable(!res.available);
      setQueuesDenied(false);
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) {
        setQueuesDenied(true);
        setQueues([]);
      }
    }
  };

  useEffect(() => {
    void load();
  }, []);

  const openConfig = (service: ServiceRow) => {
    setSelectedService(service);
    setSaveError("");
    setCheckResult(null);
    setModalOpen(true);
  };

  const handleSaveConfig = async () => {
    setSaving(true);
    setSaveError("");
    try {
      await updateParserEngineConfig(config);
      await load();
      setModalOpen(false);
    } catch (e) {
      setSaveError(e instanceof Error ? e.message : "Failed to save configuration");
    } finally {
      setSaving(false);
    }
  };

  const handleTestConnection = async () => {
    if (!selectedService) return;
    setChecking(true);
    setCheckResult(null);
    try {
      if (selectedService.engineKey === "docreader") {
        const addr = config.docreader_addr || "";
        const res = await reconnectDocReader(addr);
        if (res.connected) {
          setCheckResult({ ok: true, message: "DocReader connected successfully!" });
        } else {
          setCheckResult({
            ok: false,
            message: res.msg || "Could not connect to DocReader service.",
          });
        }
      } else {
        const res = await checkParserEngines(config);
        const engine = res.data?.find((p: ParserEngineInfo) => p.Name === selectedService.engineKey);
        if (engine?.Available) {
          setCheckResult({ ok: true, message: "Engine probe successful! Service is available." });
        } else {
          setCheckResult({
            ok: false,
            message: engine?.UnavailableReason || "Engine responded as unavailable.",
          });
        }
      }
    } catch (e) {
      setCheckResult({
        ok: false,
        message: e instanceof Error ? e.message : "Connection test failed",
      });
    } finally {
      setChecking(false);
    }
  };

  const rows = services ?? [];
  const healthyCount = rows.filter((s) => s.status === "healthy").length;

  const categoryCounts = useMemo(() => {
    return {
      all: rows.length,
      core: rows.filter((s) => s.category === "core").length,
      parser: rows.filter((s) => s.category === "parser").length,
      storage: rows.filter((s) => s.category === "storage").length,
    };
  }, [rows]);

  const filteredServices = useMemo(() => {
    return rows.filter((s) => {
      const matchCat =
        categoryFilter === "all" ||
        categoryFilter === "parsers" ||
        s.category === categoryFilter;
      const matchStatus = statusFilter === "all" || s.status === statusFilter;
      return matchCat && matchStatus;
    });
  }, [rows, categoryFilter, statusFilter]);

  return (
    <div className="mx-auto w-full max-w-[1200px]">
      {/* Header bar */}
      <div className="mb-6 flex flex-wrap items-center justify-between gap-4">
        <div>
          <h2 className="title-md font-semibold text-ink">Services & Engines</h2>
          <p className="caption text-muted">
            <span className="font-medium text-ink">
              {healthyCount}/{rows.length}
            </span>{" "}
            services healthy · {loading ? "checking…" : "checked just now"}
          </p>
        </div>

        <button className="btn btn-outline btn-sm" onClick={() => void load()}>
          <IconRefresh className="h-3.5 w-3.5" /> Refresh
        </button>
      </div>

      {error && <p className="caption mb-4 text-error">{error}</p>}

      {/* Filter Tabs */}
      {rows.length > 0 && (
        <div className="mb-6 flex flex-wrap items-center justify-between gap-3 border-b border-hairline pb-3">
          {/* Category Tabs */}
          <div className="flex flex-wrap items-center gap-1.5">
            {[
              { id: "all", label: "All Engines" },
              { id: "parsers", label: "Parsers & Core" },
              { id: "vector", label: "Vector Stores" },
              { id: "storage", label: "Storage Backends" },
              { id: "search", label: "Web Search" },
              { id: "sandbox", label: "Sandbox" },
            ].map((tab) => {
              const isSelected = categoryFilter === tab.id;
              return (
                <button
                  key={tab.id}
                  onClick={() => setCategoryFilter(tab.id)}
                  className={`flex items-center gap-1.5 rounded-full px-3.5 py-1 text-[12.5px] font-medium transition-colors ${
                    isSelected
                      ? "bg-ink text-white dark:bg-white dark:text-ink"
                      : "text-muted hover:bg-surface-strong hover:text-ink"
                  }`}
                >
                  <span>{tab.label}</span>
                </button>
              );
            })}
          </div>

          {/* Status Filter */}
          <div className="flex items-center gap-1 text-xs text-muted">
            <span className="mr-1">Status:</span>
            {["all", "healthy", "degraded", "down"].map((st) => (
              <button
                key={st}
                onClick={() => setStatusFilter(st)}
                className={`rounded px-2 py-0.5 capitalize transition-colors ${
                  statusFilter === st
                    ? "bg-surface-strong font-semibold text-ink"
                    : "text-muted hover:text-ink"
                }`}
              >
                {st}
              </button>
            ))}
          </div>
        </div>
      )}

      {/* Services Card Grid */}
      {(categoryFilter === "all" || categoryFilter === "parsers") && (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3 mb-10">
          {filteredServices.map((s) => {
            const st = STATUS[s.status];
            const isCore = s.category === "core";
            const isParser = s.category === "parser";
            const isStorage = s.category === "storage";

            return (
              <div
              key={s.id}
              onClick={() => openConfig(s)}
              className="card card-hover group flex min-w-0 flex-col p-5 cursor-pointer transition-all duration-150 hover:border-hairline-strong"
            >
              {/* Header: Logo + Badges & Actions */}
              <div className="flex items-start justify-between gap-3 mb-3">
                <div className="flex items-center gap-3 min-w-0">
                  {/* Category / Engine Icon */}
                  <div
                    className={`flex h-11 w-11 shrink-0 items-center justify-center rounded-xl border ${
                      isCore
                        ? "bg-cyan-50 dark:bg-cyan-950/40 text-cyan-600 dark:text-cyan-400 border-cyan-200 dark:border-cyan-900/60"
                        : isParser
                          ? "bg-purple-50 dark:bg-purple-950/40 text-purple-600 dark:text-purple-400 border-purple-200 dark:border-purple-900/60"
                          : "bg-amber-50 dark:bg-amber-950/40 text-amber-600 dark:text-amber-400 border-amber-200 dark:border-amber-900/60"
                    }`}
                  >
                    {isCore ? (
                      <IconDocReader className="h-5 w-5" />
                    ) : isParser ? (
                      <IconParserEngine className="h-5 w-5" />
                    ) : (
                      <IconStorageEngine className="h-5 w-5" />
                    )}
                  </div>

                  <div className="min-w-0">
                    <div className="flex items-center gap-1.5 flex-wrap">
                      <span className="rounded bg-surface-strong px-2 py-0.5 text-[10.5px] font-semibold uppercase tracking-wider text-muted">
                        {s.category}
                      </span>
                      {s.configurable && (
                        <span className="rounded bg-blue-500/10 px-1.5 py-0.5 text-[10px] font-semibold text-blue-600 dark:text-blue-400 border border-blue-500/20">
                          CONFIG
                        </span>
                      )}
                    </div>
                    <div className="flex items-center gap-1.5 mt-1">
                      <span className={`h-2 w-2 rounded-full shrink-0 ${st.dot}`} />
                      <span className="caption text-[11.5px] font-medium text-muted">
                        {st.label}
                      </span>
                    </div>
                  </div>
                </div>

                {/* Top-right action button */}
                {s.configurable && (
                  <button
                    type="button"
                    className="flex h-7 w-7 items-center justify-center rounded-lg text-muted transition-colors hover:bg-surface-strong hover:text-ink shrink-0"
                    title="Configure service"
                    aria-label="Configure service"
                    onClick={(e) => {
                      e.stopPropagation();
                      openConfig(s);
                    }}
                  >
                    <IconEdit className="h-3.5 w-3.5" />
                  </button>
                )}
              </div>

              {/* Body: Title and Description */}
              <div className="min-w-0 flex-1">
                <h3 className="text-[15px] font-semibold text-ink truncate leading-tight group-hover:text-primary transition-colors">
                  {s.name}
                </h3>
                <p className="caption text-muted line-clamp-2 mt-1.5 min-h-[36px]">
                  {s.description || "—"}
                </p>
              </div>

              {/* Footer: Detail / File types / Reason */}
              <div className="mt-4 flex items-center justify-between gap-2 border-t border-hairline pt-3 min-h-[36px] text-xs">
                {s.fileTypes && s.fileTypes.length > 0 ? (
                  <div className="flex items-center gap-1 overflow-hidden">
                    {s.fileTypes.slice(0, 4).map((ft) => (
                      <span
                        key={ft}
                        className="rounded bg-surface-strong px-1.5 py-0.5 text-[10.5px] font-mono text-muted"
                      >
                        {ft}
                      </span>
                    ))}
                    {s.fileTypes.length > 4 && (
                      <span className="text-[10.5px] text-muted-soft">
                        +{s.fileTypes.length - 4}
                      </span>
                    )}
                  </div>
                ) : s.detail ? (
                  <span
                    className={`caption truncate text-[11.5px] max-w-[220px] font-mono ${
                      s.status === "degraded" ? "text-amber-600 dark:text-amber-400" : "text-muted"
                    }`}
                    title={s.detail}
                  >
                    {s.detail}
                  </span>
                ) : (
                  <span className="text-[11.5px] text-muted-soft">Integrated</span>
                )}

                {s.configurable ? (
                  <span className="caption text-[11.5px] font-medium text-muted group-hover:text-ink transition-colors shrink-0">
                    Configure →
                  </span>
                ) : s.docLink ? (
                  <a
                    href={s.docLink}
                    target="_blank"
                    rel="noreferrer"
                    className="caption flex items-center gap-1 text-[11.5px] text-muted hover:text-ink transition-colors shrink-0"
                    onClick={(e) => e.stopPropagation()}
                  >
                    Docs <IconExternal className="h-3 w-3" />
                  </a>
                ) : null}
              </div>
            </div>
          );
        })}
        </div>
      )}

      {/* Vector Stores Section */}
      {(categoryFilter === "all" || categoryFilter === "vector") && (
        <div className="card p-6 mb-10">
          <VectorStoreSettings />
        </div>
      )}

      {/* Storage Backends Section */}
      {(categoryFilter === "all" || categoryFilter === "storage") && (
        <div className="card p-6 mb-10">
          <StorageSettings />
        </div>
      )}

      {/* Web Search Section */}
      {(categoryFilter === "all" || categoryFilter === "search") && (
        <div className="card p-6 mb-10">
          <WebSearchSettings />
        </div>
      )}

      {/* Sandbox Runtimes Section */}
      {(categoryFilter === "all" || categoryFilter === "sandbox") && (
        <div className="card p-6 mb-10">
          <SandboxSettings />
        </div>
      )}

      {/* Runtime queues section */}
      <h2 className="title-md mb-4 font-semibold text-ink">Runtime Queues</h2>
      {queuesDenied && (
        <p className="caption mb-4 text-muted">
          Runtime queue metrics are only available to system administrators.
        </p>
      )}
      {queuesUnavailable && (
        <p className="caption mb-4 text-muted">
          Queue metrics are unavailable in this deployment.
        </p>
      )}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
        {(queues ?? []).map((q) => (
          <div key={q.name} className="card p-5">
            <div className="mb-4 flex items-center justify-between">
              <span className="text-[14px] font-medium text-ink">{q.name}</span>
              {q.archived > 0 && (
                <span className="caption font-medium text-error">{q.archived} failed</span>
              )}
              {q.paused && <span className="caption font-medium text-muted">paused</span>}
            </div>
            <div className="grid grid-cols-3 gap-2 text-center">
              {[
                { label: "Active", value: q.active },
                { label: "Pending", value: q.pending },
                { label: "Retry", value: q.retry },
              ].map((m) => (
                <div key={m.label}>
                  <div className="display-sm">{m.value}</div>
                  <div className="caption-uppercase mt-1 text-[10px] text-muted-soft">
                    {m.label}
                  </div>
                </div>
              ))}
            </div>
          </div>
        ))}
      </div>
      {queues !== null && queues.length === 0 && !queuesDenied && (
        <p className="caption mt-2 text-muted-soft">No queues reported.</p>
      )}

      {/* Service Configuration & Details Modal */}
      <Modal
        open={modalOpen}
        title={selectedService?.name ?? "Service Details"}
        onClose={() => setModalOpen(false)}
        width="w-[560px]"
      >
        {selectedService && (
          <div className="space-y-4">
            {/* Service info banner */}
            <div className="rounded-xl border border-hairline bg-surface-strong/50 p-4">
              <div className="flex items-center justify-between mb-1">
                <span className="text-[11.5px] font-semibold uppercase tracking-wider text-muted">
                  {selectedService.category} engine
                </span>
                <span
                  className={`rounded-full px-2 py-0.5 text-[11px] font-medium ${
                    STATUS[selectedService.status].badge
                  }`}
                >
                  {STATUS[selectedService.status].label}
                </span>
              </div>
              <p className="body-sm text-body">{selectedService.description}</p>
              {selectedService.detail && (
                <p className="caption mt-2 font-mono text-[11px] text-muted">
                  {selectedService.detail}
                </p>
              )}
            </div>

            {/* Supported file types */}
            {selectedService.fileTypes && selectedService.fileTypes.length > 0 && (
              <div>
                <span className="caption mb-1.5 block font-medium text-muted">
                  Supported file types
                </span>
                <div className="flex flex-wrap gap-1.5">
                  {selectedService.fileTypes.map((ft) => (
                    <span
                      key={ft}
                      className="rounded-md bg-surface-strong px-2 py-0.5 font-mono text-[11px] text-ink"
                    >
                      {ft}
                    </span>
                  ))}
                </div>
              </div>
            )}

            {/* ===================== CONFIGURABLE FORMS ===================== */}

            {/* 1. DocReader config */}
            {selectedService.engineKey === "docreader" && (
              <div className="space-y-4 border-t border-hairline pt-4">
                <label className="block">
                  <span className="caption mb-1.5 block font-medium text-muted">
                    DocReader Address
                  </span>
                  <input
                    className="input font-mono text-sm"
                    placeholder="docreader:50051 or 127.0.0.1:50051"
                    value={config.docreader_addr ?? ""}
                    onChange={(e) =>
                      setConfig({ ...config, docreader_addr: e.target.value })
                    }
                  />
                  <p className="caption text-muted-soft mt-1">
                    DocReader service host & port for parsing documents.
                  </p>
                </label>

                <label className="block">
                  <span className="caption mb-1.5 block font-medium text-muted">Transport</span>
                  <select
                    className="input"
                    value={config.docreader_transport ?? "grpc"}
                    onChange={(e) =>
                      setConfig({ ...config, docreader_transport: e.target.value })
                    }
                  >
                    <option value="grpc">gRPC (recommended)</option>
                    <option value="http">HTTP</option>
                  </select>
                </label>
              </div>
            )}

            {/* 2. MinerU self-hosted config */}
            {selectedService.engineKey === "mineru" && (
              <div className="space-y-4 border-t border-hairline pt-4">
                <label className="block">
                  <span className="caption mb-1.5 block font-medium text-muted">
                    Self-Hosted Endpoint
                  </span>
                  <input
                    className="input font-mono text-sm"
                    placeholder="http://localhost:8000"
                    value={config.mineru_endpoint ?? ""}
                    onChange={(e) => setConfig({ ...config, mineru_endpoint: e.target.value })}
                  />
                </label>

                <div className="grid grid-cols-2 gap-3">
                  <label className="block">
                    <span className="caption mb-1.5 block font-medium text-muted">Backend</span>
                    <select
                      className="input"
                      value={config.mineru_model ?? "pipeline"}
                      onChange={(e) => setConfig({ ...config, mineru_model: e.target.value })}
                    >
                      <option value="pipeline">pipeline</option>
                      <option value="vlm-auto-engine">vlm-auto-engine</option>
                      <option value="vlm-http-client">vlm-http-client</option>
                      <option value="hybrid-auto-engine">hybrid-auto-engine</option>
                      <option value="hybrid-http-client">hybrid-http-client</option>
                    </select>
                  </label>

                  <label className="block">
                    <span className="caption mb-1.5 block font-medium text-muted">
                      Parse Method
                    </span>
                    <select
                      className="input"
                      value={config.mineru_parse_method ?? "auto"}
                      onChange={(e) =>
                        setConfig({
                          ...config,
                          mineru_parse_method: e.target.value as "auto" | "ocr" | "txt",
                        })
                      }
                    >
                      <option value="auto">Auto</option>
                      <option value="ocr">OCR</option>
                      <option value="txt">Text only</option>
                    </select>
                  </label>
                </div>

                <label className="block">
                  <span className="caption mb-1.5 block font-medium text-muted">
                    vLLM Server URL
                  </span>
                  <input
                    className="input font-mono text-sm"
                    placeholder="http://localhost:8001/v1"
                    value={config.mineru_vlm_server_url ?? ""}
                    onChange={(e) =>
                      setConfig({ ...config, mineru_vlm_server_url: e.target.value })
                    }
                  />
                </label>

                <label className="block">
                  <span className="caption mb-1.5 block font-medium text-muted">Language</span>
                  <input
                    className="input"
                    placeholder="ch, en, vi"
                    value={config.mineru_language ?? "ch"}
                    onChange={(e) => setConfig({ ...config, mineru_language: e.target.value })}
                  />
                </label>

                <div className="flex items-center gap-6 pt-1">
                  <label className="flex items-center gap-2 cursor-pointer text-sm text-ink">
                    <input
                      type="checkbox"
                      checked={config.mineru_enable_formula ?? true}
                      onChange={(e) =>
                        setConfig({ ...config, mineru_enable_formula: e.target.checked })
                      }
                      className="h-4 w-4 rounded"
                    />
                    Formula recognition
                  </label>

                  <label className="flex items-center gap-2 cursor-pointer text-sm text-ink">
                    <input
                      type="checkbox"
                      checked={config.mineru_enable_table ?? true}
                      onChange={(e) =>
                        setConfig({ ...config, mineru_enable_table: e.target.checked })
                      }
                      className="h-4 w-4 rounded"
                    />
                    Table recognition
                  </label>
                </div>
              </div>
            )}

            {/* 3. MinerU Cloud API config */}
            {selectedService.engineKey === "mineru_cloud" && (
              <div className="space-y-4 border-t border-hairline pt-4">
                <label className="block">
                  <span className="caption mb-1.5 block font-medium text-muted">API Key</span>
                  <input
                    className="input font-mono text-sm"
                    type="password"
                    placeholder="Enter MinerU Cloud API Key"
                    value={config.mineru_api_key ?? ""}
                    onChange={(e) => setConfig({ ...config, mineru_api_key: e.target.value })}
                  />
                </label>

                <div className="grid grid-cols-2 gap-3">
                  <label className="block">
                    <span className="caption mb-1.5 block font-medium text-muted">
                      Model Version
                    </span>
                    <select
                      className="input"
                      value={config.mineru_cloud_model ?? "pipeline"}
                      onChange={(e) =>
                        setConfig({ ...config, mineru_cloud_model: e.target.value })
                      }
                    >
                      <option value="pipeline">pipeline</option>
                      <option value="vlm">VLM</option>
                      <option value="MinerU-HTML">MinerU-HTML</option>
                    </select>
                  </label>

                  <label className="block">
                    <span className="caption mb-1.5 block font-medium text-muted">Language</span>
                    <input
                      className="input"
                      placeholder="ch, en"
                      value={config.mineru_cloud_language ?? "ch"}
                      onChange={(e) =>
                        setConfig({ ...config, mineru_cloud_language: e.target.value })
                      }
                    >
                    </input>
                  </label>
                </div>

                <div className="flex flex-wrap items-center gap-6 pt-1">
                  <label className="flex items-center gap-2 cursor-pointer text-sm text-ink">
                    <input
                      type="checkbox"
                      checked={config.mineru_cloud_enable_formula ?? true}
                      onChange={(e) =>
                        setConfig({ ...config, mineru_cloud_enable_formula: e.target.checked })
                      }
                      className="h-4 w-4 rounded"
                    />
                    Formula recognition
                  </label>

                  <label className="flex items-center gap-2 cursor-pointer text-sm text-ink">
                    <input
                      type="checkbox"
                      checked={config.mineru_cloud_enable_table ?? true}
                      onChange={(e) =>
                        setConfig({ ...config, mineru_cloud_enable_table: e.target.checked })
                      }
                      className="h-4 w-4 rounded"
                    />
                    Table recognition
                  </label>

                  <label className="flex items-center gap-2 cursor-pointer text-sm text-ink">
                    <input
                      type="checkbox"
                      checked={config.mineru_cloud_enable_ocr ?? true}
                      onChange={(e) =>
                        setConfig({ ...config, mineru_cloud_enable_ocr: e.target.checked })
                      }
                      className="h-4 w-4 rounded"
                    />
                    OCR
                  </label>
                </div>
              </div>
            )}

            {/* 4. PaddleOCR-VL self-hosted config */}
            {selectedService.engineKey === "paddleocr_vl" && (
              <div className="space-y-4 border-t border-hairline pt-4">
                <label className="block">
                  <span className="caption mb-1.5 block font-medium text-muted">
                    Self-Hosted Endpoint
                  </span>
                  <input
                    className="input font-mono text-sm"
                    placeholder="http://localhost:8080/v1"
                    value={config.paddleocr_vl_endpoint ?? ""}
                    onChange={(e) =>
                      setConfig({ ...config, paddleocr_vl_endpoint: e.target.value })
                    }
                  />
                </label>

                <div className="flex items-center gap-6 pt-1">
                  <label className="flex items-center gap-2 cursor-pointer text-sm text-ink">
                    <input
                      type="checkbox"
                      checked={config.paddleocr_vl_use_seal_recognition ?? true}
                      onChange={(e) =>
                        setConfig({
                          ...config,
                          paddleocr_vl_use_seal_recognition: e.target.checked,
                        })
                      }
                      className="h-4 w-4 rounded"
                    />
                    Seal recognition
                  </label>

                  <label className="flex items-center gap-2 cursor-pointer text-sm text-ink">
                    <input
                      type="checkbox"
                      checked={config.paddleocr_vl_use_chart_recognition ?? false}
                      onChange={(e) =>
                        setConfig({
                          ...config,
                          paddleocr_vl_use_chart_recognition: e.target.checked,
                        })
                      }
                      className="h-4 w-4 rounded"
                    />
                    Chart recognition
                  </label>
                </div>
              </div>
            )}

            {/* 5. PaddleOCR-VL Cloud API config */}
            {selectedService.engineKey === "paddleocr_vl_cloud" && (
              <div className="space-y-4 border-t border-hairline pt-4">
                <label className="block">
                  <span className="caption mb-1.5 block font-medium text-muted">Cloud Token</span>
                  <input
                    className="input font-mono text-sm"
                    type="password"
                    placeholder="Enter PaddleOCR-VL Cloud Token"
                    value={config.paddleocr_vl_cloud_token ?? ""}
                    onChange={(e) =>
                      setConfig({ ...config, paddleocr_vl_cloud_token: e.target.value })
                    }
                  />
                </label>

                <label className="block">
                  <span className="caption mb-1.5 block font-medium text-muted">Model</span>
                  <input
                    className="input font-mono text-sm"
                    placeholder="PaddleOCR-VL-1.6"
                    value={config.paddleocr_vl_cloud_model ?? "PaddleOCR-VL-1.6"}
                    onChange={(e) =>
                      setConfig({ ...config, paddleocr_vl_cloud_model: e.target.value })
                    }
                  />
                </label>

                <div className="flex items-center gap-6 pt-1">
                  <label className="flex items-center gap-2 cursor-pointer text-sm text-ink">
                    <input
                      type="checkbox"
                      checked={config.paddleocr_vl_cloud_use_seal_recognition ?? true}
                      onChange={(e) =>
                        setConfig({
                          ...config,
                          paddleocr_vl_cloud_use_seal_recognition: e.target.checked,
                        })
                      }
                      className="h-4 w-4 rounded"
                    />
                    Seal recognition
                  </label>

                  <label className="flex items-center gap-2 cursor-pointer text-sm text-ink">
                    <input
                      type="checkbox"
                      checked={config.paddleocr_vl_cloud_use_chart_recognition ?? false}
                      onChange={(e) =>
                        setConfig({
                          ...config,
                          paddleocr_vl_cloud_use_chart_recognition: e.target.checked,
                        })
                      }
                      className="h-4 w-4 rounded"
                    />
                    Chart recognition
                  </label>
                </div>
              </div>
            )}

            {/* Save error & Connection test result */}
            {saveError && <p className="caption text-error">{saveError}</p>}
            {checkResult && (
              <div
                className={`rounded-lg p-3 text-xs flex items-center gap-2 ${
                  checkResult.ok
                    ? "bg-emerald-50 dark:bg-emerald-950/40 text-emerald-700 dark:text-emerald-300 border border-emerald-500/20"
                    : "bg-rose-50 dark:bg-rose-950/40 text-rose-700 dark:text-rose-300 border border-rose-500/20"
                }`}
              >
                <IconCheck
                  className={`h-4 w-4 shrink-0 ${
                    checkResult.ok ? "text-emerald-600" : "text-rose-600"
                  }`}
                />
                <span>{checkResult.message}</span>
              </div>
            )}

            {/* Modal actions footer */}
            <div className="flex items-center justify-between border-t border-hairline pt-4">
              <div>
                {selectedService.configurable && (
                  <button
                    type="button"
                    className="btn btn-outline btn-sm"
                    onClick={() => void handleTestConnection()}
                    disabled={checking || saving}
                  >
                    {checking ? "Testing…" : "Test Connection"}
                  </button>
                )}
              </div>

              <div className="flex gap-2">
                <button
                  type="button"
                  className="btn btn-outline btn-sm"
                  onClick={() => setModalOpen(false)}
                >
                  {selectedService.configurable ? "Cancel" : "Close"}
                </button>
                {selectedService.configurable && (
                  <button
                    type="button"
                    className="btn btn-primary btn-sm"
                    onClick={() => void handleSaveConfig()}
                    disabled={saving}
                  >
                    {saving ? "Saving…" : "Save Configuration"}
                  </button>
                )}
              </div>
            </div>
          </div>
        )}
      </Modal>
    </div>
  );
}
