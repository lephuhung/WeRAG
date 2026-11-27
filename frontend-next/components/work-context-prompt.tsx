/* Asks for the user's work unit and province once their profile lacks a
 * province (decided 2026-10-12: asked up front by the UI, not by the model).
 * "Later" hides it for this browser session only; a visit opened by a
 * settings deep link is not interrupted by it. */
"use client";

import { useEffect, useRef, useState } from "react";
import { useAuth } from "@/lib/auth";
import { useT } from "@/lib/i18n";
import { Modal } from "@/components/modal";
import { WorkContextForm } from "@/components/settings/work-context-form";
import { SETTINGS_REDIRECTED_EVENT, arrivedByDeepLink } from "@/lib/settings-deep-link";

const SNOOZE_KEY = "werag.workContext.snoozed";

export function WorkContextPrompt() {
  const { t } = useT();
  const auth = useAuth();
  const [open, setOpen] = useState(false);
  // once a deep-link visit was seen, stay quiet for the rest of this
  // mount: a later profile refresh must not open the prompt over the shop
  const quietRef = useRef(false);

  useEffect(() => {
    if (!auth.user) return;
    const missing = !auth.user.preferences?.province_id;
    const snoozed = typeof window !== "undefined" && window.sessionStorage.getItem(SNOOZE_KEY) === "1";
    // a deep link (e.g. "buy this plan") opened a settings section: do not
    // cover it; the question comes back on a later visit
    if (typeof window !== "undefined" && arrivedByDeepLink(window.sessionStorage)) quietRef.current = true;
    setOpen(missing && !snoozed && !quietRef.current);
  }, [auth.user]);
  useEffect(() => {
    const onRedirect = () => {
      quietRef.current = true;
      setOpen(false);
    };
    window.addEventListener(SETTINGS_REDIRECTED_EVENT, onRedirect);
    return () => window.removeEventListener(SETTINGS_REDIRECTED_EVENT, onRedirect);
  }, []);

  const later = () => {
    window.sessionStorage.setItem(SNOOZE_KEY, "1");
    setOpen(false);
  };

  return (
    <Modal open={open} title={t("workCtx.title")} onClose={later}>
      <p className="body-sm mb-4 text-muted">{t("workCtx.why")}</p>
      <WorkContextForm
        compact
        onSaved={() => setOpen(false)}
        extraActions={
          <button className="btn btn-tertiary btn-sm" onClick={later}>
            {t("workCtx.later")}
          </button>
        }
      />
    </Modal>
  );
}
