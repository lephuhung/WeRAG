"use client";

/* How to wire the transaction checker to the payment webhook, folded under
 * the webhook secret on the SuperAdmin billing page: setup steps, the
 * request to send, what each answer means, and a live check of the secret
 * that was just generated. The long form is docs/billing-payment-webhook.md. */

import { useState, type ReactNode } from "react";
import { IconCopy } from "@/components/icons";
import { DataTable } from "@/components/settings/data-table";
import { copyToClipboard } from "@/lib/clipboard";
import { useT, type LocaleKey } from "@/lib/i18n";

const WEBHOOK_PATH = "/api/v1/billing/payments/webhook";

const FIELDS: { name: string; required: boolean; note: LocaleKey }[] = [
  { name: "source", required: true, note: "bwg.fieldSource" },
  { name: "transaction_id", required: true, note: "bwg.fieldTxId" },
  { name: "amount_vnd", required: true, note: "bwg.fieldAmount" },
  { name: "content", required: false, note: "bwg.fieldContent" },
  { name: "account_no", required: false, note: "bwg.fieldAccount" },
  { name: "direction", required: false, note: "bwg.fieldDirection" },
  { name: "transacted_at", required: false, note: "bwg.fieldTime" },
];

const CODES: { code: string; note: LocaleKey }[] = [
  { code: "200", note: "bwg.code200" },
  { code: "400", note: "bwg.code400" },
  { code: "401", note: "bwg.code401" },
  { code: "429", note: "bwg.code429" },
  { code: "500", note: "bwg.code500" },
  { code: "503", note: "bwg.code503" },
];

const STATUSES: { status: string; note: LocaleKey }[] = [
  { status: "matched", note: "bwg.stMatched" },
  { status: "unmatched", note: "bwg.stUnmatched" },
  { status: "underpaid", note: "bwg.stUnderpaid" },
  { status: "order_closed", note: "bwg.stClosed" },
  { status: "confirm_failed", note: "bwg.stFailed" },
  { status: "ignored", note: "bwg.stIgnored" },
];

function sampleRequest(origin: string): string {
  return [
    `curl -X POST ${origin}${WEBHOOK_PATH} \\`,
    `  -H "X-Webhook-Secret: <secret>" \\`,
    `  -H "Content-Type: application/json" \\`,
    `  -d '{`,
    `    "source": "bank-poller",`,
    `    "transaction_id": "FT26291123456",`,
    `    "amount_vnd": 199000,`,
    `    "content": "IBFT WRABCD2345 thanh toan goi",`,
    `    "account_no": "0011002233",`,
    `    "direction": "in",`,
    `    "transacted_at": "2026-10-18T14:05:00+07:00"`,
    `  }'`,
  ].join("\n");
}

type CheckResult = { ok: boolean; message: LocaleKey; retryAfter?: string };

/** An empty body with the secret: 400 means the secret passed and only
 *  the body was refused, so nothing is recorded. */
async function checkSecret(secret: string): Promise<CheckResult> {
  try {
    const res = await fetch(WEBHOOK_PATH, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Webhook-Secret": secret },
      body: "{}",
    });
    switch (res.status) {
      case 400:
        return { ok: true, message: "bwg.checkOk" };
      case 401:
        return { ok: false, message: "bwg.checkWrong" };
      case 429:
        return { ok: false, message: "bwg.checkLimited", retryAfter: res.headers.get("Retry-After") ?? "?" };
      case 503:
        return { ok: false, message: "bwg.checkOff" };
      default:
        return { ok: false, message: "bwg.checkOther" };
    }
  } catch {
    return { ok: false, message: "bwg.checkOther" };
  }
}

function Table({ head, rows }: { head: string[]; rows: ReactNode[][] }) {
  return (
    <DataTable<ReactNode[]>
      minWidth="min-w-[420px]"
      columns={head.map((h, i) => ({ key: `col-${i}`, header: h }))}
      rows={rows}
      rowKey={(r) => String(r[0])}
      renderRow={(r) => (
        <>
          {r.map((cell, j) => (
            <td
              key={j}
              className={
                j === 0
                  ? "whitespace-nowrap px-5 py-3.5 align-middle font-mono text-ink"
                  : "px-5 py-3.5 align-middle text-ink"
              }
            >
              {cell}
            </td>
          ))}
        </>
      )}
    />
  );
}

export function WebhookGuide({ enabled, secretShown }: { enabled: boolean; secretShown: string }) {
  const { t } = useT();
  const [copied, setCopied] = useState(false);
  const [checking, setChecking] = useState(false);
  const [check, setCheck] = useState<CheckResult | null>(null);
  const origin = typeof window === "undefined" ? "https://<server>" : window.location.origin;
  const sample = sampleRequest(origin);

  const copySample = async () => {
    setCopied(await copyToClipboard(sample));
    setTimeout(() => setCopied(false), 2000);
  };
  const runCheck = async () => {
    setChecking(true);
    setCheck(await checkSecret(secretShown));
    setChecking(false);
  };

  const h4 = "mb-1.5 mt-4 text-[13px] font-medium text-ink";
  return (
    <details className="mt-3 rounded-md border border-hairline px-3 py-2">
      <summary className="cursor-pointer text-[13px] font-medium text-ink">{t("bwg.title")}</summary>
      <div className="caption pb-2 text-muted">
        <h4 className={h4}>{t("bwg.setupTitle")}</h4>
        <ol className="list-decimal space-y-1 pl-5">
          <li>{t("bwg.step1")}</li>
          <li>{t("bwg.step2")}</li>
          <li>
            {t("bwg.step3")}
            {secretShown ? (
              <span className="mt-1.5 flex flex-wrap items-center gap-2">
                <button type="button" className="btn btn-outline btn-sm" disabled={checking} onClick={() => void runCheck()}>{t("bwg.checkButton")}</button>
                {check && <span className={check.ok ? "text-success" : "text-error"}>{t(check.message, { seconds: check.retryAfter ?? "" })}</span>}
              </span>
            ) : (
              <span className="mt-1 block text-muted-soft">{enabled ? t("bwg.checkNeedsNewSecret") : t("bwg.checkNeedsSecret")}</span>
            )}
          </li>
          <li>{t("bwg.step4")}</li>
        </ol>

        <h4 className={h4}>{t("bwg.requestTitle")}</h4>
        <p className="mb-1.5">{t("bwg.requestDesc")}</p>
        <div className="relative">
          <pre className="overflow-x-auto rounded-md bg-surface-strong p-3 font-mono text-[12px] leading-relaxed text-ink">{sample}</pre>
          <button type="button" className="btn btn-outline btn-sm absolute right-2 top-2" onClick={() => void copySample()}>
            <IconCopy className="h-3.5 w-3.5" /> {copied ? t("badm.webhookCopied") : t("badm.webhookCopy")}
          </button>
        </div>
        <p className="mt-1.5">{t("bwg.requestAuth")}</p>

        <h4 className={h4}>{t("bwg.fieldsTitle")}</h4>
        <Table
          head={[t("bwg.colField"), t("bwg.colRequired"), t("bwg.colNote")]}
          rows={FIELDS.map((f) => [f.name, f.required ? t("bwg.yes") : "", t(f.note)])}
        />

        <h4 className={h4}>{t("bwg.codesTitle")}</h4>
        <Table head={[t("bwg.colCode"), t("bwg.colAction")]} rows={CODES.map((c) => [c.code, t(c.note)])} />
        <p className="mt-1.5">{t("bwg.retryNote")}</p>

        <h4 className={h4}>{t("bwg.statusTitle")}</h4>
        <Table head={[t("bwg.colStatus"), t("bwg.colMeaning")]} rows={STATUSES.map((s) => [s.status, t(s.note)])} />
        <p className="mt-1.5">{t("bwg.listNote")}</p>

        <h4 className={h4}>{t("bwg.securityTitle")}</h4>
        <ul className="list-disc space-y-1 pl-5">
          <li>{t("bwg.secRotate")}</li>
          <li>{t("bwg.secLimits")}</li>
          <li>{t("bwg.secLeak")}</li>
        </ul>

        <h4 className={h4}>{t("bwg.deployTitle")}</h4>
        <p className="mb-1.5">{t("bwg.deployDesc")}</p>
        <pre className="overflow-x-auto rounded-md bg-surface-strong p-3 font-mono text-[12px] leading-relaxed text-ink">{"WEKNORA_TRUSTED_PROXIES=127.0.0.0/8,::1/128,<docker-subnet>"}</pre>
        <p className="mt-1.5">{t("bwg.deploySteps")}</p>

        <p className="mt-4 text-muted-soft">{t("bwg.fullGuide")}</p>
      </div>
    </details>
  );
}
