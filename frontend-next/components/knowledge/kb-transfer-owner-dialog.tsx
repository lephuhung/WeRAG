/* SuperAdmin-only owner transfer: pick the new owner unit, read the
 * warning (every share revoked, a published KB withdrawn), confirm, then
 * see what was revoked. POST /knowledge-bases/:id/transfer-owner. */
"use client";

import { useEffect, useState } from "react";
import { Modal } from "@/components/modal";
import { TenantPicker } from "@/components/knowledge/tenant-picker";
import { transferKBOwner, type KBOwnerTransferResult } from "@/lib/api/kb-access";
import { useT } from "@/lib/i18n";

export function KbTransferOwnerDialog({
  kbId,
  ownerTenantId,
  open,
  onClose,
  onTransferred,
}: {
  kbId: string;
  ownerTenantId: number;
  open: boolean;
  onClose: () => void;
  onTransferred: () => void;
}) {
  const { t } = useT();
  const [target, setTarget] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [result, setResult] = useState<KBOwnerTransferResult | null>(null);

  useEffect(() => {
    if (open) {
      setTarget("");
      setError("");
      setResult(null);
    }
  }, [open]);

  const submit = async () => {
    const id = Number(target);
    if (!id) return;
    setBusy(true);
    setError("");
    try {
      const res = await transferKBOwner(kbId, id);
      setResult(res);
      onTransferred();
    } catch (e) {
      setError(e instanceof Error && e.message ? e.message : t("kbAccess.failed"));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal open={open} title={t("kbAccess.transfer")} onClose={onClose}>
      {result ? (
        <div className="space-y-3">
          <p className="body-sm text-ink">
            {t("kbAccess.transferDone", {
              inv: result.revoked_invitations,
              grants: result.revoked_grants,
            })}
          </p>
          {result.withdrawn_from_published && <p className="body-sm text-muted">{t("kbAccess.transferWithdrawn")}</p>}
          <div className="flex justify-end">
            <button type="button" className="btn btn-primary btn-sm" onClick={onClose}>
              {t("common.close")}
            </button>
          </div>
        </div>
      ) : (
        <div className="space-y-4">
          <p className="body-sm text-body">{t("kbAccess.transferDesc")}</p>
          <p className="body-sm rounded-xl border border-warning/40 bg-warning-surface px-3 py-2 text-warning">
            {t("kbAccess.transferWarn")}
          </p>
          <label className="block">
            <span className="caption mb-1 block text-muted">{t("kbAccess.transferTarget")}</span>
            <TenantPicker value={target} onChange={setTarget} excludeId={ownerTenantId || undefined} required />
          </label>
          {error && <p className="body-sm text-error">{error}</p>}
          <div className="flex justify-end gap-2">
            <button type="button" className="btn btn-outline btn-sm" onClick={onClose}>
              {t("common.cancel")}
            </button>
            <button
              type="button"
              className="btn btn-primary btn-sm"
              disabled={busy || !target}
              onClick={() => void submit()}
            >
              {t("kbAccess.transferConfirm")}
            </button>
          </div>
        </div>
      )}
    </Modal>
  );
}
