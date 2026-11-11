"use client";

/* Answer-quality inbox: two tabs for admins.
 *   - "Disliked answers": answers users voted down, with their comment, the
 *     question and the answer (expandable), the agent and the references.
 *   - "Ungrounded questions": turns a gate answered without evidence (the
 *     knowledge base lacks it), as a list or grouped by the most frequent
 *     normalized question over the period.
 * Admins tag a cause and mark a row reviewed; the user's message is never
 * changed. scope="tenant" is a Tenant Admin's own organization (settings
 * modal); scope="platform" is the SuperAdmin console, with an organization
 * filter. There is no read-only conversation viewer for admins, so the
 * question and answer are shown inline. Backend: /api/v1/quality/* and
 * /api/v1/system/quality/* (server guards are authoritative). */

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { IconRefresh } from "@/components/icons";
import { Select, type SelectOption } from "@/components/select";
import { useToast } from "@/components/toast";
import { DataTable } from "@/components/settings/data-table";
import { Pager } from "@/components/settings/pager";
import { Pill, PillGroup } from "@/components/settings/pill";
import { Notice } from "@/components/settings/notice";
import { TabStrip } from "@/components/settings/tab-strip";
import { useT, type LocaleKey } from "@/lib/i18n";
import { ApiError } from "@/lib/api-client";
import { formatDateTime } from "@/lib/datetime";
import { listTenantDirectory, type TenantDirectoryEntry } from "@/lib/api/tenants";
import {
  DISLIKED_CAUSES,
  UNGROUNDED_CAUSES,
  groupUngroundedQuestions,
  listDislikedAnswers,
  listUngroundedQuestions,
  saveQualityReview,
  type QualityAgent,
  type QualityCause,
  type QualityFilter,
  type QualityKind,
  type QualityRow,
  type QualityScope,
  type UngroundedGroup,
  type UngroundedReason,
} from "@/lib/api/answer-quality";

const PAGE_SIZE = 20;

type ReviewedFilter = "all" | "open" | "done";
type UngroundedView = "list" | "groups";

/* The default period: the last 30 days, as YYYY-MM-DD in Vietnam time. */
function vnDate(d: Date): string {
  return new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Ho_Chi_Minh", year: "numeric", month: "2-digit", day: "2-digit" }).format(d);
}

const CAUSE_KEYS: Record<QualityCause, LocaleKey> = {
  wrong_basis: "qinbox.cause.wrong_basis",
  missing_info: "qinbox.cause.missing_info",
  off_topic: "qinbox.cause.off_topic",
  format_error: "qinbox.cause.format_error",
  other: "qinbox.cause.other",
  kb_missing: "qinbox.cause.kb_missing",
  retrieval_miss: "qinbox.cause.retrieval_miss",
  out_of_scope: "qinbox.cause.out_of_scope",
};

const REASON_KEYS: Record<UngroundedReason, LocaleKey> = {
  no_evidence: "qinbox.reason.no_evidence",
  legal_document_missing: "qinbox.reason.legal_document_missing",
};

export function QualityInbox({ scope, onDenied }: { scope: QualityScope; onDenied?: () => void }) {
  const { t } = useT();
  const [tab, setTab] = useState<QualityKind>("disliked");
  const [from, setFrom] = useState(() => vnDate(new Date(Date.now() - 29 * 86400000)));
  const [to, setTo] = useState(() => vnDate(new Date()));
  const [tenantId, setTenantId] = useState("");
  const [agentId, setAgentId] = useState("");
  const [reviewed, setReviewed] = useState<ReviewedFilter>("open");
  const [tenants, setTenants] = useState<TenantDirectoryEntry[]>([]);

  useEffect(() => {
    if (scope !== "platform") return;
    let alive = true;
    listTenantDirectory()
      .then((list) => alive && setTenants(list))
      .catch(() => undefined);
    return () => {
      alive = false;
    };
  }, [scope]);

  const filter: QualityFilter = useMemo(
    () => ({
      from,
      to,
      tenantId: scope === "platform" && tenantId ? Number(tenantId) : undefined,
      agentId: agentId || undefined,
      reviewed: reviewed === "all" ? undefined : reviewed === "done",
    }),
    [from, to, tenantId, agentId, reviewed, scope],
  );

  const tenantOptions: SelectOption[] = useMemo(
    () => [{ value: "", label: t("qinbox.allOrgs") }, ...tenants.map((x) => ({ value: String(x.id), label: x.name || `#${x.id}` }))],
    [tenants, t],
  );

  return (
    <div className="space-y-4">
      <TabStrip<QualityKind>
        ariaLabel={t("qinbox.title")}
        value={tab}
        onChange={(v) => {
          setTab(v);
          setAgentId("");
        }}
        tabs={[
          { id: "disliked", label: t("qinbox.tab.disliked") },
          { id: "ungrounded", label: t("qinbox.tab.ungrounded") },
        ]}
      />
      <div className="flex flex-wrap items-end gap-3">
        <label className="flex flex-col gap-1">
          <span className="caption text-muted">{t("qinbox.from")}</span>
          <input type="date" className="input h-9 text-[13px]" value={from} max={to || undefined} onChange={(e) => setFrom(e.target.value)} />
        </label>
        <label className="flex flex-col gap-1">
          <span className="caption text-muted">{t("qinbox.to")}</span>
          <input type="date" className="input h-9 text-[13px]" value={to} min={from || undefined} onChange={(e) => setTo(e.target.value)} />
        </label>
        {scope === "platform" && (
          <div className="flex min-w-[220px] flex-col gap-1">
            <span className="caption text-muted">{t("qinbox.org")}</span>
            <Select
              className="h-9 text-[13px]"
              value={tenantId}
              onChange={setTenantId}
              options={tenantOptions}
              searchable
              searchPlaceholder={t("qinbox.orgSearch")}
              ariaLabel={t("qinbox.org")}
            />
          </div>
        )}
        <div className="flex flex-col gap-1">
          <span className="caption text-muted">{t("qinbox.reviewed")}</span>
          <PillGroup ariaLabel={t("qinbox.reviewed")}>
            {(["open", "done", "all"] as ReviewedFilter[]).map((v) => (
              <Pill key={v} active={reviewed === v} onClick={() => setReviewed(v)}>
                {t(`qinbox.reviewed.${v}` as LocaleKey)}
              </Pill>
            ))}
          </PillGroup>
        </div>
      </div>
      <p className="caption text-muted">{t(tab === "disliked" ? "qinbox.help.disliked" : "qinbox.help.ungrounded")}</p>
      <div role="tabpanel" id={`tabpanel-${tab}`} aria-labelledby={`tab-${tab}`}>
        {tab === "disliked" ? (
          <DislikedList scope={scope} filter={filter} agentId={agentId} onAgent={setAgentId} onDenied={onDenied} />
        ) : (
          <UngroundedList scope={scope} filter={filter} agentId={agentId} onAgent={setAgentId} onDenied={onDenied} />
        )}
      </div>
    </div>
  );
}

function AgentFilter({ agents, value, onChange }: { agents: QualityAgent[]; value: string; onChange: (v: string) => void }) {
  const { t } = useT();
  const options: SelectOption[] = [
    { value: "", label: t("qinbox.allAgents") },
    ...agents.map((a) => ({ value: a.id, label: a.name || a.id })),
  ];
  if (value && !agents.some((a) => a.id === value)) options.push({ value, label: value });
  return (
    <div className="min-w-[200px]">
      <Select className="h-8 text-[13px]" value={value} onChange={onChange} options={options} ariaLabel={t("qinbox.agent")} />
    </div>
  );
}

function RefreshButton({ loading, onClick }: { loading: boolean; onClick: () => void }) {
  const { t } = useT();
  return (
    <button
      className="btn btn-outline btn-sm h-8 w-8 p-0"
      onClick={onClick}
      disabled={loading}
      title={t("common.refresh")}
      aria-label={t("common.refresh")}
    >
      <IconRefresh className={`h-3.5 w-3.5 ${loading ? "animate-spin" : ""}`} />
    </button>
  );
}

/* usePageFor keeps the page of one filter: a new filter starts at page 1
 * in the same render (no reset effect, so no second request). */
function usePageFor(key: string): [number, (page: number) => void] {
  const [state, setState] = useState({ key, page: 1 });
  const page = state.key === key ? state.page : 1;
  const setPage = useCallback((p: number) => setState({ key, page: p }), [key]);
  return [page, setPage];
}

/* useLatest keeps a callback out of effect dependencies. */
function useLatest<T>(value: T) {
  const ref = useRef(value);
  useEffect(() => {
    ref.current = value;
  }, [value]);
  return ref;
}

/* usePagedList loads one page of a list for the filter. */
function usePagedList(
  load: (f: QualityFilter) => Promise<{ items: QualityRow[]; total: number; agents: QualityAgent[] }>,
  filter: QualityFilter,
  onDenied?: () => void,
) {
  const { t } = useT();
  const [rows, setRows] = useState<QualityRow[]>([]);
  const [agents, setAgents] = useState<QualityAgent[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const key = JSON.stringify(filter);
  const [page, setPage] = usePageFor(key);
  const denied = useLatest(onDenied);
  // only the latest request may set the state
  const seq = useRef(0);

  const reload = useCallback(async () => {
    const mine = ++seq.current;
    setLoading(true);
    setError("");
    try {
      const res = await load({ ...(JSON.parse(key) as QualityFilter), page, pageSize: PAGE_SIZE });
      if (mine !== seq.current) return;
      setRows(res.items ?? []);
      setTotal(res.total ?? 0);
      setAgents(res.agents ?? []);
    } catch (e) {
      if (mine !== seq.current) return;
      if (e instanceof ApiError && e.status === 403) denied.current?.();
      setError(t("qinbox.loadFailed"));
    } finally {
      if (mine === seq.current) setLoading(false);
    }
  }, [key, page, load, t, denied]);

  useEffect(() => {
    void reload();
  }, [reload]);

  /* applyReview puts a saved review on its row; under a reviewed / not
   * reviewed filter a row that no longer matches leaves the list at once. */
  const applyReview = useCallback(
    (messageId: string, review: NonNullable<QualityRow["review"]>) => {
      const f = JSON.parse(key) as QualityFilter;
      if (f.reviewed !== undefined && review.reviewed !== f.reviewed) {
        setRows((rows) => rows.filter((x) => x.message_id !== messageId));
        setTotal((n) => Math.max(0, n - 1));
        return;
      }
      setRows((rows) => rows.map((x) => (x.message_id === messageId ? { ...x, review } : x)));
    },
    [key],
  );

  return { rows, agents, total, page, setPage, loading, error, reload, applyReview };
}

function DislikedList({
  scope,
  filter,
  agentId,
  onAgent,
  onDenied,
}: {
  scope: QualityScope;
  filter: QualityFilter;
  agentId: string;
  onAgent: (v: string) => void;
  onDenied?: () => void;
}) {
  const { t, locale } = useT();
  const [hasComment, setHasComment] = useState(false);
  const f = useMemo(() => ({ ...filter, hasComment }), [filter, hasComment]);
  const load = useCallback((q: QualityFilter) => listDislikedAnswers(scope, q), [scope]);
  const list = usePagedList(load, f, onDenied);
  const pages = Math.max(1, Math.ceil(list.total / PAGE_SIZE));

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-3">
        <AgentFilter agents={list.agents} value={agentId} onChange={onAgent} />
        <label className="flex items-center gap-2 text-[13px] text-body">
          <input type="checkbox" checked={hasComment} onChange={(e) => setHasComment(e.target.checked)} />
          {t("qinbox.hasComment")}
        </label>
        <span className="caption ml-auto text-muted">{t("qinbox.total", { n: list.total })}</span>
        <RefreshButton loading={list.loading} onClick={() => void list.reload()} />
      </div>
      {list.error && <Notice tone="error">{list.error}</Notice>}
      <DataTable<QualityRow>
        columns={[
          { key: "time", header: t("qinbox.col.time"), className: "w-[150px]" },
          { key: "who", header: t("qinbox.col.who"), className: "w-[180px]" },
          { key: "qa", header: t("qinbox.col.qa"), className: "min-w-[320px]" },
          { key: "comment", header: t("qinbox.col.comment"), className: "w-[220px]" },
          { key: "review", header: t("qinbox.col.review"), className: "w-[200px]" },
        ]}
        rows={list.rows}
        rowKey={(r) => r.message_id}
        renderRow={(r) => (
          <>
            <td className="px-5 py-3.5 align-top">
              <span className="caption tabular-nums text-body">{formatDateTime(r.feedback_at || r.created_at, locale)}</span>
            </td>
            <td className="px-5 py-3.5 align-top">
              <WhoCell row={r} scope={scope} />
            </td>
            <td className="px-5 py-3.5 align-top">
              <QuestionAnswer row={r} />
            </td>
            <td className="px-5 py-3.5 align-top">
              {r.comment ? (
                <p className="whitespace-pre-wrap break-words text-[13px] text-ink">{r.comment}</p>
              ) : (
                <span className="caption text-muted">{t("qinbox.noComment")}</span>
              )}
            </td>
            <td className="px-5 py-3.5 align-top">
              <ReviewCell scope={scope} kind="disliked" row={r} onSaved={(rv) => list.applyReview(r.message_id, rv)} />
            </td>
          </>
        )}
        loading={list.loading}
        loadingLabel={t("common.loading")}
        emptyLabel={t("qinbox.emptyDisliked")}
        minWidth="min-w-[1080px]"
      />
      <Pager page={list.page} pageCount={pages} onPage={list.setPage} loading={list.loading} />
    </div>
  );
}

function UngroundedList({
  scope,
  filter,
  agentId,
  onAgent,
  onDenied,
}: {
  scope: QualityScope;
  filter: QualityFilter;
  agentId: string;
  onAgent: (v: string) => void;
  onDenied?: () => void;
}) {
  const { t, locale } = useT();
  const [view, setView] = useState<UngroundedView>("groups");
  const [reason, setReason] = useState<UngroundedReason | "">("");
  const f = useMemo(() => ({ ...filter, reason }), [filter, reason]);
  const load = useCallback((q: QualityFilter) => listUngroundedQuestions(scope, q), [scope]);
  const list = usePagedList(load, f, onDenied);
  const pages = Math.max(1, Math.ceil(list.total / PAGE_SIZE));
  const reasonOptions: SelectOption[] = [
    { value: "", label: t("qinbox.allReasons") },
    { value: "no_evidence", label: t("qinbox.reason.no_evidence") },
    { value: "legal_document_missing", label: t("qinbox.reason.legal_document_missing") },
  ];

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-3">
        <PillGroup ariaLabel={t("qinbox.view")}>
          <Pill active={view === "groups"} onClick={() => setView("groups")}>
            {t("qinbox.view.groups")}
          </Pill>
          <Pill active={view === "list"} onClick={() => setView("list")}>
            {t("qinbox.view.list")}
          </Pill>
        </PillGroup>
        <AgentFilter agents={list.agents} value={agentId} onChange={onAgent} />
        <div className="min-w-[200px]">
          <Select className="h-8 text-[13px]" value={reason} onChange={(v) => setReason(v as UngroundedReason | "")} options={reasonOptions} ariaLabel={t("qinbox.col.reason")} />
        </div>
        {view === "list" && <span className="caption ml-auto text-muted">{t("qinbox.total", { n: list.total })}</span>}
        {view === "list" && <RefreshButton loading={list.loading} onClick={() => void list.reload()} />}
      </div>
      {view === "groups" ? (
        <UngroundedGroups scope={scope} filter={f} onDenied={onDenied} />
      ) : (
        <>
          {list.error && <Notice tone="error">{list.error}</Notice>}
          <DataTable<QualityRow>
            columns={[
              { key: "time", header: t("qinbox.col.time"), className: "w-[150px]" },
              { key: "who", header: t("qinbox.col.who"), className: "w-[180px]" },
              { key: "qa", header: t("qinbox.col.question"), className: "min-w-[320px]" },
              { key: "reason", header: t("qinbox.col.reason"), className: "w-[200px]" },
              { key: "review", header: t("qinbox.col.review"), className: "w-[200px]" },
            ]}
            rows={list.rows}
            rowKey={(r) => r.message_id}
            renderRow={(r) => (
              <>
                <td className="px-5 py-3.5 align-top">
                  <span className="caption tabular-nums text-body">{formatDateTime(r.created_at, locale)}</span>
                </td>
                <td className="px-5 py-3.5 align-top">
                  <WhoCell row={r} scope={scope} />
                </td>
                <td className="px-5 py-3.5 align-top">
                  <QuestionAnswer row={r} />
                </td>
                <td className="px-5 py-3.5 align-top">
                  <ReasonCell reason={r.reason} legalRequest={r.legal_request} />
                </td>
                <td className="px-5 py-3.5 align-top">
                  <ReviewCell scope={scope} kind="ungrounded" row={r} onSaved={(rv) => list.applyReview(r.message_id, rv)} />
                </td>
              </>
            )}
            loading={list.loading}
            loadingLabel={t("common.loading")}
            emptyLabel={t("qinbox.emptyUngrounded")}
            minWidth="min-w-[1050px]"
          />
          <Pager page={list.page} pageCount={pages} onPage={list.setPage} loading={list.loading} />
        </>
      )}
    </div>
  );
}

function UngroundedGroups({ scope, filter, onDenied }: { scope: QualityScope; filter: QualityFilter; onDenied?: () => void }) {
  const { t, locale } = useT();
  const [items, setItems] = useState<UngroundedGroup[]>([]);
  const [total, setTotal] = useState(0);
  const [rows, setRows] = useState(0);
  const [truncated, setTruncated] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const key = JSON.stringify(filter);
  const [page, setPage] = usePageFor(key);
  const denied = useLatest(onDenied);
  const seq = useRef(0);

  const reload = useCallback(async () => {
    const mine = ++seq.current;
    setLoading(true);
    setError("");
    try {
      const res = await groupUngroundedQuestions(scope, { ...(JSON.parse(key) as QualityFilter), page, pageSize: PAGE_SIZE });
      if (mine !== seq.current) return;
      setItems(res.items ?? []);
      setTotal(res.total ?? 0);
      setRows(res.rows ?? 0);
      setTruncated(!!res.truncated);
    } catch (e) {
      if (mine !== seq.current) return;
      if (e instanceof ApiError && e.status === 403) denied.current?.();
      setError(t("qinbox.loadFailed"));
    } finally {
      if (mine === seq.current) setLoading(false);
    }
  }, [key, page, scope, t, denied]);

  useEffect(() => {
    void reload();
  }, [reload]);

  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE));
  return (
    <div className="space-y-3">
      <div className="flex items-center gap-3">
        <span className="caption text-muted">{t("qinbox.groupsSummary", { groups: total, rows })}</span>
        <span className="ml-auto" />
        <RefreshButton loading={loading} onClick={() => void reload()} />
      </div>
      {truncated && <Notice tone="warning">{t("qinbox.truncated")}</Notice>}
      {error && <Notice tone="error">{error}</Notice>}
      <DataTable<UngroundedGroup>
        columns={[
          { key: "question", header: t("qinbox.col.question"), className: "min-w-[320px]" },
          { key: "count", header: t("qinbox.col.count"), className: "w-[90px]" },
          ...(scope === "platform" ? [{ key: "orgs", header: t("qinbox.col.orgs"), className: "w-[110px]" }] : []),
          { key: "reason", header: t("qinbox.col.reason"), className: "w-[220px]" },
          { key: "last", header: t("qinbox.col.last"), className: "w-[160px]" },
        ]}
        rows={items}
        rowKey={(g) => g.key}
        renderRow={(g) => (
          <>
            <td className="px-5 py-3.5 align-top">
              <p className="whitespace-pre-wrap break-words text-[13px] font-medium text-ink">{g.sample}</p>
            </td>
            <td className="px-5 py-3.5 align-top">
              <span className="text-[13px] tabular-nums text-ink">{g.count}</span>
            </td>
            {scope === "platform" && (
              <td className="px-5 py-3.5 align-top">
                <span className="text-[13px] tabular-nums text-body">{g.organizations}</span>
              </td>
            )}
            <td className="px-5 py-3.5 align-top">
              <div className="flex flex-wrap gap-1">
                {Object.entries(g.reasons ?? {}).map(([r, n]) => (
                  <span key={r} className={`badge-pill text-[10.5px] ${r === "legal_document_missing" ? "badge-warning" : "badge-neutral"}`}>
                    {t(REASON_KEYS[r as UngroundedReason] ?? "qinbox.reason.no_evidence")} · {n}
                  </span>
                ))}
              </div>
              {g.legal_request && <div className="caption mt-1 text-muted">{t("qinbox.legalRequested", { number: g.legal_request })}</div>}
            </td>
            <td className="px-5 py-3.5 align-top">
              <span className="caption tabular-nums text-body">{formatDateTime(g.last_at, locale)}</span>
            </td>
          </>
        )}
        loading={loading}
        loadingLabel={t("common.loading")}
        emptyLabel={t("qinbox.emptyUngrounded")}
        minWidth="min-w-[860px]"
      />
      <Pager page={page} pageCount={pages} onPage={setPage} loading={loading} />
    </div>
  );
}

function WhoCell({ row, scope }: { row: QualityRow; scope: QualityScope }) {
  const { t } = useT();
  return (
    <div className="space-y-0.5 text-[13px]">
      {scope === "platform" && <div className="font-medium text-ink">{row.tenant_name || `#${row.tenant_id}`}</div>}
      <div className="text-body">{row.user_name || t("qinbox.unknownUser")}</div>
      {row.agent_name || row.agent_id ? <div className="caption text-muted">{row.agent_name || row.agent_id}</div> : null}
      {row.references > 0 && <div className="caption text-muted">{t("qinbox.references", { n: row.references })}</div>}
    </div>
  );
}

function QuestionAnswer({ row }: { row: QualityRow }) {
  const { t } = useT();
  const [open, setOpen] = useState(false);
  return (
    <div className="space-y-1.5">
      <p className="whitespace-pre-wrap break-words text-[13px] font-medium text-ink">{row.question || "—"}</p>
      <div className={`whitespace-pre-wrap break-words text-[12.5px] text-body ${open ? "max-h-[420px] overflow-y-auto" : "line-clamp-3"}`}>
        {row.answer}
        {open && row.answer_truncated ? " …" : ""}
      </div>
      {row.answer && (
        <button type="button" className="text-[12px] font-medium text-primary hover:underline" onClick={() => setOpen((v) => !v)} aria-expanded={open}>
          {open ? t("qinbox.collapse") : t("qinbox.expand")}
        </button>
      )}
    </div>
  );
}

function ReasonCell({ reason, legalRequest }: { reason?: UngroundedReason; legalRequest?: string }) {
  const { t } = useT();
  return (
    <div className="space-y-1">
      {reason && (
        <span className={`badge-pill text-[10.5px] ${reason === "legal_document_missing" ? "badge-warning" : "badge-neutral"}`}>
          {t(REASON_KEYS[reason] ?? "qinbox.reason.no_evidence")}
        </span>
      )}
      <div className="caption text-muted">
        {legalRequest ? t("qinbox.legalRequested", { number: legalRequest }) : t("qinbox.noLegalRequest")}
      </div>
    </div>
  );
}

function ReviewCell({
  scope,
  kind,
  row,
  onSaved,
}: {
  scope: QualityScope;
  kind: QualityKind;
  row: QualityRow;
  onSaved: (review: NonNullable<QualityRow["review"]>) => void;
}) {
  const { t, locale } = useT();
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const review = row.review;
  const [note, setNote] = useState(review?.note ?? "");
  const causes = kind === "disliked" ? DISLIKED_CAUSES : UNGROUNDED_CAUSES;
  const options: SelectOption[] = [
    { value: "", label: t("qinbox.noCause") },
    ...causes.map((c) => ({ value: c, label: t(CAUSE_KEYS[c]) })),
  ];

  const save = async (patch: { cause?: QualityCause | ""; reviewed?: boolean; note?: string }) => {
    setBusy(true);
    try {
      await saveQualityReview(scope, row.message_id, { kind, ...patch });
      const next = {
        cause: patch.cause ?? review?.cause ?? "",
        note: patch.note ?? review?.note ?? "",
        reviewed: patch.reviewed ?? review?.reviewed ?? false,
        reviewed_by: review?.reviewed_by ?? "",
        reviewer_name: patch.reviewed === undefined ? review?.reviewer_name ?? "" : patch.reviewed ? t("qinbox.you") : "",
        reviewed_at: patch.reviewed === undefined ? review?.reviewed_at ?? null : patch.reviewed ? new Date().toISOString() : null,
      };
      onSaved(next);
    } catch {
      toast.error(t("qinbox.saveFailed"));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="space-y-2">
      <Select
        className="h-8 w-full text-[12.5px]"
        value={review?.cause ?? ""}
        onChange={(v) => void save({ cause: v as QualityCause | "" })}
        options={options}
        disabled={busy}
        ariaLabel={t("qinbox.cause")}
      />
      <label className="flex items-center gap-2 text-[12.5px] text-body">
        <input type="checkbox" checked={!!review?.reviewed} disabled={busy} onChange={(e) => void save({ reviewed: e.target.checked })} />
        {t("qinbox.markReviewed")}
      </label>
      <textarea
        className="input min-h-[52px] w-full resize-y py-1.5 text-[12.5px]"
        value={note}
        maxLength={2000}
        placeholder={t("qinbox.notePlaceholder")}
        aria-label={t("qinbox.note")}
        disabled={busy}
        onChange={(e) => setNote(e.target.value)}
        onBlur={() => {
          if (note.trim() !== (review?.note ?? "")) void save({ note: note.trim() });
        }}
      />
      {review?.reviewed && (
        <div className="caption text-muted">
          {t("qinbox.reviewedBy", { name: review.reviewer_name || "—", at: formatDateTime(review.reviewed_at, locale) })}
        </div>
      )}
    </div>
  );
}
