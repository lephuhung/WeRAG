"use client";

import {
  IconCheckCircleFilled,
  IconChevronRight,
  IconClock,
  IconClose,
  IconErrorCircleFilled,
  IconInfoCircleFilled,
  IconMinusCircle,
  IconRefresh,
} from "@/components/icons";
import { renderFileIconSvg } from "@/components/files/file-icon";
import { ProgressRing } from "@/components/upload-tasks/progress-ring";
import { itemPhase, type UploadItem } from "@/lib/upload-tasks-state";
import { useT, type LocaleKey } from "@/lib/i18n";

/* Ported from frontend/src/components/upload-tasks/UploadTaskRow.vue. */

function bytes(value: number): string {
  if (!value) return "0 B";
  const k = 1024;
  const sizes = ["B", "KB", "MB", "GB"];
  const i = Math.min(Math.floor(Math.log(value) / Math.log(k)), sizes.length - 1);
  return `${parseFloat((value / Math.pow(k, i)).toFixed(1))} ${sizes[i]}`;
}

type RowAction = "cancel" | "retry" | "open";

function StatusIcon({ phase, ratio }: { phase: string; ratio: number }) {
  const cls = "h-5 w-5";
  if (phase === "uploading") return <ProgressRing value={ratio} size={20} stroke={2.5} />;
  if (phase === "waiting") return <IconClock className={cls} />;
  if (phase === "saving" || phase === "parsing")
    return (
      <span className="block h-4 w-4 animate-spin rounded-full border-2 border-current border-t-transparent" />
    );
  if (phase === "ready") return <IconCheckCircleFilled className={cls} />;
  if (phase === "failed") return <IconErrorCircleFilled className={cls} />;
  if (phase === "duplicate") return <IconInfoCircleFilled className={cls} />;
  return <IconMinusCircle className={cls} />;
}

const ACTION_META: Record<RowAction, { labelKey: LocaleKey; icon: (cls: string) => React.ReactNode }> = {
  cancel: { labelKey: "uploadTasks.cancel", icon: (cls) => <IconClose className={cls} /> },
  retry: { labelKey: "uploadTasks.retry", icon: (cls) => <IconRefresh className={cls} /> },
  open: { labelKey: "uploadTasks.open", icon: (cls) => <IconChevronRight className={cls} /> },
};

export function UploadTaskRow({
  item,
  onCancel,
  onRetry,
  onOpen,
}: {
  item: UploadItem;
  onCancel: (id: string) => void;
  onRetry: (id: string) => void;
  onOpen: (item: UploadItem) => void;
}) {
  const { t } = useT();
  const phase = itemPhase(item);
  /** Folder uploads repeat names like README.md; the subfolder tells them apart. */
  const folder = item.relativePath.split("/").slice(0, -1).join("/");
  const ratio = item.size > 0 ? item.loaded / item.size : 0;

  const action: RowAction | null = (() => {
    switch (phase) {
      case "waiting":
      case "uploading":
        return "cancel";
      case "saving":
        return null;
      case "failed":
      case "cancelled":
        if (item.transfer === "failed" || item.transfer === "cancelled") return "retry";
        return item.knowledgeId && item.parseStatus !== "deleted" ? "open" : null;
      default:
        return item.knowledgeId ? "open" : null;
    }
  })();

  /** Failure reason, shown after the phase label and in full on hover. */
  const reason =
    phase === "failed"
      ? (item.transfer === "failed" ? item.error : item.parseError) || ""
      : "";

  const meta = (() => {
    switch (phase) {
      case "waiting":
        return `${t("uploadTasks.phaseWaiting")} · ${bytes(item.size)}`;
      case "uploading":
        return `${bytes(item.loaded)} / ${bytes(item.size)}`;
      case "saving":
        return t("uploadTasks.phaseSaving");
      case "parsing":
        return t(item.parseStatus === "processing" ? "uploadTasks.phaseParsing" : "uploadTasks.phasePending");
      case "ready":
        return t(item.parseStatus === "finalizing" ? "uploadTasks.phaseFinalizing" : "uploadTasks.phaseReady");
      case "failed": {
        const label = t(item.transfer === "failed" ? "uploadTasks.phaseUploadFailed" : "uploadTasks.phaseParseFailed");
        return reason ? `${label} · ${reason}` : label;
      }
      case "duplicate":
        return t("uploadTasks.phaseDuplicate");
      case "cancelled":
        return t(item.parseStatus === "deleted" ? "uploadTasks.phaseDeleted" : "uploadTasks.phaseCancelled");
    }
  })();

  const runAction = () => {
    if (action === "cancel") onCancel(item.id);
    else if (action === "retry") onRetry(item.id);
    else if (action === "open") onOpen(item);
  };

  const statusColor =
    phase === "uploading" || phase === "saving" || phase === "parsing" || phase === "ready"
      ? "text-ink"
      : phase === "failed"
        ? "text-error"
        : phase === "duplicate"
          ? "text-amber-600"
          : "text-muted-soft";

  return (
    <div
      className={`group flex items-center gap-3 rounded-[8px] p-2 transition-colors hover:bg-surface-strong/60 ${
        action === "open" ? "cursor-pointer" : ""
      }`}
      onClick={() => action === "open" && onOpen(item)}
    >
      <span
        className="grid h-8 w-8 shrink-0 place-items-center rounded-[6px] bg-surface-strong text-muted"
        aria-hidden="true"
        dangerouslySetInnerHTML={{ __html: renderFileIconSvg(item.name) }}
      />
      <span className="flex min-w-0 flex-1 flex-col">
        <span className="flex min-w-0 items-baseline gap-1.5 text-[13px] leading-5 text-ink" title={item.relativePath || item.name}>
          <span className="min-w-0 truncate">{item.name}</span>
          {folder && <span className="min-w-0 truncate text-[11px] text-muted-soft">{folder}</span>}
        </span>
        <span
          className={`truncate text-[11.5px] leading-[18px] tabular-nums ${
            phase === "failed" ? "text-error" : phase === "duplicate" ? "text-amber-600" : "text-muted-soft"
          }`}
          title={reason || undefined}
        >
          {meta}
        </span>
      </span>
      <span className="relative grid h-7 w-7 shrink-0 place-items-center">
        <span
          className={`grid place-items-center transition-opacity ${statusColor} ${
            action ? "group-hover:opacity-0 group-focus-within:opacity-0" : ""
          }`}
          aria-hidden="true"
        >
          <StatusIcon phase={phase} ratio={ratio} />
        </span>
        {action && (
          <button
            type="button"
            className="absolute inset-0 grid place-items-center rounded-[6px] text-muted opacity-0 transition-opacity hover:bg-surface-strong hover:text-ink focus-visible:opacity-100 group-hover:opacity-100 group-focus-within:opacity-100"
            aria-label={t(ACTION_META[action].labelKey)}
            title={t(ACTION_META[action].labelKey)}
            onClick={(e) => {
              e.stopPropagation();
              runAction();
            }}
          >
            {ACTION_META[action].icon("h-4.5 w-4.5")}
          </button>
        )}
      </span>
    </div>
  );
}
