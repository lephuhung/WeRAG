"use client";

import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type MouseEvent as ReactMouseEvent,
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
} from "@/lib/knowledge-trace";
import { copyToClipboard } from "@/lib/clipboard";
import { shouldApplyResponse } from "@/lib/request-identity";
import { TimelineRequestCoordinator } from "@/lib/timeline-request-coordinator";
import { useT } from "@/lib/i18n";
import { STAGES, dotCls } from "@/components/knowledge/trace-status";
import {
  collectEnds,
  collectStarts,
  formatDuration,
  getFileTypeFromName,
  hasContent,
  localizedStatus as localizedStatusFor,
  nodeEnd,
  nodeStart,
  normalizeFileType,
  parseTime,
  prettyJSON,
  rowKey,
  stageLabelFor,
  type FlatRow,
  type SpanNode,
} from "./processing-timeline/timeline-helpers";
import {
  TimelineHeader,
  type TimelineHeaderView,
  type TimelineNotice,
} from "./processing-timeline/timeline-header";
import { TimelineRows, type TimelineRowsView } from "./processing-timeline/timeline-rows";
import {
  TimelineDetail,
  type TimelineDetailTab,
  type TimelineDetailView,
} from "./processing-timeline/timeline-detail";

/* Ported from frontend/src/components/knowledge-processing-timeline.vue:
 * Langfuse-style waterfall of the document processing pipeline
 * (docreader → chunking → embedding → multimodal → postprocess), with
 * per-span detail panel, attempt switching, cancel/retry and live
 * polling while the trace is active. */

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

const POLL_INTERVAL_MS = 2000;
/* Keeps the poll loop alive briefly after the trace goes quiet so
 * late-arriving async postprocess subspans (wiki debounce ~30s, summary,
 * question, graph.chunk[*]) still surface without a manual refresh. */
const QUIESCE_GRACE_MS = 2 * 60 * 1000;

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

/* Latest descendant end — async postprocess children often extend
 * well past the parent's own finished_at. */
function descendantMaxEnd(node: SpanNode): number | null {
  const ends: number[] = [];
  for (const c of node.children || []) collectEnds(c, ends);
  return ends.length ? Math.max(...ends) : null;
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
  const [detailTab, setDetailTab] = useState<TimelineDetailTab>("overview");
  const [lastFetchedAt, setLastFetchedAt] = useState(0);
  const [lastFetchOk, setLastFetchOk] = useState(true);
  const [failedAttempts, setFailedAttempts] = useState(0);
  const [attemptStatuses, setAttemptStatuses] = useState<Map<number, string>>(new Map());
  const [cancelling, setCancelling] = useState(false);
  const [confirmCancel, setConfirmCancel] = useState(false);
  const [notice, setNotice] = useState<TimelineNotice | null>(null);
  const [copiedKey, setCopiedKey] = useState<string | null>(null);

  const scrollRef = useRef<HTMLDivElement | null>(null);
  /* Generation guard for side-channel fetches that bypass the spans
   * coordinator (fetchProcessOverrides, per-attempt ensureAttemptStatuses
   * sub-fetches): incremented on every knowledgeId switch and captured at
   * request time, so a late doc-A callback can never write doc-B
   * header/tab state — on success or on failure. */
  const timelineGenRef = useRef(0);
  /* Request coordination: exactly one spans fetch is ever in flight. A
   * fetch requested while another runs (attempt switch, knowledge-ID
   * switch, manual refresh) is remembered as pending and fires the moment
   * the old ticket settles; a late old payload that is no longer the
   * newest request for the current selection is discarded without touching
   * data/summary/status/loading. */
  const coordRef = useRef<TimelineRequestCoordinator | null>(null);
  if (!coordRef.current) {
    coordRef.current = new TimelineRequestCoordinator({ knowledgeId, attempt: undefined });
  }
  const shouldPollRef = useRef(false);
  const fetchRef = useRef<() => void>(() => {});
  const attemptRef = useRef<number | undefined>(undefined);
  const dataRef = useRef<SpansResponse | null>(null);
  const attemptStatusRef = useRef<Set<number>>(new Set());
  const userToggledRef = useRef<Set<string>>(new Set());
  /* Latest props for callbacks fired inside async fetches. */
  const propsRef = useRef({ knowledgeId, parseStatus, compact, gracePoll, onHasSpans });
  useEffect(() => {
    propsRef.current = { knowledgeId, parseStatus, compact, gracePoll, onHasSpans };
  });

  const setSelectedAttempt = useCallback((n: number | undefined) => {
    attemptRef.current = n;
    setSelectedAttemptState(n);
  }, []);

  const localizedStatus = useCallback(
    (status: string): string => localizedStatusFor(status, t),
    [t],
  );

  const stageLabel = useCallback(
    (name: string): string => stageLabelFor(name, t),
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
      const coord = coordRef.current;
      if (!coord) return;
      const { knowledgeId: id, onHasSpans: emitHas } = propsRef.current;
      if (!id) return;
      /* Sync the newest selection, then issue the fetch: when a request
       * is already in flight this queues the selection as pending and
       * returns — the settle path below fires it immediately instead of
       * waiting for the next poll tick. */
      coord.updateSelection({ knowledgeId: id, attempt: attemptRef.current });
      const ticket = coord.requestFetch();
      if (!ticket) return;
      /* Flags this ticket raised itself, so a stale settle can undo them
       * without ever clearing state owned by a newer selection. */
      let raisedLoading = false;
      let raisedRefreshing = false;
      if (opts.manual) {
        setRefreshing(true);
        raisedRefreshing = true;
      }
      if (!dataRef.current) {
        setLoading(true);
        raisedLoading = true;
      }
      let attemptOk = false;
      let settledAccept = false;
      /* Settle exactly once per ticket: releases the in-flight slot (no
       * deadlock on late resolutions) and reports the pending newest
       * selection, if any, for an immediate retry. */
      const settleTicket = (): boolean => {
        const { accept, retry } = coord.settle(ticket);
        settledAccept = accept;
        if (retry) void fetchSpans();
        return accept;
      };
      try {
        const res = (await getKnowledgeSpans(
          ticket.selection.knowledgeId,
          ticket.selection.attempt,
        )) as {
          success?: boolean;
          data?: SpansResponse;
        };
        /* Stale ticket (a newer selection was requested while this ran):
         * never let the old payload touch data/summary/status/loading —
         * and a stale terminal payload must not freeze polling for the
         * selection that is actually live. */
        if (!settleTicket()) {
          return;
        }
        if (res?.success && res.data) {
          const payload = res.data;
          dataRef.current = payload;
          setData(payload);
          attemptOk = true;
          /* Adopt the payload attempt only for a fetch that targeted no
           * attempt while the user has still chosen none — a newer user
           * choice always wins over auto-selection. */
          if (coord.shouldAutoSelectAttempt(ticket.selection.attempt)) {
            setSelectedAttempt(payload.attempt);
            coord.updateSelection({
              knowledgeId: ticket.selection.knowledgeId,
              attempt: payload.attempt,
            });
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
          /* Owner-pinned so these sub-fetches die with this selection. */
          ensureAttemptStatuses(
            payload.latest_attempt || 0,
            ticket.selection.knowledgeId,
            timelineGenRef.current,
          );
          emitHas?.(knowledgeSpansPayloadHasTrace(payload));
        } else {
          emitHas?.(false);
        }
      } catch (e) {
        console.warn("[ProcessingTimeline] fetchSpans failed", e);
        /* A failed stale ticket still releases the slot and triggers the
         * pending retry; only the newest ticket may report the failure. */
        if (!settleTicket()) {
          return;
        }
        emitHas?.(false);
      } finally {
        if (!settledAccept) {
          /* Stale ticket without a pending retry: undo only the flags it
           * raised itself. (With a retry, the fresh fetch owns them.) */
          const retrying = coord.hasPending() || coord.isInFlight();
          if (!retrying) {
            if (raisedLoading) setLoading(false);
            if (raisedRefreshing) setRefreshing(false);
          }
          return;
        }
        setLastFetchedAt(Date.now());
        setLastFetchOk(attemptOk);
        setFailedAttempts((n) => (attemptOk ? 0 : n + 1));
        setLoading(false);
        setRefreshing(false);
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [],
  );

  /* Per-attempt header statuses for the attempt tabs — the main fetch
   * only describes the selected attempt, so the other attempts are
   * queried once each. Each sub-fetch captures its owner (knowledgeId +
   * generation): a late doc-A response after a knowledgeId switch is
   * discarded instead of writing doc-B header/tab state. */
  const ensureAttemptStatuses = useCallback(
    (latest: number, ownerId?: string, ownerGen?: number) => {
      const id = ownerId ?? propsRef.current.knowledgeId;
      const gen = ownerGen ?? timelineGenRef.current;
      if (!id || latest <= 1) return;
      for (let n = 1; n <= latest; n++) {
        if (attemptStatusRef.current.has(n)) continue;
        attemptStatusRef.current.add(n);
        getKnowledgeSpans(id, n)
          .then((res) => {
            if (
              !shouldApplyResponse(
                { id, gen },
                { id: propsRef.current.knowledgeId, gen: timelineGenRef.current },
              )
            ) {
              return;
            }
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

  /* Owner-guarded like ensureAttemptStatuses above: captures the
   * knowledgeId + generation at request time and applies nothing — on
   * success or on failure — once a newer document owns this timeline. */
  const fetchProcessOverrides = useCallback(async (ownerId?: string, ownerGen?: number) => {
    const { compact: isCompact } = propsRef.current;
    const id = ownerId ?? propsRef.current.knowledgeId;
    const gen = ownerGen ?? timelineGenRef.current;
    if (isCompact || !id) return;
    const isCurrent = () =>
      shouldApplyResponse(
        { id, gen },
        { id: propsRef.current.knowledgeId, gen: timelineGenRef.current },
      );
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
      if (!isCurrent()) return;
      if (res?.success && res.data) {
        setProcessOverrides(res.data.metadata?.process_overrides ?? null);
        setCurrentFileType(
          normalizeFileType(res.data.file_type || getFileTypeFromName(res.data.file_name || res.data.title || "")),
        );
      }
    } catch {
      if (!isCurrent()) return;
      setProcessOverrides(null);
      setCurrentFileType("");
    }
  }, []);

  /* Reset + initial fetch whenever the document changes; mount the
   * permanent poll/now intervals once. */
  useEffect(() => {
    /* Drop any in-flight/pending work for the previous document so its
     * late response can never touch the new document's state, and bump
     * the generation so the coordinator-bypassing side fetches
     * (fetchProcessOverrides, per-attempt statuses) discard theirs too. */
    timelineGenRef.current += 1;
    const gen = timelineGenRef.current;
    coordRef.current?.reset({ knowledgeId, attempt: undefined });
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
    void fetchProcessOverrides(knowledgeId, gen);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [knowledgeId]);

  useEffect(() => {
    let poll: ReturnType<typeof setInterval> | null = null;
    if (autoPoll) {
      poll = setInterval(() => {
        /* While a fetch is in flight the coordinator queues the newest
         * selection as pending; the settle path fires it immediately, so
         * the tick only needs to start work when the slot is free. */
        if (coordRef.current?.isInFlight()) return;
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

  const barStyle = useCallback(
    (node: SpanNode): CSSProperties => {
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
    },
    [totalMs, t0, isLive, nowTick],
  );

  /* Faint outline from the parent's start to the latest descendant end;
   * only drawn when descendants extend ≥50ms past the parent, otherwise
   * it just duplicates the solid self-bar. */
  const wrapStyle = useCallback(
    (node: SpanNode): CSSProperties | null => {
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
    },
    [totalMs, t0],
  );

  const wrapDurationMs = useCallback((node: SpanNode): number => {
    const start = nodeStart(node);
    const childEnd = descendantMaxEnd(node);
    if (start === null || childEnd === null) return 0;
    return Math.max(0, childEnd - start);
  }, []);

  const barOffsetPct = useCallback(
    (node: SpanNode): number | null => {
      if (!totalMs || t0 === null) return null;
      const start = nodeStart(node);
      if (start === null) return null;
      return Math.max(0, Math.min(100, ((start - t0) / totalMs) * 100));
    },
    [totalMs, t0],
  );

  const barOffsetMs = useCallback(
    (node: SpanNode): number => {
      const start = nodeStart(node);
      if (start === null || t0 === null) return 0;
      return Math.max(0, start - t0);
    },
    [t0],
  );

  const liveElapsedMs = useCallback(
    (node: SpanNode): number => {
      const s = nodeStart(node);
      return s === null ? 0 : Math.max(0, nowTick - s);
    },
    [nowTick],
  );

  const scrollRowIntoView = useCallback((key: string) => {
    requestAnimationFrame(() => {
      const root = scrollRef.current;
      if (!root) return;
      const safeKey =
        typeof CSS !== "undefined" && CSS.escape ? CSS.escape(key) : key.replace(/"/g, '\\"');
      root
        .querySelector(`[data-span-key="${safeKey}"]`)
        ?.scrollIntoView({ block: "nearest", behavior: "smooth" });
    });
  }, []);

  const selectRow = useCallback(
    (row: FlatRow) => {
      if (selectedSpanId === row.key) return;
      setSelectedSpanId(row.key);
      setDetailTab("overview");
      scrollRowIntoView(row.key);
    },
    [selectedSpanId, scrollRowIntoView],
  );

  const toggleTree = useCallback(
    (row: FlatRow, ev?: ReactMouseEvent) => {
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
    },
    [expandedRows, selectRow],
  );

  /* ===== detail panel helpers ===== */

  const jsonExpandKey = useCallback(
    (section: string, key: string): string => `${selectedSpanId || ""}::${section}::${key}`,
    [selectedSpanId],
  );

  const toggleJsonKey = useCallback(
    (section: string, key: string) => {
      const k = jsonExpandKey(section, key);
      setExpandedJsonKeys((prev) => {
        const next = new Set(prev);
        if (next.has(k)) next.delete(k);
        else next.add(k);
        return next;
      });
    },
    [jsonExpandKey],
  );

  const isJsonExpanded = useCallback(
    (section: string, key: string): boolean => expandedJsonKeys.has(jsonExpandKey(section, key)),
    [expandedJsonKeys, jsonExpandKey],
  );

  const tabHasContent = useCallback(
    (tab: "input" | "output" | "metadata"): boolean => {
      const node = selectedRow?.node;
      if (!node) return false;
      return hasContent(node[tab]);
    },
    [selectedRow],
  );

  /* Fall back to overview when the metadata tab loses its content. */
  useEffect(() => {
    if (detailTab === "metadata" && !tabHasContent("metadata")) setDetailTab("overview");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedSpanId, detailTab, selectedRow]);

  const traceMetadata = useMemo(() => {
    const m = data?.trace?.metadata;
    return hasContent(m) ? m : null;
  }, [data]);

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

  const onRetry = useCallback(async () => {
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
  }, [knowledgeId, fetchSpans, setSelectedAttempt]);

  const onManualRefresh = useCallback(async () => {
    if (refreshing || loading) return;
    await fetchSpans({ manual: true });
  }, [refreshing, loading, fetchSpans]);

  /* Mirrors the backend CancelKnowledgeParse gate. */
  const canCancelParse = isPollingStatus(data?.parse_status ?? parseStatus);

  const onCancelParseConfirm = useCallback(async () => {
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
  }, [cancelling, knowledgeId, t, fetchSpans]);

  const onAttemptChange = useCallback(
    (n: number) => {
      if (Number.isNaN(n)) return;
      setSelectedAttempt(n);
      setSelectedSpanId(null);
      /* New attempt: forget per-row toggles so auto-expand re-evaluates
       * cleanly against the new tree. */
      userToggledRef.current = new Set();
      setExpandedRows(new Set(["__root__"]));
      void fetchSpans();
    },
    [setSelectedAttempt, fetchSpans],
  );

  /* ===== render ===== */

  /* ===== view models ===== */

  const headerView = useMemo<TimelineHeaderView>(
    () => ({
      docTitle,
      knowledgeId,
      hasData: data !== null,
      parseStatus: data?.parse_status,
      lastError: data?.last_error ?? null,
      headerStatus,
      headerStatusText,
      isLive,
      processOverrides,
      currentFileType,
      loading,
      refreshing,
      canCancelParse,
      cancelling,
      confirmCancel,
      notice,
      headMetaParts,
      attemptTabs,
      showClose,
      onClose,
      setConfirmCancel,
      onManualRefresh,
      onCancelParseConfirm,
      onRetry,
      onAttemptChange,
    }),
    [
      docTitle,
      knowledgeId,
      data,
      headerStatus,
      headerStatusText,
      isLive,
      processOverrides,
      currentFileType,
      loading,
      refreshing,
      canCancelParse,
      cancelling,
      confirmCancel,
      notice,
      headMetaParts,
      attemptTabs,
      showClose,
      onClose,
      setConfirmCancel,
      onManualRefresh,
      onCancelParseConfirm,
      onRetry,
      onAttemptChange,
    ],
  );

  const rowsView = useMemo<TimelineRowsView>(
    () => ({
      loading,
      hasData: data !== null,
      showRuler,
      rulerTicks,
      scrollRef,
      flatRows,
      expandedRows,
      selectedSpanId,
      nowMarkerPct,
      barStyle,
      wrapStyle,
      barOffsetPct,
      barOffsetMs,
      wrapDurationMs,
      liveElapsedMs,
      selectRow,
      toggleTree,
    }),
    [
      loading,
      data,
      showRuler,
      rulerTicks,
      scrollRef,
      flatRows,
      expandedRows,
      selectedSpanId,
      nowMarkerPct,
      barStyle,
      wrapStyle,
      barOffsetPct,
      barOffsetMs,
      wrapDurationMs,
      liveElapsedMs,
      selectRow,
      toggleTree,
    ],
  );

  const detailView = useMemo<TimelineDetailView>(
    () => ({
      detailOpen,
      selectedRow,
      detailTab,
      setDetailTab,
      tabHasContent,
      copiedKey,
      copyValue,
      toggleJsonKey,
      isJsonExpanded,
      setSelectedSpanId,
      stages,
      totalMs,
      traceMetadata,
      traceKnowledgeId: data?.knowledge_id,
      currentAttempt: data?.current_attempt,
      barOffsetMs,
      liveElapsedMs,
    }),
    [
      detailOpen,
      selectedRow,
      detailTab,
      setDetailTab,
      tabHasContent,
      copiedKey,
      copyValue,
      toggleJsonKey,
      isJsonExpanded,
      setSelectedSpanId,
      stages,
      totalMs,
      traceMetadata,
      data,
      barOffsetMs,
      liveElapsedMs,
    ],
  );

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
      <TimelineHeader view={headerView} />
      <TimelineRows view={rowsView} />
      <TimelineDetail view={detailView} />
    </div>
  );
}

