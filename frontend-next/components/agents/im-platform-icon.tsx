/* Per-platform glyphs for IM channels — simplified monochrome marks in the
 * app's stroke style (24px, 1.6 stroke) so they sit next to the existing
 * icon set. Exact brand logos are intentionally not reproduced. */
"use client";

import { IconChat } from "@/components/icons";

export function IMPlatformIcon({ platform, className }: {
  platform: string;
  className?: string;
}) {
  const c = className ?? "h-5 w-5";
  switch (platform) {
    case "telegram":
      return (
        <svg viewBox="0 0 24 24" className={c} fill="currentColor" aria-hidden>
          <path d="M21.9 4.6 18.9 19c-.2 1-.8 1.2-1.6.8l-4.5-3.3-2.2 2.1c-.24.24-.44.44-.9.44l.32-4.6L18.3 6.5c.36-.32-.08-.5-.56-.18L7.5 12.9l-4.4-1.4c-.96-.3-.98-.96.2-1.42l17.2-6.6c.8-.3 1.5.18 1.4 1.12z" />
        </svg>
      );
    case "slack":
      return (
        <svg viewBox="0 0 24 24" className={c} fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" aria-hidden>
          <path d="M9 4.5v15M15 4.5v15M4.5 9h15M4.5 15h15" />
        </svg>
      );
    case "wecom":
    case "wechat":
      return (
        <svg viewBox="0 0 24 24" className={c} fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
          <ellipse cx="9.5" cy="9.5" rx="6.5" ry="5.5" />
          <path d="M13.2 14.6c.9.9 2.2 1.4 3.6 1.4.6 0 1.2-.1 1.8-.3l2.1 1-.5-1.8c1-.8 1.6-1.9 1.6-3.2 0-2.4-2.2-4.3-4.9-4.3" />
          <circle cx="7.5" cy="9" r="0.5" fill="currentColor" />
          <circle cx="11.5" cy="9" r="0.5" fill="currentColor" />
        </svg>
      );
    case "dingtalk":
      return (
        <svg viewBox="0 0 24 24" className={c} fill="currentColor" aria-hidden>
          <path d="M13.2 2.5 5.4 13.4h4.8L9.1 21.5l7.9-11.1h-4.9l1.1-7.9z" />
        </svg>
      );
    case "feishu":
    case "lark":
      return (
        <svg viewBox="0 0 24 24" className={c} fill="currentColor" aria-hidden>
          <path d="M4 12.2C4 8.2 7.2 5 11.3 5c4.4 0 8 3.4 8.9 8.8-2.9-1-5-1-6.9.1-2.5 1.4-5 1.1-6.6-.4L4 12.2z" />
        </svg>
      );
    case "qqbot":
      return (
        <svg viewBox="0 0 24 24" className={c} fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" aria-hidden>
          <rect x="6" y="8" width="12" height="10" rx="4" />
          <path d="M12 8V5" />
          <circle cx="12" cy="4" r="1" fill="currentColor" stroke="none" />
          <circle cx="9.5" cy="12.5" r="0.6" fill="currentColor" stroke="none" />
          <circle cx="14.5" cy="12.5" r="0.6" fill="currentColor" stroke="none" />
        </svg>
      );
    case "mattermost":
      return (
        <svg viewBox="0 0 24 24" className={c} fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinejoin="round" aria-hidden>
          <circle cx="12" cy="12" r="8.5" />
          <path d="M15.5 8.5v7l-6.5-3.5 6.5-3.5z" fill="currentColor" stroke="none" />
        </svg>
      );
    default:
      return <IconChat className={c} />;
  }
}

/* Human label — single source for selects and cards. */
export const IM_PLATFORM_LABELS: Record<string, string> = {
  telegram: "Telegram",
  slack: "Slack",
  feishu: "Feishu",
  lark: "Lark",
  wecom: "WeCom",
  wechat: "WeChat",
  dingtalk: "DingTalk",
  qqbot: "QQ Bot",
  mattermost: "Mattermost",
};
