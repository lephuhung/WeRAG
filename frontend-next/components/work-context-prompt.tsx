/* Asks for the user's work unit and province once their profile lacks a
 * province (decided 2026-10-12: asked up front by the UI, not by the model).
 * "Later" hides it for this browser session only. */
"use client";

import { useEffect, useState } from "react";
import { useAuth } from "@/lib/auth";
import { useT } from "@/lib/i18n";
import { Modal } from "@/components/modal";
import { WorkContextForm } from "@/components/settings/work-context-form";

const SNOOZE_KEY = "werag.workContext.snoozed";

export function WorkContextPrompt() {
  const { t } = useT();
  const auth = useAuth();
  const [open, setOpen] = useState(false);

  useEffect(() => {
    if (!auth.user) return;
    const missing = !auth.user.preferences?.province_id;
    const snoozed = typeof window !== "undefined" && window.sessionStorage.getItem(SNOOZE_KEY) === "1";
    setOpen(missing && !snoozed);
  }, [auth.user]);

  const later = () => {
    window.sessionStorage.setItem(SNOOZE_KEY, "1");
    setOpen(false);
  };

  return (
    <Modal open={open} title={t("workCtx.title")} onClose={later}>
      <p className="body-sm mb-4 text-muted">{t("workCtx.why")}</p>
      <WorkContextForm compact onSaved={() => setOpen(false)} />
      <div className="mt-4 flex justify-end">
        <button className="btn btn-tertiary btn-sm" onClick={later}>
          {t("workCtx.later")}
        </button>
      </div>
    </Modal>
  );
}
