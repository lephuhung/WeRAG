"use client";

import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type MouseEvent as ReactMouseEvent,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
} from "react";
import {
  cancelKnowledgeParse,
  getKnowledgeDetails,
  getKnowledgeSpans,
  reparseKnowledge,
  type KnowledgeProcessOverrides,
} from "@/lib/api/knowledge";
import {
  groupPostprocessGraphSpans,
  knowledgeSpansPayloadHasTrace,
  resolveTimelineHeaderStatus,
  summarizePostprocessTasks,
  type KnowledgeTraceNode,
} from "@/lib/knowledge-trace";
import { copyToClipboard } from "@/lib/clipboard";
import { useT, type LocaleKey } from "@/lib/i18n";
import {
  IconCheck,
  IconChevronDown,
  IconChevronRight,
  IconClose,
  IconCopy,
  IconInfoCircle,
  IconMinusCircle,
  IconRefresh,
} from "@/components/icons";

/* Ported from frontend/src/components/knowledge-processing-timeline.vue:
 * Langfuse-style waterfall of the document processing pipeline
 * (docreader → chunking → embedding → multimodal → postprocess), with
 * per-span detail panel, attempt switching, cancel/retry and live
 * polling while the trace is active. */

type SpanNode = KnowledgeTraceNode;

interface LastError {
  name: string;
  error_code: string;
  error_message: string;
  finished_at?: string;
}

interface SpansResponse {
  knowledge_id: string;
  attempt: number;
  latest_attempt: number;
  current_attempt?: number;
  parse_status: string;
  current_stage?: string;
  trace: SpanNode;
  last_error?: LastError | null;
}

export interface TimelineSummary {
  totalMs: number;
  status: string;
  stageIndex: number;
  stageTotal: number;
  stageLabel: string;
}

const STAGES = ["docreader", "chunking", "embedding", "multimodal", "postprocess"] as const;
const POLL_INTERVAL_MS = 2000;
/* Keeps the poll loop alive briefly after the trace goes quiet so
 * late-arriving async postprocess subspans (wiki debounce ~30s, summary,
 * question, graph.chunk[*]) still surface without a manual refresh. */
const QUIESCE_GRACE_MS = 2 * 60 * 1000;

const STAGE_LABEL: Record<string, LocaleKey> = {
  docreader: "ks.stage.docreader",
  chunking: "ks.stage.chunking",
  embedding: "ks.stage.embedding",
  multimodal: "ks.stage.multimodal",
  postprocess: "ks.stage.postprocess",
};

const STATUS_LABEL: Record<string, LocaleKey> = {
  pending: "ks.status.pending",
  running: "ks.status.running",
  processing: "ks.status.running",
  finalizing: "ks.status.finalizing",
  done: "ks.status.done",
  completed: "ks.status.done",
  failed: "ks.status.failed",
  skipped: "ks.status.skipped",
  cancelled: "ks.status.cancelled",
};

const DOT_CLS: Record<string, string> = {
  done: "bg-success",
  completed: "bg-success",
  failed: "bg-error",
  running: "bg-amber-500 kp-live-pulse",
  processing: "bg-amber-500 kp-live-pulse",
  finalizing: "bg-amber-500 kp-live-pulse",
  pending: "border border-hairline-strong bg-transparent",
  skipped: "bg-muted-soft/40",
  cancelled: "border border-dashed border-muted-soft bg-transparent",
};

const BAR_CLS: Record<string, string> = {
  done: "bg-success",
  completed: "bg-success",
  failed: "bg-error",
  running: "kp-bar-running kp-bar-sweep",
  processing: "kp-bar-running kp-bar-sweep",
  finalizing: "kp-bar-running kp-bar-sweep",
  skipped: "bg-muted-soft/40",
  cancelled: "border border-dashed border-error/70 bg-transparent",
};

const WRAP_CLS: Record<string, string> = {
  done: "border-success/40",
  completed: "border-success/40",
  failed: "border-error/50",
  running: "border-amber-500/50",
  processing: "border-amber-500/50",
  finalizing: "border-amber-500/50",
  cancelled: "border-error/30",
};

const CHIP_CLS: Record<string, string> = {
  done: "bg-success/10 text-success",
  completed: "bg-success/10 text-success",
  running: "bg-amber-500/15 text-amber-600",
  processing: "bg-amber-500/15 text-amber-600",
  finalizing: "bg-amber-500/15 text-amber-600",
  failed: "bg-error/10 text-error",
  cancelled: "bg-surface-strong text-muted",
  skipped: "bg-surface-strong text-muted-soft",
  pending: "bg-surface-strong text-muted",
};

function dotCls(status: string, placeholder = false) {
  if (placeholder) return "border border-dashed border-hairline-strong bg-transparent";
  return DOT_CLS[status] ?? "bg-muted-soft";
}

function barCls(status: string) {
  return BAR_CLS[status] ?? "bg-muted-soft";
}

function isPollingStatus(status?: string): boolean {
  /* finalizing is the post-process fan-out window — subspans
   * (summary/question/graph.chunk[*]) still produce events. */
  return status === "pending" || status === "processing" || status === "finalizing";
}

/* Hard-terminal statuses override traceActive: a cancel or crash can
 * leave child spans stranded in 'running' — never poll forever on them. */
function isHardTerminal(status?: string): boolean {
  return status === "cancelled" || status === "failed" || status === "completed";
}

function spanTreeActive(node?: SpanNode): boolean {
  if (!node) return false;
  if (node.status === "running" || node.status === "pending") return true;
  return (node.children || []).some(spanTreeActive);
}

function parseTime(s?: string | null): number | null {
  if (!s) return null;
  const t = Date.parse(s);
  return Number.isNaN(t) ? null : t;
}

function nodeStart(node: SpanNode): number | null {
  return parseTime(node.started_at || undefined);
}

function nodeEnd(node: SpanNode): number | null {
  const e = parseTime(node.finished_at || undefined);
  if (e !== null) return e;
  const s = nodeStart(node);
  if (s !== null && typeof node.duration_ms === "number" && node.duration_ms > 0) {
    return s + node.duration_ms;
  }
  return null;
}

function collectStarts(node: SpanNode | undefined, out: number[]) {
  if (!node) return;
  const s = nodeStart(node);
  if (s !== null) out.push(s);
  for (const c of node.children || []) collectStarts(c, out);
}

function collectEnds(node: SpanNode | undefined, out: number[]) {
  if (!node) return;
  const e = nodeEnd(node);
  if (e !== null) out.push(e);
  for (const c of node.children || []) collectEnds(c, out);
}

/* Freshest timestamp anywhere in the tree — drives the quiesce-grace
 * window ("did this trace finish recently, or is it an old completed
 * one we shouldn't waste polls on?"). */
function spanTreeLastActivity(node?: SpanNode): number {
  if (!node) return 0;
  let max = 0;
  for (const s of [node.updated_at, node.finished_at, node.started_at, node.created_at]) {
    const t = parseTime(s || undefined);
    if (t !== null && t > max) max = t;
  }
  for (const c of node.children || []) {
    const t = spanTreeLastActivity(c);
    if (t > max) max = t;
  }
  return max;
}

function formatDuration(ms?: number): string {
  if (ms === undefined || ms === null || isNaN(ms) || ms < 0) return "—";
  if (ms < 1000) return `${Math.round(ms)}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(2)}s`;
  const mins = Math.floor(ms / 60000);
  const rem = ((ms % 60000) / 1000).toFixed(1);
  return `${mins}m${rem}s`;
}

/* A skipped stage did not execute — its bookkeeping interval is not
 * processing time, so don't present it as such. */
function formatSpanDuration(node: SpanNode): string {
  if (node.status === "skipped" || node.status === "pending") return "—";
  return formatDuration(node.duration_ms);
}

function formatTime(s?: string | null): string {
  if (!s) return "—";
  const ts = Date.parse(s);
  if (Number.isNaN(ts)) return s;
  const d = new Date(ts);
  const ms = String(d.getMilliseconds()).padStart(3, "0");
  const hh = String(d.getHours()).padStart(2, "0");
  const mm = String(d.getMinutes()).padStart(2, "0");
  const ss = String(d.getSeconds()).padStart(2, "0");
  /* Omit the year when it matches the current one; month+day always
   * shown so older traces stay unambiguous. */
  const now = new Date();
  const yyyy = d.getFullYear();
  const mo = String(d.getMonth() + 1).padStart(2, "0");
  const dd = String(d.getDate()).padStart(2, "0");
  const datePart = yyyy === now.getFullYear() ? `${mo}-${dd}` : `${yyyy}-${mo}-${dd}`;
  return `${datePart} ${hh}:${mm}:${ss}.${ms}`;
}

function isObjectWithKeys(v: unknown): v is Record<string, unknown> {
  return !!v && typeof v === "object" && !Array.isArray(v) && Object.keys(v).length > 0;
}

function hasContent(v: unknown): boolean {
  if (v === null || v === undefined || v === "") return false;
  if (Array.isArray(v)) return v.length > 0;
  if (typeof v === "object") return Object.keys(v).length > 0;
  return true;
}

function prettyJSON(v: unknown): string {
  try {
    return JSON.stringify(v, null, 2);
  } catch {
    return String(v);
  }
}

function humanizeKey(k: string): string {
  return k
    .replace(/[_-]+/g, " ")
    .replace(/\s+/g, " ")
    .trim()
    .replace(/\b([a-z])/g, (_, c: string) => c.toUpperCase());
}

interface KvEntry {
  key: string;
  label: string;
  kind: "scalar" | "bool" | "array" | "object";
  display: string;
  raw: unknown;
  /* Short payloads render their JSON inline — no extra click needed. */
  defaultExpanded: boolean;
}

const KV_INLINE_ARRAY_LIMIT = 8;
const KV_INLINE_OBJECT_KEY_LIMIT = 8;
const KV_INLINE_JSON_BYTES_LIMIT = 600;

function shouldInlineExpand(value: unknown): boolean {
  if (Array.isArray(value)) {
    if (value.length > KV_INLINE_ARRAY_LIMIT) return false;
    try {
      return JSON.stringify(value).length <= KV_INLINE_JSON_BYTES_LIMIT;
    } catch {
      return false;
    }
  }
  if (value && typeof value === "object") {
    if (Object.keys(value).length > KV_INLINE_OBJECT_KEY_LIMIT) return false;
    try {
      return JSON.stringify(value).length <= KV_INLINE_JSON_BYTES_LIMIT;
    } catch {
      return false;
    }
  }
  return false;
}

function toKvEntry(key: string, value: unknown): KvEntry {
  const label = humanizeKey(key);
  if (value === null || value === undefined) {
    return { key, label, kind: "scalar", display: "—", raw: value, defaultExpanded: false };
  }
  if (typeof value === "boolean") {
    return { key, label, kind: "bool", display: value ? "true" : "false", raw: value, defaultExpanded: false };
  }
  if (typeof value === "number") {
    return { key, label, kind: "scalar", display: value.toLocaleString(), raw: value, defaultExpanded: false };
  }
  if (typeof value === "string") {
    return { key, label, kind: "scalar", display: value, raw: value, defaultExpanded: false };
  }
  if (Array.isArray(value)) {
    return {
      key, label, kind: "array",
      display: `Array · ${value.length}`, raw: value,
      defaultExpanded: shouldInlineExpand(value),
    };
  }
  if (typeof value === "object") {
    const n = Object.keys(value as object).length;
    return {
      key, label, kind: "object",
      display: `Object · ${n} keys`, raw: value,
      defaultExpanded: shouldInlineExpand(value),
    };
  }
  return { key, label, kind: "scalar", display: String(value), raw: value, defaultExpanded: false };
}

function buildKvEntries(obj: unknown): KvEntry[] {
  if (!isObjectWithKeys(obj)) return [];
  return Object.entries(obj).map(([key, value]) => toKvEntry(key, value));
}

function normalizeFileType(value: string): string {
  return String(value || "").trim().replace(/^\./, "").toLowerCase();
}

function getFileTypeFromName(name: string): string {
  const clean = String(name || "").split(/[?#]/)[0];
  const dot = clean.lastIndexOf(".");
  return dot >= 0 ? clean.slice(dot + 1) : "";
}

interface FlatRow {
  key: string;
  depth: number;
  node: SpanNode;
  hasChildren: boolean;
  isRoot: boolean;
  isStage: boolean;
  parentKey?: string;
}

function rowKey(node: SpanNode, fallback: string): string {
  return node.span_id || fallback;
}

function isPlaceholder(node: SpanNode): boolean {
  return !node.span_id && !node.started_at;
}

export function ProcessingTimeline({
  knowledgeId,
  parseStatus,
  autoPoll = true,
  compact = false,
  /* gracePoll=true (drawer mount): poll through post-pipeline async work
   * + a grace window after the tree quiesces. false (hidden badge
   * driver): poll only while parse_status itself is non-terminal. */
  gracePoll = true,
  docTitle = "",
  showClose = false,
  onHasSpans,
  onSummary,
  onClose,
}: {
  knowledgeId: string;
  parseStatus?: string;
  autoPoll?: boolean;
  compact?: boolean;
  gracePoll?: boolean;
  docTitle?: string;
  showClose?: boolean;
  onHasSpans?: (has: boolean) => void;
  onSummary?: (summary: TimelineSummary) => void;
  onClose?: () => void;
}) {
  const { t } = useT();

  const [data, setData] = useState<SpansResponse | null>(null);
  const [processOverrides, setProcessOverrides] = useState<KnowledgeProcessOverrides | null>(null);
  const [currentFileType, setCurrentFileType] = useState("");
  const [loading, setLoading] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [selectedAttempt, setSelectedAttemptState] = useState<number | undefined>(undefined);
  const [expandedRows, setExpandedRows] = useState<Set<string>>(new Set(["__root__"]));
  const [selectedSpanId, setSelectedSpanId] = useState<string | null>(null);
  const [expandedJsonKeys, setExpandedJsonKeys] = useState<Set<string>>(new Set());
  const [nowTick, setNowTick] = useState(Date.now());
  const [detailTab, setDetailTab] = useState<"overview" | "input" | "output" | "metadata" | "raw">("overview");
  const [lastFetchedAt, setLastFetchedAt] = useState(0);
  const [lastFetchOk, setLastFetchOk] = useState(true);
  const [failedAttempts, setFailedAttempts] = useState(0);
  const [attemptStatuses, setAttemptStatuses] = useState<Map<number, string>>(new Map());
  const [cancelling, setCancelling] = useState(false);
  const [confirmCancel, setConfirmCancel] = useState(false);
  const [notice, setNotice] = useState<{ kind: "ok" | "err"; text: string } | null>(null);
  const [copiedKey, setCopiedKey] = useState<string | null>(null);

  const scrollRef = useRef<HTMLDivElement | null>(null);
  /* One permanent interval drives polling; the tick reads the freshest
   * "should I fetch?" decision via this ref — no re-arming, no chains. */
  const fetchInFlightRef = useRef(false);
  const shouldPollRef = useRef(false);
  const fetchRef = useRef<() => void>(() => {});
  const attemptRef = useRef<number | undefined>(undefined);
  const dataRef = useRef<SpansResponse | null>(null);
  const attemptStatusRef = useRef<Set<number>>(new Set());
  const userToggledRef = useRef<Set<string>>(new Set());
  const setSelectedAttempt = (n: number | undefined) => {
    attemptRef.current = n;
    setSelectedAttemptState(n);
  };
  /* Latest props for callbacks fired inside async fetches. */
  const propsRef = useRef({ knowledgeId, parseStatus, compact, gracePoll, onHasSpans });
  useEffect(() => {
    propsRef.current = { knowledgeId, parseStatus, compact, gracePoll, onHasSpans };
  });

  const localizedStatus = useCallback(
    (status: string): string => {
      const key = STATUS_LABEL[status];
      return key ? t(key) : status;
    },
    [t],
  );

  const stageLabel = useCallback(
    (name: string): string => {
      const key = STAGE_LABEL[name];
      return key ? t(key) : name;
    },
    [t],
  );

  /* ===== derived trace data ===== */

  const stages = useMemo<SpanNode[]>(() => {
    const children = data?.trace?.children || [];
    const byName = new Map<string, SpanNode>();
    for (const c of children) {
      if (c && c.kind === "stage" && c.name) {
        byName.set(c.name, c.name === "postprocess" ? groupPostprocessGraphSpans(c) : c);
      }
    }
    return STAGES.map(
      (n) => byName.get(n) || ({ name: n, kind: "stage", status: "pending" } as SpanNode),
    );
  }, [data]);

  const traceActive = useMemo(() => spanTreeActive(data?.trace), [data]);

  const isLive = useMemo<boolean>(() => {
    if (data) {
      /* Hard-terminal parse_status wins over a stale traceActive:
       * cancel/irrecoverable failure can leave child spans stranded as
       * 'running' — stop polling on those. */
      if (isHardTerminal(data.parse_status)) return false;
      return isPollingStatus(data.parse_status) || traceActive;
    }
    if (isHardTerminal(parseStatus)) return false;
    return isPollingStatus(parseStatus);
  }, [data, traceActive, parseStatus]);

  const lastTraceActivityAt = useMemo(() => spanTreeLastActivity(data?.trace), [data]);

  const isWithinQuiesceGrace = useMemo<boolean>(() => {
    if (isLive) return false;
    if (!lastTraceActivityAt) return false;
    return nowTick - lastTraceActivityAt < QUIESCE_GRACE_MS;
  }, [isLive, lastTraceActivityAt, nowTick]);

  const shouldPollNow = useCallback((): boolean => {
    if (!data) return isPollingStatus(parseStatus);
    if (gracePoll) return isLive || isWithinQuiesceGrace;
    return isPollingStatus(data.parse_status);
  }, [data, gracePoll, isLive, isWithinQuiesceGrace, parseStatus]);

  useEffect(() => {
    shouldPollRef.current = shouldPollNow();
  });

  /* ===== fetch ===== */

  const fetchSpans = useCallback(
    async (opts: { manual?: boolean } = {}) => {
      const { knowledgeId: id, onHasSpans: emitHas } = propsRef.current;
      if (!id) return;
      if (fetchInFlightRef.current) return;
      fetchInFlightRef.current = true;
      if (opts.manual) setRefreshing(true);
      if (!dataRef.current) setLoading(true);
      let attemptOk = false;
      try {
        const res = (await getKnowledgeSpans(id, attemptRef.current)) as {
          success?: boolean;
          data?: SpansResponse;
        };
        if (res?.success && res.data) {
          const payload = res.data;
          dataRef.current = payload;
          setData(payload);
          attemptOk = true;
          if (attemptRef.current === undefined) {
            setSelectedAttempt(payload.attempt);
          }
          /* Auto-expand, on EVERY fetch and at EVERY depth: rows with
           * children expand unless the user has toggled them. Deep
           * subspans (e.g. postprocess.wiki.page[*]) appearing later in
           * the run surface automatically. */
          setExpandedRows((prev) => {
            const next = new Set(prev);
            next.add("__root__");
            const autoExpand = (n: SpanNode) => {
              const key = n.span_id || `stage:${n.name}`;
              const kids = n.children || [];
              if (kids.length > 0 && !userToggledRef.current.has(key)) next.add(key);
              kids.forEach(autoExpand);
            };
            for (const stage of payload.trace?.children || []) autoExpand(stage);
            return next;
          });
          const latestAttempt = payload.latest_attempt || payload.attempt || 0;
          const tabStatus =
            resolveTimelineHeaderStatus({
              parseStatus: payload.parse_status,
              traceStatus: payload.trace?.status,
              isLatestAttempt: payload.attempt === latestAttempt,
            }) || "running";
          setAttemptStatuses((prev) => new Map(prev).set(payload.attempt, tabStatus));
          ensureAttemptStatuses(payload.latest_attempt || 0);
          emitHas?.(knowledgeSpansPayloadHasTrace(payload));
        } else {
          emitHas?.(false);
        }
      } catch (e) {
        console.warn("[ProcessingTimeline] fetchSpans failed", e);
        emitHas?.(false);
      } finally {
        setLastFetchedAt(Date.now());
        setLastFetchOk(attemptOk);
        setFailedAttempts((n) => (attemptOk ? 0 : n + 1));
        setLoading(false);
        setRefreshing(false);
        fetchInFlightRef.current = false;
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [],
  );

  /* Per-attempt header statuses for the attempt tabs — the main fetch
   * only describes the selected attempt, so the other attempts are
   * queried once each. */
  const ensureAttemptStatuses = useCallback(
    (latest: number) => {
      const id = propsRef.current.knowledgeId;
      if (!id || latest <= 1) return;
      for (let n = 1; n <= latest; n++) {
        if (attemptStatusRef.current.has(n)) continue;
        attemptStatusRef.current.add(n);
        getKnowledgeSpans(id, n)
          .then((res) => {
            const r = res as { success?: boolean; data?: SpansResponse };
            if (r?.success && r.data?.trace) {
              const status =
                resolveTimelineHeaderStatus({
                  parseStatus: r.data.parse_status,
                  traceStatus: r.data.trace?.status,
                  isLatestAttempt: n === latest,
                }) || "running";
              setAttemptStatuses((prev) => new Map(prev).set(n, status));
            }
          })
          .catch(() => {});
      }
    },
    [],
  );

  useEffect(() => {
    fetchRef.current = () => void fetchSpans();
  }, [fetchSpans]);

  const fetchProcessOverrides = useCallback(async () => {
    const { knowledgeId: id, compact: isCompact } = propsRef.current;
    if (isCompact || !id) return;
    try {
      const res = (await getKnowledgeDetails(id)) as {
        success?: boolean;
        data?: {
          metadata?: { process_overrides?: KnowledgeProcessOverrides };
          file_type?: string;
          file_name?: string;
          title?: string;
        };
      };
      if (res?.success && res.data) {
        setProcessOverrides(res.data.metadata?.process_overrides ?? null);
        setCurrentFileType(
          normalizeFileType(res.data.file_type || getFileTypeFromName(res.data.file_name || res.data.title || "")),
        );
      }
    } catch {
      setProcessOverrides(null);
      setCurrentFileType("");
    }
  }, []);

  /* Reset + initial fetch whenever the document changes; mount the
   * permanent poll/now intervals once. */
  useEffect(() => {
    setSelectedAttempt(undefined);
    dataRef.current = null;
    setData(null);
    setProcessOverrides(null);
    setCurrentFileType("");
    setExpandedRows(new Set(["__root__"]));
    setSelectedSpanId(null);
    setAttemptStatuses(new Map());
    attemptStatusRef.current = new Set();
    userToggledRef.current = new Set();
    setNotice(null);
    setConfirmCancel(false);
    void fetchSpans();
    void fetchProcessOverrides();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [knowledgeId]);

  useEffect(() => {
    let poll: ReturnType<typeof setInterval> | null = null;
    if (autoPoll) {
      poll = setInterval(() => {
        if (fetchInFlightRef.current) return;
        if (!shouldPollRef.current) return;
        fetchRef.current();
      }, POLL_INTERVAL_MS);
    }
    const now = setInterval(() => setNowTick(Date.now()), 1000);
    const onKeydown = (ev: KeyboardEvent) => {
      if (ev.key === "Escape") setSelectedSpanId((s) => (s ? null : s));
    };
    window.addEventListener("keydown", onKeydown);
    return () => {
      if (poll) clearInterval(poll);
      clearInterval(now);
      window.removeEventListener("keydown", onKeydown);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [knowledgeId, autoPoll]);

  /* ===== waterfall geometry ===== */

  const traceRoot = useMemo<SpanNode | null>(() => {
    const trace = data?.trace;
    if (!trace) return null;
    return {
      ...trace,
      name: trace.name || "knowledge_processing",
      kind: trace.kind || "root",
      children: stages,
    };
  }, [data, stages]);

  const t0 = useMemo<number | null>(() => {
    if (!traceRoot) return null;
    const direct = nodeStart(traceRoot);
    if (direct !== null) return direct;
    const all: number[] = [];
    collectStarts(traceRoot, all);
    return all.length ? Math.min(...all) : null;
  }, [traceRoot]);

  const tEnd = useMemo<number | null>(() => {
    if (!traceRoot) return null;
    const direct = parseTime(traceRoot.finished_at || undefined);
    const all: number[] = [];
    collectEnds(traceRoot, all);
    let candidate: number | null = direct;
    if (all.length > 0) {
      const max = Math.max(...all);
      candidate = candidate === null ? max : Math.max(candidate, max);
    }
    /* Extend the right edge to "now" while the trace is still producing
     * spans — covers parse_status mid-flight AND async postprocess
     * subspans that keep running after the root closes. */
    if (isLive) {
      candidate = candidate === null ? nowTick : Math.max(candidate, nowTick);
    }
    return candidate;
  }, [traceRoot, isLive, nowTick]);

  const totalMs = useMemo<number>(() => {
    if (t0 === null || tEnd === null) return 0;
    /* The trace's own duration_ms covers only the parsing pipeline;
     * async postprocess subspans keep producing rows after the root
     * closes, so scale to the latest descendant end. */
    const observed = Math.max(0, tEnd - t0);
    const traceDur = data?.trace?.duration_ms;
    if (typeof traceDur === "number" && traceDur > 0) return Math.max(traceDur, observed);
    return observed;
  }, [t0, tEnd, data]);

  const showRuler = totalMs >= 50;

  const rulerTicks = useMemo(() => {
    if (!showRuler) return [] as { left: string; label: string }[];
    const total = totalMs;
    return [0, 0.25, 0.5, 0.75, 1].map((f) => ({
      left: `${f * 100}%`,
      label: formatDuration(total * f),
    }));
  }, [showRuler, totalMs]);

  /* "Now" cursor position — lets the user see time advancing while
   * polling even when a running bar grows slowly. */
  const nowMarkerPct = useMemo<number | null>(() => {
    if (!isLive || !t0 || !totalMs) return null;
    const pct = ((nowTick - t0) / totalMs) * 100;
    return Math.max(0, Math.min(100, pct));
  }, [isLive, t0, totalMs, nowTick]);

  const flatRows = useMemo<FlatRow[]>(() => {
    if (!traceRoot) return [];
    const rows: FlatRow[] = [];
    const rootK = rowKey(traceRoot, "__root__");
    rows.push({
      key: rootK, depth: 0, node: traceRoot,
      hasChildren: (traceRoot.children || []).length > 0,
      isRoot: true, isStage: false,
    });
    for (const stage of traceRoot.children || []) {
      const stageKey = rowKey(stage, `stage:${stage.name}`);
      const stageChildren = stage.children || [];
      rows.push({
        key: stageKey, depth: 1, node: stage,
        hasChildren: stageChildren.length > 0,
        isRoot: false, isStage: true, parentKey: rootK,
      });
      if (!expandedRows.has(stageKey)) continue;
      const walk = (n: SpanNode, depth: number, idxPath: string, parentK: string) => {
        const key = rowKey(n, `${idxPath}:${n.name}`);
        const kids = n.children || [];
        rows.push({
          key, depth, node: n,
          hasChildren: kids.length > 0,
          isRoot: false, isStage: false, parentKey: parentK,
        });
        /* Honour expand/collapse at every depth — without this gate a
         * collapsed subspan still rendered its children. */
        if (!expandedRows.has(key)) return;
        kids.forEach((c, i) => walk(c, depth + 1, `${idxPath}/${i}`, key));
      };
      stageChildren.forEach((c, i) => walk(c, 2, `${stageKey}/${i}`, stageKey));
    }
    return rows;
  }, [traceRoot, expandedRows]);

  const selectedRow = useMemo<FlatRow | null>(() => {
    if (!selectedSpanId) return null;
    return flatRows.find((r) => r.key === selectedSpanId) || null;
  }, [selectedSpanId, flatRows]);

  const detailOpen = selectedSpanId !== null && selectedRow !== null;

  /* ===== row helpers ===== */

  function barStyle(node: SpanNode): CSSProperties {
    if (!totalMs || t0 === null) return { display: "none" };
    const start = nodeStart(node);
    if (start === null) return { display: "none" };
    /* A span with no finished_at uses "now" as its end while it's
     * plausibly still running — otherwise postprocess subspans that
     * survived past parse_status='completed' collapse to zero width. */
    const liveBar = isLive || node.status === "running" || node.status === "pending";
    const end = nodeEnd(node) ?? (liveBar ? nowTick : start);
    const leftPct = ((start - t0) / totalMs) * 100;
    const widthPct = Math.max(0.4, ((end - start) / totalMs) * 100);
    return {
      left: `${Math.max(0, Math.min(100, leftPct))}%`,
      width: `${Math.min(100 - Math.max(0, leftPct), widthPct)}%`,
    };
  }

  /* Latest descendant end — async postprocess children often extend
   * well past the parent's own finished_at. */
  function descendantMaxEnd(node: SpanNode): number | null {
    const ends: number[] = [];
    for (const c of node.children || []) collectEnds(c, ends);
    return ends.length ? Math.max(...ends) : null;
  }

  /* Faint outline from the parent's start to the latest descendant end;
   * only drawn when descendants extend ≥50ms past the parent, otherwise
   * it just duplicates the solid self-bar. */
  function wrapStyle(node: SpanNode): CSSProperties | null {
    if (!totalMs || t0 === null) return null;
    const start = nodeStart(node);
    if (start === null) return null;
    const selfEnd = nodeEnd(node) ?? start;
    const childEnd = descendantMaxEnd(node);
    if (childEnd === null || childEnd - selfEnd < 50) return null;
    const leftPct = ((start - t0) / totalMs) * 100;
    const widthPct = Math.max(0.4, ((childEnd - start) / totalMs) * 100);
    return {
      left: `${Math.max(0, Math.min(100, leftPct))}%`,
      width: `${Math.min(100 - Math.max(0, leftPct), widthPct)}%`,
    };
  }

  function wrapDurationMs(node: SpanNode): number {
    const start = nodeStart(node);
    const childEnd = descendantMaxEnd(node);
    if (start === null || childEnd === null) return 0;
    return Math.max(0, childEnd - start);
  }

  function barOffsetPct(node: SpanNode): number | null {
    if (!totalMs || t0 === null) return null;
    const start = nodeStart(node);
    if (start === null) return null;
    return Math.max(0, Math.min(100, ((start - t0) / totalMs) * 100));
  }

  function barOffsetMs(node: SpanNode): number {
    const start = nodeStart(node);
    if (start === null || t0 === null) return 0;
    return Math.max(0, start - t0);
  }

  function liveElapsedMs(node: SpanNode): number {
    const s = nodeStart(node);
    return s === null ? 0 : Math.max(0, nowTick - s);
  }

  function rowLabel(row: FlatRow): string {
    if (row.isRoot) return t("ks.root");
    if (row.isStage) return stageLabel(row.node.name);
    if (row.node.name === "postprocess.graph") return t("ks.proc.graph");
    const graphChunk = /^postprocess\.graph\.chunk\[(\d+)\]$/.exec(row.node.name);
    if (graphChunk) return `${t("ks.proc.graph")} #${Number(graphChunk[1]) + 1}`;
    return row.node.name;
  }

  function rowKindLabel(row: FlatRow): string {
    if (row.isRoot) return "root";
    if (row.isStage) return "stage";
    return row.node.kind || "span";
  }

  function scrollRowIntoView(key: string) {
    requestAnimationFrame(() => {
      const root = scrollRef.current;
      if (!root) return;
      const safeKey =
        typeof CSS !== "undefined" && CSS.escape ? CSS.escape(key) : key.replace(/"/g, '\\"');
      root
        .querySelector(`[data-span-key="${safeKey}"]`)
        ?.scrollIntoView({ block: "nearest", behavior: "smooth" });
    });
  }

  function toggleTree(row: FlatRow, ev?: ReactMouseEvent) {
    ev?.stopPropagation();
    if (!row.hasChildren) return;
    const wasExpanded = expandedRows.has(row.key);
    setExpandedRows((prev) => {
      const next = new Set(prev);
      if (next.has(row.key)) next.delete(row.key);
      else next.add(row.key);
      return next;
    });
    /* Record the manual toggle at every depth so the next poll's
     * auto-expand pass leaves this row alone. */
    userToggledRef.current = new Set(userToggledRef.current).add(row.key);
    /* Expanding via chevron also surfaces the detail panel. */
    if (!wasExpanded) selectRow(row);
  }

  function selectRow(row: FlatRow) {
    if (selectedSpanId === row.key) return;
    setSelectedSpanId(row.key);
    setDetailTab("overview");
    scrollRowIntoView(row.key);
  }

  /* ===== detail panel helpers ===== */

  function jsonExpandKey(section: string, key: string): string {
    return `${selectedSpanId || ""}::${section}::${key}`;
  }

  function toggleJsonKey(section: string, key: string) {
    const k = jsonExpandKey(section, key);
    setExpandedJsonKeys((prev) => {
      const next = new Set(prev);
      if (next.has(k)) next.delete(k);
      else next.add(k);
      return next;
    });
  }

  function isJsonExpanded(section: string, key: string): boolean {
    return expandedJsonKeys.has(jsonExpandKey(section, key));
  }

  function tabHasContent(tab: "input" | "output" | "metadata"): boolean {
    const node = selectedRow?.node;
    if (!node) return false;
    return hasContent(node[tab]);
  }

  /* Fall back to overview when the metadata tab loses its content. */
  useEffect(() => {
    if (detailTab === "metadata" && !tabHasContent("metadata")) setDetailTab("overview");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedSpanId, detailTab, selectedRow]);

  const traceMetadata = useMemo(() => {
    const m = data?.trace?.metadata;
    return hasContent(m) ? m : null;
  }, [data]);

  interface IdentityField {
    key: string;
    label: string;
    value: string;
    mono: boolean;
    copyable: boolean;
  }

  function identityFields(row: FlatRow): IdentityField[] {
    const out: IdentityField[] = [];
    const node = row.node;
    out.push({ key: "name", label: t("ks.detail.name"), value: rowLabel(row), mono: false, copyable: false });
    out.push({ key: "kind", label: t("ks.detail.kind"), value: rowKindLabel(row), mono: true, copyable: false });
    out.push({ key: "status", label: t("ks.detail.status"), value: localizedStatus(row.node.status), mono: false, copyable: false });
    if (row.isStage) {
      const idx = stages.findIndex((s) => s.name === row.node.name);
      if (idx >= 0) {
        out.push({ key: "stageIndex", label: t("ks.detail.stageOrder"), value: `${idx + 1} / ${stages.length}`, mono: true, copyable: false });
      }
    }
    if (row.hasChildren) {
      out.push({ key: "children", label: t("ks.detail.childCount"), value: String((row.node.children || []).length), mono: true, copyable: false });
    }
    if (node.span_id) out.push({ key: "span_id", label: "span_id", value: node.span_id, mono: true, copyable: true });
    if (node.parent_span_id) out.push({ key: "parent_span_id", label: "parent_span_id", value: node.parent_span_id, mono: true, copyable: true });
    if (data?.knowledge_id) out.push({ key: "knowledge_id", label: "knowledge_id", value: data.knowledge_id, mono: true, copyable: true });
    if (data?.current_attempt) out.push({ key: "attempt", label: t("ks.head.attempt"), value: `#${data.current_attempt}`, mono: true, copyable: false });
    return out;
  }

  const stageBreakdown = useMemo(() => {
    const total = totalMs || 1;
    return stages.map((s) => ({
      name: s.name,
      label: stageLabel(s.name),
      status: s.status,
      duration_ms: s.duration_ms,
      pct:
        typeof s.duration_ms === "number" && s.duration_ms > 0
          ? Math.min(100, (s.duration_ms / total) * 100)
          : 0,
    }));
  }, [stages, totalMs, stageLabel]);

  /* ===== header ===== */

  const attemptTabs = useMemo(() => {
    const latest = data?.latest_attempt || 0;
    if (latest <= 1) return [] as { n: number; status: string; active: boolean }[];
    const active = selectedAttempt ?? data?.attempt ?? latest;
    const out: { n: number; status: string; active: boolean }[] = [];
    for (let n = 1; n <= latest; n++) {
      out.push({ n, status: attemptStatuses.get(n) || "unknown", active: n === active });
    }
    return out;
  }, [data, selectedAttempt, attemptStatuses]);

  const viewingLatestAttempt = useMemo(() => {
    const latest = data?.latest_attempt || 0;
    if (latest <= 1) return true;
    const active = selectedAttempt ?? data?.attempt ?? latest;
    return active === latest;
  }, [data, selectedAttempt]);

  const headerStatus = useMemo(
    () =>
      resolveTimelineHeaderStatus({
        parseStatus: data?.parse_status,
        traceStatus: data?.trace?.status,
        isLatestAttempt: viewingLatestAttempt,
      }),
    [data, viewingLatestAttempt],
  );

  const headerStatusText = headerStatus ? localizedStatus(headerStatus) : "";

  const stagesStatDisplay = useMemo(() => {
    const total = stages.length;
    const completedCount = stages.filter((s) => s.status === "done" || s.status === "skipped").length;
    const inProgress = stages.some(
      (s) => s.status === "running" || s.status === "failed" || s.status === "pending",
    );
    if (inProgress) {
      return { label: t("ks.head.stagesProgress"), value: `${currentStageIndexOf(stages)}/${total}` };
    }
    return { label: t("ks.head.stagesDone"), value: `${completedCount}/${total}` };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [stages, t]);

  function currentStageIndexOf(list: SpanNode[]): number {
    const idx = list.findIndex((s) => s.status === "running" || s.status === "failed");
    if (idx >= 0) return idx + 1;
    const traversed = list.filter((s) => s.status === "done" || s.status === "skipped").length;
    return Math.min(traversed + 1, list.length);
  }

  const currentStageIndex = currentStageIndexOf(stages);

  const currentStageLabel = useMemo(() => {
    const running = stages.find((s) => s.status === "running");
    const failed = stages.find((s) => s.status === "failed");
    const target =
      failed || running || stages.find((s) => s.status === "pending") || stages[stages.length - 1];
    return target ? stageLabel(target.name) : "";
  }, [stages, stageLabel]);

  const postprocessTaskStats = useMemo(() => summarizePostprocessTasks(data?.trace), [data]);

  const headMetaParts = useMemo(() => {
    if (!data) return [] as string[];
    const parts: string[] = [t("ks.title")];
    if (totalMs > 0) parts.push(t("ks.total", { d: formatDuration(totalMs) }));
    const st = stagesStatDisplay;
    parts.push(`${st.label} ${st.value}`);
    const pp = postprocessTaskStats;
    if (pp.total > 0) {
      parts.push(t("ks.head.postprocessTasks", { running: pp.running, failed: pp.failed, completed: pp.completed }));
    }
    if (data.parse_status === "completed" && pp.running > 0) {
      parts.push(t("ks.head.completedWithActiveTrace", { n: pp.running }));
    }
    if (attemptTabs.length === 0 && data.current_attempt) {
      parts.push(t("ks.attempt", { n: data.current_attempt }));
    }
    if (lastFetchedAt && isLive) {
      let updated = formatRelativeTime(lastFetchedAt);
      if (!lastFetchOk) updated += ` (${t("ks.fetchFailedShort")})`;
      parts.push(`${t("ks.head.updated")} ${updated}`);
    }
    return parts;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [data, totalMs, stagesStatDisplay, postprocessTaskStats, attemptTabs, lastFetchedAt, isLive, lastFetchOk, nowTick, t]);

  function formatRelativeTime(ts: number): string {
    if (!ts) return "—";
    const sec = Math.max(0, Math.floor((nowTick - ts) / 1000));
    if (sec < 1) return t("ks.justNow");
    if (sec < 60) return t("ks.secondsAgo", { n: sec });
    return t("ks.minutesAgo", { n: Math.floor(sec / 60) });
  }

  /* Emit the one-line summary upstream (drives the doc panel's trace
   * entry button) whenever the inputs change. */
  const summaryRef = useRef(onSummary);
  useEffect(() => {
    summaryRef.current = onSummary;
  });
  useEffect(() => {
    summaryRef.current?.({
      totalMs,
      status: headerStatus,
      stageIndex: currentStageIndex,
      stageTotal: stages.length,
      stageLabel: currentStageLabel,
    });
  }, [totalMs, headerStatus, currentStageIndex, stages.length, currentStageLabel]);

  /* ===== actions ===== */

  const copyValue = useCallback(
    async (value: unknown, flashKey: string) => {
      const text = typeof value === "string" ? value : prettyJSON(value);
      const ok = await copyToClipboard(text);
      if (ok) {
        setCopiedKey(flashKey);
        setTimeout(() => setCopiedKey((k) => (k === flashKey ? null : k)), 1500);
      }
    },
    [],
  );

  const onRetry = async () => {
    if (!knowledgeId) return;
    try {
      await reparseKnowledge(knowledgeId);
      setSelectedAttempt(undefined);
      setAttemptStatuses(new Map());
      attemptStatusRef.current = new Set();
      setSelectedSpanId(null);
      await fetchSpans();
    } catch {
      /* surfaced via the unchanged tree */
    }
  };

  const onManualRefresh = async () => {
    if (refreshing || loading) return;
    await fetchSpans({ manual: true });
  };

  /* Mirrors the backend CancelKnowledgeParse gate. */
  const canCancelParse = isPollingStatus(data?.parse_status ?? parseStatus);

  const onCancelParseConfirm = async () => {
    if (cancelling || !knowledgeId) return;
    setCancelling(true);
    try {
      await cancelKnowledgeParse(knowledgeId);
      setNotice({ kind: "ok", text: t("doc.cancelParseSubmitted") });
      setConfirmCancel(false);
      await fetchSpans({ manual: true });
    } catch (e) {
      setNotice({ kind: "err", text: e instanceof Error && e.message ? e.message : t("doc.cancelParseFailed") });
    } finally {
      setCancelling(false);
    }
  };

  const onAttemptChange = (n: number) => {
    if (Number.isNaN(n)) return;
    setSelectedAttempt(n);
    setSelectedSpanId(null);
    /* New attempt: forget per-row toggles so auto-expand re-evaluates
     * cleanly against the new tree. */
    userToggledRef.current = new Set();
    setExpandedRows(new Set(["__root__"]));
    void fetchSpans();
  };

  /* ===== process config popup ===== */

  function formatParserRulesForCurrentFile(
    rules: Array<{ file_types?: string[]; engine?: string }>,
  ): string {
    if (currentFileType) {
      const matched = rules.find((rule) =>
        (rule.file_types || []).some((ft) => normalizeFileType(ft) === currentFileType),
      );
      if (matched?.engine) return `${currentFileType}→${matched.engine}`;
    }
    return rules.map((r) => `${(r.file_types || []).join("/")}→${r.engine}`).join(", ");
  }

  /* Human-readable summary of the per-upload parse overrides stored in
   * knowledge.metadata.process_overrides; empty → KB defaults. */
  const processConfigLines = useMemo<string[]>(() => {
    const o = processOverrides;
    const onOff = (v: boolean) => (v ? t("ks.proc.on") : t("ks.proc.off"));
    if (!o) return [t("ks.proc.kbDefault")];
    const lines: string[] = [];

    const cc = o.chunking_config;
    if (cc) {
      const parts: string[] = [];
      if (cc.chunk_size != null) parts.push(t("ks.proc.chunkSize", { n: cc.chunk_size }));
      if (cc.enable_parent_child != null) {
        parts.push(cc.enable_parent_child ? t("ks.proc.parentChildOn") : t("ks.proc.parentChildOff"));
      }
      if (parts.length) lines.push(`${t("ks.proc.chunking")}: ${parts.join(" · ")}`);
    }

    const rules = o.parser_engine_rules || cc?.parser_engine_rules;
    if (rules?.length) {
      lines.push(`${t("ks.proc.parser")}: ${formatParserRulesForCurrentFile(rules)}`);
    }

    const mm = o.vlm_config?.enabled ?? o.enable_multimodel;
    if (mm != null) lines.push(`${t("ks.proc.multimodal")}: ${onOff(mm)}`);

    if (o.asr_config?.enabled != null) lines.push(`${t("ks.proc.asr")}: ${onOff(o.asr_config.enabled)}`);

    const qg = o.question_generation_config;
    if (o.summary_enabled != null) lines.push(`${t("ks.proc.summary")}: ${onOff(o.summary_enabled)}`);
    if (qg?.enabled != null) {
      lines.push(`${t("ks.proc.question")}: ${qg.enabled ? t("ks.proc.questionOn", { n: qg.question_count ?? 3 }) : t("ks.proc.off")}`);
    }

    const graph = o.graph_enabled ?? o.extract_config?.enabled;
    if (graph != null) lines.push(`${t("ks.proc.graph")}: ${onOff(graph)}`);

    return lines.length ? lines : [t("ks.proc.kbDefault")];
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [processOverrides, currentFileType, t]);

  /* ===== render ===== */

  const iconBtn =
    "inline-flex h-[26px] w-[26px] items-center justify-center rounded-[6px] text-muted-soft transition-colors hover:bg-surface-strong hover:text-ink disabled:cursor-not-allowed disabled:opacity-40";

  if (compact) {
    return (
      <div className="max-w-[320px]">
        <div className="flex items-center gap-1.5">
          {stages.map((s) => (
            <span
              key={s.name}
              title={`${stageLabel(s.name)} · ${localizedStatus(s.status)}`}
              className={`inline-block h-2 w-2 rounded-full ${dotCls(s.status)}`}
            />
          ))}
        </div>
        <div className="caption mt-1 truncate text-muted">
          {totalMs > 0 ? (
            t("ks.totalDuration", { d: formatDuration(totalMs) })
          ) : (
            <span>
              {t("ks.title")}：
              <span className="font-semibold text-ink">
                {currentStageIndex}/{stages.length}
              </span>
              {" · "}
              {currentStageLabel}
            </span>
          )}
        </div>
      </div>
    );
  }

  return (
    <div className="flex h-full min-h-0 min-w-0 w-full flex-col overflow-hidden bg-surface-card text-body">
      {/* ============== HEADER ============== */}
      <div className="shrink-0 border-b border-hairline bg-surface-card px-5 pb-2.5 pt-3.5">
        <div className="flex min-w-0 items-center gap-2">
          <h2 className="min-w-0 flex-1 truncate text-[15px] font-semibold leading-[1.35] text-ink" title={docTitle || t("ks.title")}>
            {docTitle || t("ks.title")}
          </h2>
          {data && headerStatusText && (
            <span className={`shrink-0 rounded-md px-2 py-0.5 text-[11px] font-medium ${CHIP_CLS[headerStatus] ?? "bg-surface-strong text-muted"}`}>
              {headerStatusText}
            </span>
          )}
          {isLive && (
            <span
              className="inline-flex shrink-0 items-center gap-[5px] rounded-md bg-amber-500/15 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-[0.06em] text-amber-600"
              title={t("ks.liveTooltip")}
            >
              <span className="kp-live-pulse h-1.5 w-1.5 rounded-full bg-amber-500" />
              <span className="font-mono">{t("ks.live")}</span>
            </span>
          )}
          <div className="ml-auto flex shrink-0 items-center gap-1">
            {/* Parse config popover (hover) */}
            <div className="group relative">
              <button
                type="button"
                className={iconBtn}
                title={t("ks.proc.title")}
                aria-label={t("ks.proc.title")}
              >
                <IconInfoCircle className="h-3.5 w-3.5" />
              </button>
              <div className="pointer-events-none absolute right-0 top-full z-20 mt-1 hidden w-max max-w-[340px] rounded-lg border border-hairline bg-surface-card p-3 shadow-[0_8px_30px_rgba(0,0,0,0.12)] group-hover:block">
                <div className="mb-1 text-[12px] font-semibold text-ink">{t("ks.proc.title")}</div>
                {processConfigLines.map((line, i) => (
                  <div key={i} className="caption text-muted">
                    {line}
                  </div>
                ))}
              </div>
            </div>
            <button
              type="button"
              className={iconBtn}
              disabled={loading || refreshing}
              title={isLive ? t("ks.autoRefreshOn") : t("ks.refresh")}
              aria-label={isLive ? t("ks.autoRefreshOn") : t("ks.refresh")}
              onClick={() => void onManualRefresh()}
            >
              <IconRefresh
                className={`h-3.5 w-3.5 ${
                  refreshing
                    ? "animate-spin"
                    : isLive
                      ? "animate-[spin_4s_linear_infinite] text-amber-500"
                      : ""
                }`}
              />
            </button>
            {canCancelParse && (
              <button
                type="button"
                className={`${iconBtn} hover:bg-error/10 hover:text-error`}
                disabled={cancelling}
                title={t("doc.cancelParse")}
                aria-label={t("doc.cancelParse")}
                onClick={() => setConfirmCancel((v) => !v)}
              >
                {cancelling ? (
                  <span className="h-3.5 w-3.5 animate-spin rounded-full border-2 border-hairline-strong border-t-error" />
                ) : (
                  <IconMinusCircle className="h-[15px] w-[15px]" />
                )}
              </button>
            )}
            {data?.parse_status === "failed" && (
              <button
                type="button"
                className="btn btn-outline btn-sm h-7"
                onClick={() => void onRetry()}
              >
                <IconRefresh className="h-3.5 w-3.5" />
                {t("ks.retry")}
              </button>
            )}
            {showClose && (
              <button
                type="button"
                className={iconBtn}
                aria-label={t("ks.close")}
                title={t("ks.close")}
                onClick={onClose}
              >
                <IconClose className="h-4 w-4" />
              </button>
            )}
          </div>
        </div>

        {confirmCancel && canCancelParse && (
          <div className="mt-2 flex items-center gap-2 rounded-lg border border-amber-500/40 bg-amber-500/10 px-3 py-2">
            <p className="caption min-w-0 flex-1 text-body">
              {t("doc.cancelParseConfirm", { fileName: docTitle || knowledgeId })}
            </p>
            <button type="button" className="btn btn-outline btn-sm h-7" onClick={() => setConfirmCancel(false)} disabled={cancelling}>
              {t("common.cancel")}
            </button>
            <button
              type="button"
              className="btn btn-primary btn-sm h-7 border-error bg-error hover:border-error hover:bg-error"
              onClick={() => void onCancelParseConfirm()}
              disabled={cancelling}
            >
              {t("doc.cancelParse")}
            </button>
          </div>
        )}
        {notice && (
          <p className={`caption mt-1.5 ${notice.kind === "err" ? "text-error" : "text-success"}`}>
            {notice.text}
          </p>
        )}

        {headMetaParts.length > 0 && (
          <p className="caption mb-0 mt-2 break-words text-muted">
            {headMetaParts.map((part, idx) => (
              <span key={idx}>
                {idx > 0 && <span className="mx-1.5 text-muted-soft">·</span>}
                {part}
              </span>
            ))}
          </p>
        )}

        {attemptTabs.length > 0 && (
          <div className="mt-2.5 flex gap-1.5 overflow-x-auto pb-0.5">
            {attemptTabs.map((tab) => (
              <button
                key={tab.n}
                type="button"
                onClick={() => onAttemptChange(tab.n)}
                className={`inline-flex items-center gap-[5px] whitespace-nowrap rounded-md border px-2.5 py-1 caption transition-colors ${
                  tab.active
                    ? "border-ink bg-ink text-on-primary"
                    : "border-hairline-strong bg-surface-card text-muted hover:bg-surface-strong hover:text-ink"
                }`}
              >
                <span className="font-mono text-[11px] font-semibold">#{tab.n}</span>
                <span className={`text-[9px] leading-none ${tab.active ? "text-on-primary" : attemptGlyphCls(tab.status)}`}>
                  {attemptGlyph(tab.status)}
                </span>
              </button>
            ))}
          </div>
        )}

        {data?.last_error && data.parse_status === "failed" && (
          <div className="mt-2.5 flex overflow-hidden rounded-lg border border-error/30 bg-error/10" role="alert">
            <div className="w-[3px] shrink-0 bg-error" />
            <div className="min-w-0 flex-1 px-3.5 py-2.5">
              <div className="mb-1 flex items-center gap-2">
                <span className="inline-flex h-4 w-4 shrink-0 items-center justify-center rounded-full bg-error text-[11px] font-bold text-white">
                  !
                </span>
                <span className="caption font-semibold text-error">{data.last_error.error_code || t("ks.detail.error")}</span>
                {data.last_error.error_code && (
                  <span className="ml-auto rounded bg-error px-1.5 py-px font-mono text-[10px] text-white">
                    {data.last_error.error_code}
                  </span>
                )}
              </div>
              <div className="caption mb-1 text-muted">{t("ks.errUnknownSuggestion")}</div>
              {data.last_error.error_message && (
                <div className="whitespace-pre-wrap break-words font-mono text-[11px] text-muted-soft">
                  {data.last_error.error_message}
                </div>
              )}
            </div>
          </div>
        )}
      </div>

      {/* ============== BODY (Waterfall) ============== */}
      <div className="flex min-h-0 flex-1 flex-col overflow-hidden bg-surface-card">
        {loading && !data ? (
          <div className="flex flex-1 items-center justify-center gap-2 px-5 py-14 text-muted-soft">
            <span className="h-4 w-4 animate-spin rounded-full border-2 border-hairline-strong border-t-ink" />
          </div>
        ) : !data ? (
          <div className="flex flex-1 items-center justify-center px-5 py-14 text-muted-soft">
            <span>{t("ks.noActivity")}</span>
          </div>
        ) : (
          <>
            {/* Ruler stays outside the scroll region so the time axis
             * never scrolls away. */}
            {showRuler && (
              <div className="grid h-6 shrink-0 grid-cols-[minmax(220px,42%)_64px_1fr] items-end border-b border-dashed border-hairline px-5 pb-1.5 pt-3">
                <div />
                <div />
                <div className="relative mr-4 h-full">
                  {rulerTicks.map((tick, i) => (
                    <span
                      key={i}
                      className={`absolute bottom-0 flex flex-col items-center text-[10px] text-muted-soft ${
                        i === 0
                          ? "translate-x-0 items-start"
                          : i === rulerTicks.length - 1
                            ? "-translate-x-full items-end"
                            : "-translate-x-1/2"
                      }`}
                      style={{ left: tick.left }}
                    >
                      <span className="h-[5px] w-px bg-hairline-strong" />
                      <span className="mt-0.5 font-mono tracking-[0.02em]">{tick.label}</span>
                    </span>
                  ))}
                </div>
              </div>
            )}

            <div ref={scrollRef} className="min-h-0 flex-1 overflow-auto pb-4">
              <div className="flex flex-col">
                {flatRows.map((row) => {
                  const wrap = wrapStyle(row.node);
                  const offsetPct = barOffsetPct(row.node);
                  const offsetMs = barOffsetMs(row.node);
                  return (
                    <div
                      key={row.key}
                      data-span-key={row.key}
                      title={row.hasChildren && !row.isRoot ? t("ks.rowSelectHint") : undefined}
                      onClick={() => selectRow(row)}
                      className={`group/kp relative grid h-8 cursor-pointer grid-cols-[minmax(220px,42%)_64px_1fr] items-center px-5 transition-colors ${
                        selectedSpanId === row.key
                          ? "bg-surface-strong"
                          : row.isStage
                            ? "bg-surface-strong/55 hover:bg-surface-strong"
                            : !row.isRoot
                              ? "hover:bg-surface-strong"
                              : "hover:bg-surface-strong"
                      }`}
                    >
                      {selectedSpanId === row.key && (
                        <span className="absolute bottom-1 left-0 top-1 w-0.5 rounded-r bg-ink" />
                      )}

                      {/* name cell */}
                      <div className="min-w-0">
                        <div className="flex min-w-0 items-center gap-[7px]" style={{ paddingLeft: row.depth * 16 }}>
                          {row.hasChildren && !row.isRoot ? (
                            <button
                              type="button"
                              aria-expanded={expandedRows.has(row.key)}
                              aria-label={expandedRows.has(row.key) ? t("ks.collapseBranch") : t("ks.expandBranch")}
                              onClick={(e) => toggleTree(row, e)}
                              className="-my-[3px] inline-flex h-[22px] w-[22px] shrink-0 items-center justify-center rounded-md text-muted-soft transition-colors hover:bg-surface-card hover:text-ink"
                            >
                              {expandedRows.has(row.key) ? (
                                <IconChevronDown className="h-3.5 w-3.5" />
                              ) : (
                                <IconChevronRight className="h-3.5 w-3.5" />
                              )}
                            </button>
                          ) : (
                            <span className="-my-[3px] inline-block h-[22px] w-[22px] shrink-0" />
                          )}
                          <span className={`h-[7px] w-[7px] shrink-0 rounded-full ${dotCls(row.node.status, isPlaceholder(row.node))}`} />
                          <span
                            className={`truncate text-[13px] text-ink ${
                              row.isRoot
                                ? "text-[14px] font-semibold"
                                : !row.isStage
                                  ? "font-mono text-[11px]"
                                  : ""
                            }`}
                          >
                            {rowLabel(row)}
                          </span>
                          <span className="ml-auto shrink-0 pl-2 font-mono text-[10px] uppercase tracking-[0.5px] text-muted-soft">
                            {rowKindLabel(row)}
                          </span>
                        </div>
                      </div>

                      {/* duration cell */}
                      <div className="pr-3 text-right font-mono text-[11px] tracking-[0.02em] text-muted">
                        {row.node.status === "running" ? (
                          <span className="font-semibold text-amber-600">
                            {formatDuration(liveElapsedMs(row.node))}
                          </span>
                        ) : (
                          formatSpanDuration(row.node)
                        )}
                      </div>

                      {/* bar cell */}
                      <div className="relative mr-4 h-8">
                        {nowMarkerPct !== null && row.isRoot && (
                          <span
                            className="pointer-events-none absolute bottom-1 top-1 z-[1] w-px bg-amber-500/65 transition-[left] duration-1000 ease-linear"
                            style={{ left: `${nowMarkerPct}%` }}
                          >
                            <span className="kp-live-pulse absolute -left-[3px] -top-0.5 h-[7px] w-[7px] rounded-full bg-amber-500" />
                          </span>
                        )}
                        {isPlaceholder(row.node) ? (
                          <div className="absolute right-1 top-[13px] h-1.5 w-3.5 rounded-sm border border-dashed border-hairline-strong" />
                        ) : (
                          <>
                            {wrap && (
                              <div
                                className={`group/wrap absolute top-[9px] z-[1] h-3.5 min-w-[4px] rounded-sm border border-dashed transition-[left,width] duration-700 ${WRAP_CLS[row.node.status] ?? "border-hairline-strong"}`}
                                style={wrap}
                              >
                                <span className="pointer-events-none absolute -top-[26px] left-1/2 z-10 flex -translate-x-1/2 items-center gap-1 whitespace-nowrap rounded-md bg-ink px-2 py-1 text-[11px] text-on-primary opacity-0 transition-opacity group-hover/wrap:opacity-100">
                                  <span className="font-medium">{rowLabel(row)}</span>
                                  <span className="opacity-60">·</span>
                                  <span className="font-mono">{formatDuration(wrapDurationMs(row.node))}</span>
                                  <span className="opacity-60">·</span>
                                  <span>{t("ks.detail.includingChildren")}</span>
                                </span>
                              </div>
                            )}
                            <div
                              className={`group/bar absolute top-3 z-[2] h-2 min-w-[2px] rounded-sm transition-[left,width,filter] duration-700 ${barCls(row.node.status)}`}
                              style={barStyle(row.node)}
                            >
                              <span className={`pointer-events-none absolute left-1/2 z-10 flex -translate-x-1/2 items-center gap-1 whitespace-nowrap rounded-md bg-ink px-2 py-1 text-[11px] text-on-primary opacity-0 transition-opacity group-hover/bar:opacity-100 ${row.isRoot ? "top-[calc(100%+8px)]" : "-top-[26px]"}`}>
                                <span className="font-medium">{rowLabel(row)}</span>
                                <span className="opacity-60">·</span>
                                <span className="font-mono">
                                  {row.node.status === "running"
                                    ? formatDuration(liveElapsedMs(row.node))
                                    : formatSpanDuration(row.node)}
                                </span>
                                <span className="opacity-60">·</span>
                                <span>{localizedStatus(row.node.status)}</span>
                              </span>
                            </div>
                            {offsetPct !== null && offsetMs > 0 && (
                              <span
                                className="absolute bottom-0 overflow-hidden text-ellipsis whitespace-nowrap text-center font-mono text-[9px] leading-[11px] text-muted-soft opacity-0 transition-opacity group-hover/kp:opacity-100"
                                style={{
                                  left: `clamp(0px, calc(${offsetPct}% - 45px), max(0px, calc(100% - 90px)))`,
                                  width: "min(90px, 100%)",
                                }}
                                title={`+${formatDuration(offsetMs)}`}
                              >
                                +{formatDuration(offsetMs)}
                              </span>
                            )}
                          </>
                        )}
                      </div>
                    </div>
                  );
                })}
              </div>
            </div>
          </>
        )}
      </div>

      {/* ============== DETAIL PANEL ============== */}
      <div
        className={`flex shrink-0 flex-col overflow-hidden border-t bg-surface-card transition-[height] duration-200 ${
          detailOpen ? "h-1/2 min-h-[320px] border-hairline" : "h-0 border-transparent"
        }`}
      >
        {selectedRow && (
          <>
            <div className="flex shrink-0 items-center justify-between gap-2 border-b border-hairline px-5 pb-2.5 pt-3">
              <div className="flex min-w-0 items-center gap-2">
                <span className={`h-2 w-2 shrink-0 rounded-full ${dotCls(selectedRow.node.status)}`} />
                <span className="truncate text-[14px] font-semibold text-ink">{rowLabel(selectedRow)}</span>
                <span className="shrink-0 font-mono text-[10px] uppercase tracking-[0.5px] text-muted-soft">
                  {rowKindLabel(selectedRow)}
                </span>
                <span className={`inline-flex shrink-0 items-center rounded-md px-2 py-px text-[11px] font-medium ${CHIP_CLS[selectedRow.node.status] ?? "bg-surface-strong text-muted"}`}>
                  {localizedStatus(selectedRow.node.status)}
                </span>
              </div>
              <div className="flex items-center gap-1">
                <button
                  type="button"
                  className={iconBtn}
                  title={t("ks.copyDetails")}
                  onClick={(e) => {
                    e.stopPropagation();
                    void copyValue(selectedRow.node, "detail");
                  }}
                >
                  {copiedKey === "detail" ? (
                    <IconCheck className="h-4 w-4 text-success" />
                  ) : (
                    <IconCopy className="h-4 w-4" />
                  )}
                </button>
                <button type="button" className={iconBtn} title={t("ks.close")} onClick={() => setSelectedSpanId(null)}>
                  <IconClose className="h-4 w-4" />
                </button>
              </div>
            </div>

            <div className="flex shrink-0 border-b border-hairline bg-surface-card px-5">
              {(
                [
                  ["overview", t("ks.tab.overview"), true],
                  ["input", t("ks.detail.input"), tabHasContent("input")],
                  ["output", t("ks.detail.output"), tabHasContent("output")],
                  ["metadata", t("ks.detail.metadata"), tabHasContent("metadata")],
                  ["raw", t("ks.tab.raw"), true],
                ] as const
              )
                .filter(([key, , has]) => key !== "metadata" || has)
                .map(([key, label, has]) => (
                  <button
                    key={key}
                    type="button"
                    onClick={() => setDetailTab(key)}
                    className={`relative px-3.5 pb-2.5 pt-[9px] text-[13.5px] transition-colors ${
                      detailTab === key
                        ? "font-semibold text-ink after:absolute after:inset-x-3.5 after:-bottom-px after:h-0.5 after:rounded-t after:bg-ink"
                        : has
                          ? "text-muted hover:text-ink"
                          : "text-muted-soft hover:text-ink"
                    }`}
                  >
                    {label}
                  </button>
                ))}
            </div>

            <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-5 py-4">
              {detailTab === "overview" && (
                <>
                  {/* Timing */}
                  <div className="flex flex-col gap-2">
                    <div className="text-[11px] font-medium uppercase tracking-[0.5px] text-muted">
                      {t("ks.detail.timing")}
                    </div>
                    <KvTable>
                      <KvRow k={t("ks.detail.started")} v={formatTime(selectedRow.node.started_at)} mono />
                      <KvRow
                        k={t("ks.detail.finished")}
                        mono
                        v={
                          selectedRow.node.status === "running" ? (
                            <span className="font-sans text-[11px] italic text-amber-600">
                              {t("ks.detail.inProgress")}
                            </span>
                          ) : (
                            formatTime(selectedRow.node.finished_at)
                          )
                        }
                      />
                      <KvRow
                        k={t("ks.detail.duration")}
                        mono
                        v={
                          selectedRow.node.status === "running" ? (
                            <>
                              {formatDuration(liveElapsedMs(selectedRow.node))}
                              <span className="ml-1.5 inline-block rounded bg-amber-500/15 px-1.5 text-[9px] font-semibold uppercase tracking-[0.5px] text-amber-600">
                                {t("ks.detail.elapsed")}
                              </span>
                            </>
                          ) : (
                            formatSpanDuration(selectedRow.node)
                          )
                        }
                      />
                      {!selectedRow.isRoot && barOffsetMs(selectedRow.node) > 0 && (
                        <KvRow k={t("ks.detail.offset")} mono v={`+${formatDuration(barOffsetMs(selectedRow.node))}`} />
                      )}
                    </KvTable>
                  </div>

                  {/* Identity / lineage */}
                  <div className="flex flex-col gap-2">
                    <div className="text-[11px] font-medium uppercase tracking-[0.5px] text-muted">
                      {t("ks.detail.identity")}
                    </div>
                    <KvTable>
                      {identityFields(selectedRow).map((entry) => (
                        <KvRow
                          key={entry.key}
                          k={entry.label}
                          mono={entry.mono}
                          truncate={entry.copyable}
                          v={
                            <>
                              <span className={entry.copyable ? "min-w-0 flex-1 truncate" : ""}>{entry.value}</span>
                              {entry.copyable && (
                                <button
                                  type="button"
                                  title={t("ks.copy")}
                                  onClick={(e) => {
                                    e.stopPropagation();
                                    void copyValue(entry.value, `id:${entry.key}`);
                                  }}
                                  className="inline-flex h-[22px] w-[22px] shrink-0 items-center justify-center rounded text-muted-soft hover:bg-surface-strong hover:text-ink"
                                >
                                  {copiedKey === `id:${entry.key}` ? (
                                    <IconCheck className="h-3.5 w-3.5 text-success" />
                                  ) : (
                                    <IconCopy className="h-3.5 w-3.5" />
                                  )}
                                </button>
                              )}
                            </>
                          }
                        />
                      ))}
                    </KvTable>
                  </div>

                  {traceMetadata != null && (
                    <div className="flex flex-col gap-2">
                      <div className="text-[11px] font-medium uppercase tracking-[0.5px] text-muted">
                        {t("ks.detail.traceMetadata")}
                      </div>
                      <p className="caption m-0 text-muted-soft">{t("ks.detail.metadataHint")}</p>
                      <KvTable>
                        {buildKvEntries(traceMetadata).map((entry) => (
                          <KvRow key={entry.key} k={entry.key} monoKey v={entry.display} multiline />
                        ))}
                      </KvTable>
                    </div>
                  )}

                  {/* Stage breakdown (root only) */}
                  {selectedRow.isRoot && (
                    <div className="flex flex-col gap-2">
                      <div className="text-[11px] font-medium uppercase tracking-[0.5px] text-muted">
                        {t("ks.detail.stageBreakdown")}
                      </div>
                      <div className="flex flex-col gap-1.5 rounded-lg border border-hairline bg-surface-card px-3 py-2.5">
                        {stageBreakdown.map((s) => (
                          <div key={s.name} className="grid grid-cols-[110px_1fr_64px] items-center gap-2.5 text-[13px]">
                            <span className="inline-flex items-center gap-1.5 text-ink">
                              <span className={`h-[7px] w-[7px] rounded-full ${dotCls(s.status)}`} />
                              {s.label}
                            </span>
                            <div className="relative h-1.5 overflow-hidden rounded-sm bg-surface-strong">
                              {s.status !== "pending" && (
                                <div
                                  className={`absolute inset-y-0 left-0 rounded-sm transition-[width] duration-700 ${barCls(s.status)}`}
                                  style={{ width: `${s.pct}%` }}
                                />
                              )}
                            </div>
                            <span className="text-right font-mono text-[11px] text-muted">
                              {s.status === "skipped" || s.status === "pending" ? "—" : formatDuration(s.duration_ms)}
                            </span>
                          </div>
                        ))}
                      </div>
                    </div>
                  )}

                  {/* Error */}
                  {(selectedRow.node.status === "failed" || selectedRow.node.status === "cancelled") &&
                    (selectedRow.node.error_code || selectedRow.node.error_message) && (
                      <div className="flex flex-col gap-2 rounded-lg border border-error/30 bg-error/10 px-3 py-2.5">
                        <div className="flex items-center gap-2">
                          <span className="inline-flex h-4 w-4 shrink-0 items-center justify-center rounded-full bg-error text-[11px] font-bold text-white">
                            !
                          </span>
                          <span className="caption font-semibold text-error">
                            {selectedRow.node.error_code || t("ks.detail.error")}
                          </span>
                          {selectedRow.node.error_code && (
                            <span className="ml-auto rounded bg-error px-1.5 py-px font-mono text-[10px] text-white">
                              {selectedRow.node.error_code}
                            </span>
                          )}
                        </div>
                        {selectedRow.node.error_message && (
                          <pre className="m-0 max-h-40 overflow-auto whitespace-pre-wrap break-words rounded-md border border-hairline bg-surface-card px-2.5 py-2 font-mono text-[11px] text-muted">
                            {selectedRow.node.error_message}
                          </pre>
                        )}
                      </div>
                    )}

                  {isPlaceholder(selectedRow.node) && (
                    <div className="caption rounded-lg border-l-2 border-hairline-strong bg-surface-strong px-3 py-2.5 text-muted">
                      {t("ks.detail.placeholderHint")}
                    </div>
                  )}
                </>
              )}

              {(detailTab === "input" || detailTab === "output" || detailTab === "metadata") && (
                <>
                  {!tabHasContent(detailTab) ? (
                    <div className="flex items-center justify-center py-12 text-muted-soft">
                      <span>{detailTab === "metadata" ? t("ks.detail.metadataEmpty") : t("ks.detail.empty")}</span>
                    </div>
                  ) : (
                    <div className="flex flex-col gap-2">
                      <div className="flex items-center justify-between gap-2">
                        <span className="text-[11px] font-medium uppercase tracking-[0.5px] text-muted">
                          {t(`ks.detail.${detailTab}` as LocaleKey)}
                        </span>
                        <button
                          type="button"
                          className="inline-flex items-center gap-1 rounded-md border border-hairline-strong bg-surface-card px-2 py-[3px] text-[11px] text-muted transition-colors hover:bg-ink hover:text-on-primary"
                          onClick={() => void copyValue(selectedRow.node[detailTab], `sec:${detailTab}`)}
                        >
                          {copiedKey === `sec:${detailTab}` ? (
                            <IconCheck className="h-3.5 w-3.5 text-success" />
                          ) : (
                            <IconCopy className="h-3.5 w-3.5" />
                          )}
                          <span>{copiedKey === `sec:${detailTab}` ? t("ks.copied") : t("ks.copy")}</span>
                        </button>
                      </div>

                      {isObjectWithKeys(selectedRow.node[detailTab]) ? (
                        <KvTable>
                          {buildKvEntries(selectedRow.node[detailTab]).map((entry) => (
                            <KvRow key={entry.key} k={entry.key} monoKey multiline v={
                              entry.kind === "bool" ? (
                                <span className={`font-mono text-[11px] font-medium ${entry.raw ? "text-success" : "text-error"}`}>
                                  {entry.display}
                                </span>
                              ) : entry.kind === "scalar" ? (
                                <span className="text-[13px]">{entry.display}</span>
                              ) : entry.defaultExpanded ? (
                                <div className="flex min-w-0 flex-col gap-1">
                                  <span className="font-mono text-[11px] text-muted-soft">{entry.display}</span>
                                  <pre className="m-0 max-h-[360px] overflow-auto whitespace-pre-wrap break-words rounded-lg border border-hairline bg-surface-strong px-3 py-2.5 font-mono text-[11px] leading-[1.6] text-ink">
                                    {prettyJSON(entry.raw)}
                                  </pre>
                                </div>
                              ) : (
                                <div className="flex min-w-0 flex-col gap-1.5">
                                  <button
                                    type="button"
                                    className="flex flex-wrap items-baseline gap-2 text-left"
                                    onClick={(e) => {
                                      e.stopPropagation();
                                      toggleJsonKey(detailTab, entry.key);
                                    }}
                                  >
                                    <span className="font-mono text-[11px] text-muted">{entry.display}</span>
                                    <span className="text-[11px] font-medium text-ink underline-offset-2 hover:underline">
                                      {isJsonExpanded(detailTab, entry.key)
                                        ? t("ks.detail.hideJson")
                                        : t("ks.detail.showJson")}
                                    </span>
                                  </button>
                                  {isJsonExpanded(detailTab, entry.key) && (
                                    <pre className="m-0 max-h-[360px] overflow-auto whitespace-pre-wrap break-words rounded-lg border border-hairline bg-surface-strong px-3 py-2.5 font-mono text-[11px] leading-[1.6] text-ink">
                                      {prettyJSON(entry.raw)}
                                    </pre>
                                  )}
                                </div>
                              )
                            } />
                          ))}
                        </KvTable>
                      ) : (
                        <pre className="m-0 max-h-[360px] overflow-auto whitespace-pre-wrap break-words rounded-lg border border-hairline bg-surface-strong px-3 py-2.5 font-mono text-[11px] leading-[1.6] text-ink">
                          {prettyJSON(selectedRow.node[detailTab])}
                        </pre>
                      )}
                    </div>
                  )}
                </>
              )}

              {detailTab === "raw" && (
                <div className="flex flex-col gap-2">
                  <div className="flex items-center justify-between gap-2">
                    <span className="text-[11px] font-medium uppercase tracking-[0.5px] text-muted">
                      {t("ks.tab.raw")}
                    </span>
                    <button
                      type="button"
                      className="inline-flex items-center gap-1 rounded-md border border-hairline-strong bg-surface-card px-2 py-[3px] text-[11px] text-muted transition-colors hover:bg-ink hover:text-on-primary"
                      onClick={() => void copyValue(selectedRow.node, "raw")}
                    >
                      {copiedKey === "raw" ? (
                        <IconCheck className="h-3.5 w-3.5 text-success" />
                      ) : (
                        <IconCopy className="h-3.5 w-3.5" />
                      )}
                      <span>{copiedKey === "raw" ? t("ks.copied") : t("ks.copy")}</span>
                    </button>
                  </div>
                  <pre className="m-0 max-h-[480px] overflow-auto whitespace-pre-wrap break-words rounded-lg border border-hairline bg-surface-strong px-3 py-2.5 font-mono text-[11px] leading-[1.6] text-ink">
                    {prettyJSON(selectedRow.node)}
                  </pre>
                </div>
              )}
            </div>
          </>
        )}
      </div>
    </div>
  );
}

/* ===== shared bits ===== */

function attemptGlyph(status: string): string {
  switch (status) {
    case "done":
      return "✓";
    case "failed":
      return "✗";
    case "running":
    case "pending":
    case "processing":
      return "●";
    default:
      return "–";
  }
}

function attemptGlyphCls(status: string): string {
  switch (status) {
    case "done":
      return "text-success";
    case "failed":
      return "text-error";
    case "running":
    case "pending":
    case "processing":
      return "text-amber-500 kp-live-pulse";
    default:
      return "text-muted-soft";
  }
}

function KvTable({ children }: { children: ReactNode }) {
  return (
    <div className="flex flex-col overflow-hidden rounded-lg border border-hairline bg-surface-card">
      {children}
    </div>
  );
}

function KvRow({
  k,
  v,
  mono = false,
  monoKey = false,
  multiline = false,
  truncate = false,
}: {
  k: string;
  v: ReactNode;
  mono?: boolean;
  monoKey?: boolean;
  multiline?: boolean;
  truncate?: boolean;
}) {
  return (
    <div
      className={`grid grid-cols-[130px_1fr] gap-3 px-3 py-2 text-[13px] [&+&]:border-t [&+&]:border-hairline-soft ${
        multiline ? "items-start" : "items-center"
      }`}
    >
      <span className={`truncate text-[11px] font-medium text-muted ${monoKey ? "font-mono" : ""}`}>{k}</span>
      <span
        className={`inline-flex min-w-0 items-center gap-1.5 break-words text-ink ${
          mono ? "font-mono text-[11px]" : ""
        } ${multiline ? "flex-col items-stretch gap-1" : ""} ${truncate ? "overflow-hidden" : ""}`}
      >
        {v}
      </span>
    </div>
  );
}

/**
 * Secondary drawer hosting the full waterfall — port of the resizable
 * t-drawer used by doc-content.vue. Owns its own backdrop, Escape
 * handling and left-edge drag resize so it stacks above the doc panel.
 */
export function ProcessingTimelineDrawer({
  open,
  knowledgeId,
  parseStatus,
  docTitle,
  onClose,
}: {
  open: boolean;
  knowledgeId: string;
  parseStatus?: string;
  docTitle?: string;
  onClose: () => void;
}) {
  const { t } = useT();
  const [width, setWidth] = useState(() =>
    typeof window === "undefined"
      ? 860
      : Math.round(Math.min(1100, Math.max(640, window.innerWidth * 0.62))),
  );
  const [dragging, setDragging] = useState(false);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  const startResize = useCallback((e: ReactPointerEvent) => {
    e.preventDefault();
    setDragging(true);
    const onMove = (ev: PointerEvent) => {
      /* Drawer is right-anchored: width grows as the cursor moves left. */
      const w = window.innerWidth - ev.clientX;
      setWidth(Math.round(Math.min(Math.max(480, w), window.innerWidth - 48)));
    };
    const onUp = () => {
      setDragging(false);
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
  }, []);

  if (!open) return null;

  return (
    <>
      <div
        className="fixed inset-0 z-[55] bg-ink/10"
        onClick={onClose}
        aria-hidden="true"
      />
      <aside
        role="dialog"
        aria-label={t("ks.title")}
        className={`fixed right-0 top-0 z-[60] flex h-full max-w-[96vw] flex-col border-l border-hairline bg-surface-card shadow-[0_4px_24px_rgba(0,0,0,0.12)] ${
          dragging ? "" : "transition-transform duration-200 ease-out"
        }`}
        style={{ width }}
      >
        {/* left-edge resize handle */}
        <div
          role="separator"
          aria-orientation="vertical"
          aria-label={t("ks.resizeDrawer")}
          title={t("ks.resizeDrawer")}
          onPointerDown={startResize}
          className="absolute inset-y-0 -left-1.5 z-10 w-3 cursor-ew-resize"
        >
          <div className="mx-auto h-full w-px bg-transparent transition-colors hover:bg-ink/30" />
        </div>
        <ProcessingTimeline
          knowledgeId={knowledgeId}
          parseStatus={parseStatus}
          docTitle={docTitle}
          showClose
          onClose={onClose}
        />
      </aside>
    </>
  );
}
