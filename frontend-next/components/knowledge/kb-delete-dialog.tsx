/* "Delete knowledge base" — the danger zone of the KB settings modal.
 * DELETE /knowledge-bases/:id is a Manage decision on the backend
 * (TenantAdmin + KBAccessManage, then permission == admin in the handler):
 * an explicit SuperAdmin on any KB, or a Tenant Admin of the owning
 * organization. The caller passes canDelete from the server's effective
 * `manage` permission, so members, invited readers and viewers of public
 * or published KBs never see the button. The backend stays authoritative.
 * The user must type the KB name before the delete is sent. */
"use client";

import { useState } from "react";
import { Modal } from "@/components/modal";
import { ApiError } from "@/lib/api-client";
import { deleteKnowledgeBase } from "@/lib/api/knowledge";
import { useT } from "@/lib/i18n";
import { userErrorMessage } from "@/lib/user-error";
import { kbDeleteWarnings, kbNameMatches, type KbDeleteReach } from "@/lib/kb-delete";

export function KbDangerZone({
  kbId,
  kbName,
  reach,
  onDeleted,
}: {
  kbId: string;
  kbName: string;
  /** Who else reads the KB (visibility, grantees), for extra warnings. */
  reach?: KbDeleteReach;
  onDeleted: () => void;
}) {
  const { t } = useT();
  const [open, setOpen] = useState(false);
  return (
    <div className="mt-6 rounded-xl border border-error/40 p-4">
      <h3 className="title-sm text-error">{t("kbDelete.zoneTitle")}</h3>
      <div className="mt-2 flex flex-wrap items-center justify-between gap-3">
        <p className="caption max-w-[440px] text-muted">{t("kbDelete.zoneBody")}</p>
        <button type="button" className="btn btn-danger btn-sm" onClick={() => setOpen(true)}>
          {t("kbDelete.cta")}
        </button>
      </div>
      {open && (
        <KbDeleteDialog kbId={kbId} kbName={kbName} reach={reach} onClose={() => setOpen(false)} onDeleted={onDeleted} />
      )}
    </div>
  );
}

function KbDeleteDialog({
  kbId,
  kbName,
  reach,
  onClose,
  onDeleted,
}: {
  kbId: string;
  kbName: string;
  reach?: KbDeleteReach;
  onClose: () => void;
  onDeleted: () => void;
}) {
  const { t } = useT();
  const [typed, setTyped] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const matches = kbNameMatches(typed, kbName);
  const warnings = kbDeleteWarnings(reach);

  const submit = async () => {
    if (!matches || busy) return;
    setBusy(true);
    setError("");
    try {
      await deleteKnowledgeBase(kbId);
      onDeleted();
    } catch (e) {
      /* Localized causes only: backend messages are English. */
      if (e instanceof ApiError && e.status === 403) setError(t("kbDelete.errForbidden"));
      else if (e instanceof ApiError && e.status === 404) setError(t("kbDelete.errNotFound"));
      else setError(userErrorMessage(e, t, "kbDelete.errFailed"));
      setBusy(false);
    }
  };

  return (
    <Modal open title={t("kbDelete.title")} onClose={() => !busy && onClose()} width="w-[480px]">
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
      >
        <p className="body-sm text-body">{t("kbDelete.body", { name: kbName })}</p>
        {warnings.map((w) => (
          <p key={w.key} className="body-sm rounded-lg bg-warning-surface px-3 py-2 text-warning">
            {t(w.key, w.vars)}
          </p>
        ))}
        <p className="body-sm text-error">{t("kbDelete.irreversible")}</p>
        <label className="block">
          <span className="caption mb-1.5 block text-muted">{t("kbDelete.typeName", { name: kbName })}</span>
          <input
            className="input"
            value={typed}
            autoComplete="off"
            spellCheck={false}
            disabled={busy}
            onChange={(e) => setTyped(e.target.value)}
            aria-invalid={typed !== "" && !matches}
          />
        </label>
        {error && (
          <p role="alert" className="caption text-error">
            {error}
          </p>
        )}
        <div className="flex justify-end gap-2 pt-1">
          <button type="button" className="btn btn-outline" onClick={onClose} disabled={busy}>
            {t("common.cancel")}
          </button>
          <button type="submit" className="btn btn-danger" disabled={!matches || busy} aria-busy={busy}>
            {busy && (
              <span
                aria-hidden
                className="h-3.5 w-3.5 animate-spin rounded-full border-[1.5px] border-current border-t-transparent motion-reduce:animate-none"
              />
            )}
            {busy ? t("kbDelete.deleting") : t("kbDelete.confirm")}
          </button>
        </div>
      </form>
    </Modal>
  );
}
