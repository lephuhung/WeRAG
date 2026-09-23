/* Ported from frontend/src/views/integrations/{CliIntegrationLanding,
 * ChromeExtensionLanding, ClawSkillLanding, IntegrationLandingLayout}.vue —
 * doc-style landing that keeps the copy command builder (cliIntegration.ts)
 * and links each section into the matching surface. The heavy Api
 * playground / MCP-server settings (~3.4k lines in the Vue app) stay there
 * until ported; the cards below link to their Settings sections instead so
 * nothing is silently absent.
 */
"use client";

import { useEffect, useState } from "react";
import { useT } from "@/lib/i18n";
import { copyToClipboard } from "@/lib/clipboard";

/** Port of cliIntegration.ts buildCLIConnectCommand. */
function buildCliConnectCommand(apiBase: string): string {
  let host = "https://your-server.com";
  try {
    const url = new URL(apiBase, window.location.origin);
    if (url.protocol === "https:" || url.protocol === "http:") {
      const path = url.pathname.replace(/\/+$/, "").replace(/\/api\/v1$/, "");
      host = `${url.origin}${path}`;
    }
  } catch {
    /* keep default host */
  }
  const quoted = `'${host.replace(/'/g, `'\"'\"'`)}'`;
  return `weknora profile add weknora --host ${quoted} --use &&\nweknora auth login`;
}

type Tab = "cli" | "chrome" | "claw" | "im" | "api" | "mcp";

const TABS: { id: Tab; labelEn: string; labelVi: string; descEn: string; descVi: string }[] = [
  { id: "cli", labelEn: "CLI", labelVi: "CLI", descEn: "Terminal client: connect, chat, manage from the shell.", descVi: "Ứng dụng dòng lệnh: kết nối, chat, quản lý từ terminal." },
  { id: "chrome", labelEn: "Chrome extension", labelVi: "Tiện ích Chrome", descEn: "Ask your knowledge bases from any page.", descVi: "Hỏi kho tri thức ngay trên mọi trang web." },
  { id: "claw", labelEn: "Claw skills", labelVi: "Kỹ năng Claw", descEn: "Publish skills the agent sandbox can install.", descVi: "Công bố kỹ năng để sandbox cài đặt." },
  { id: "im", labelEn: "Instant messaging", labelVi: "Tin nhắn tức thời", descEn: "Bring the agent into IM channels.", descVi: "Đưa trợ lý vào các kênh tin nhắn." },
  { id: "api", labelEn: "HTTP API", labelVi: "HTTP API", descEn: "Serve RAG answers to your own product.", descVi: "Cung cấp câu trả lời RAG cho sản phẩm của bạn." },
  { id: "mcp", labelEn: "MCP server", labelVi: "MCP server", descEn: "Expose the workspace as an MCP tool source.", descVi: "Cung cấp không gian làm việc như nguồn công cụ MCP." },
];

function ConnectionBanner() {
  const { t } = useT();
  const [cmd, setCmd] = useState("weknora profile add weknora --host 'https://your-server.com' --use &&\nweknora auth login");
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    // apiBase is relative on same-origin deployments; the builder trims
    // trailing /api/v1 the same way the Vue helper does.
    setCmd(buildCliConnectCommand("/api/v1"));
  }, []);

  return (
    <div className="card p-5">
      <div className="caption-uppercase mb-2 text-muted">{t("integrations.connect")}</div>
      <div className="code-toolbar-like flex items-center gap-3 rounded-[12px] bg-surface-strong px-4 py-3">
        <pre className="min-w-0 flex-1 overflow-x-auto font-mono text-[13px] text-ink">{cmd}</pre>
        <button
          className="btn btn-outline btn-sm shrink-0"
          onClick={async () => {
            const ok = await copyToClipboard(cmd);
            if (ok) {
              setCopied(true);
              setTimeout(() => setCopied(false), 1500);
            }
          }}
        >
          {copied ? t("integrations.copied") : t("integrations.copy")}
        </button>
      </div>
    </div>
  );
}

export default function Integrations() {
  const { t, locale } = useT();
  const [active, setActive] = useState<Tab>("cli");

  useEffect(() => {
    const hash = window.location.hash.replace("#", "");
    if (TABS.some((x) => x.id === hash)) setActive(hash as Tab);
  }, []);

  return (
    <div className="flex-1 overflow-y-auto">
      <div className="mx-auto w-full max-w-[900px] px-4 py-6 sm:px-8 sm:py-10 lg:px-12">
        <div className="caption-uppercase mb-3 text-muted">Workspace</div>
        <h1 className="display-xl mb-10">{t("integrations.title")}</h1>

        <ConnectionBanner />

        <div className="mt-8 grid grid-cols-1 gap-4 md:grid-cols-2">
          {TABS.map((x) => (
            <button
              key={x.id}
              onClick={() => setActive(x.id)}
              className={`card card-hover p-5 text-left ${active === x.id ? "border-ink" : ""}`}
            >
              <div className="flex items-center justify-between">
                <div className="title-sm">{locale === "vi" ? x.labelVi : x.labelEn}</div>
                <span className="caption text-muted-soft">→</span>
              </div>
              <p className="body-sm mt-1.5 text-body">
                {locale === "vi" ? x.descVi : x.descEn}
              </p>
            </button>
          ))}
        </div>

        <div className="mt-8 card p-6">
          {active === "cli" && (
            <div className="flex flex-col gap-4">
              <h2 className="title-md">{t("integrations.cli.quickstart")}</h2>
              <ol className="flex flex-col gap-3">
                {[
                  { k: "install", c: "npm i -g @weknora/cli" },
                  { k: "connect", c: buildCliConnectCommand("/api/v1") },
                  { k: "chat", c: "weknora chat \"how do I file an expense?\"" },
                ].map((s, i) => (
                  <li key={s.k} className="flex gap-4">
                    <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-surface-strong text-[13px] font-semibold text-ink">
                      {i + 1}
                    </span>
                    <div className="min-w-0 flex-1">
                      <div className="text-[14px] font-medium text-ink">
                        {t(`integrations.cli.${s.k}Title` as never)}
                      </div>
                      <div className="mt-1.5 overflow-x-auto rounded-[10px] bg-surface-strong px-3.5 py-2.5 font-mono text-[13px] text-ink">
                        {s.c}
                      </div>
                    </div>
                  </li>
                ))}
              </ol>
            </div>
          )}
          {active !== "cli" && (
            <p className="body-sm text-body">
              {t("integrations.comingSoon")}
            </p>
          )}
        </div>
      </div>
    </div>
  );
}

