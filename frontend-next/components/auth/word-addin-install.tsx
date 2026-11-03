"use client";

import { useEffect, useState } from "react";
import { useT } from "@/lib/i18n";
import { copyToClipboard } from "@/lib/clipboard";
import { renderMacInstallCommand } from "@/lib/word-addin/installer";
import { richText } from "@/lib/rich-text";
import styles from "./auth-shell.module.css";

type Platform = "windows" | "mac" | "web";

const PLATFORMS: Platform[] = ["windows", "mac", "web"];

function detectPlatform(): Platform {
  const ua = navigator.userAgent;
  if (/Windows/i.test(ua)) return "windows";
  if (/Macintosh|Mac OS X/i.test(ua)) return "mac";
  return "web";
}

/**
 * The install guide under "Install the Word add-in": one tab per way of
 * getting the manifest into Word, opened on the visitor's own system.
 */
export function WordAddinInstall() {
  const { t } = useT();
  const [platform, setPlatform] = useState<Platform>("windows");
  const [macCommand, setMacCommand] = useState("");
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    setPlatform(detectPlatform());
    setMacCommand(renderMacInstallCommand(window.location.origin));
  }, []);

  const copyCommand = async () => {
    if (await copyToClipboard(macCommand)) {
      setCopied(true);
      window.setTimeout(() => setCopied(false), 2000);
    }
  };

  const tabLabel: Record<Platform, string> = {
    windows: t("auth.installTabWindows"),
    mac: t("auth.installTabMac"),
    web: t("auth.installTabWeb"),
  };

  return (
    <div className={styles.installGuide}>
      <strong>{t("auth.installTitle")}</strong>
      <div className={styles.installTabs} role="group" aria-label={t("auth.installTitle")}>
        {PLATFORMS.map((item) => (
          <button
            key={item}
            type="button"
            aria-pressed={platform === item}
            className={styles.installTab}
            onClick={() => setPlatform(item)}
          >
            {tabLabel[item]}
          </button>
        ))}
      </div>
      <div aria-live="polite">
        {platform === "windows" && (
          <>
            <a
              className={styles.manifestDownload}
              href="/word-addin/install-windows.cmd"
              download="werag-word-addin-windows.cmd"
            >
              {t("auth.installWinDownload")} <span>.cmd</span>
            </a>
            <ol>
              <li>{richText(t("auth.installWinStep1"))}</li>
              <li>{richText(t("auth.installWinStep2"))}</li>
              <li>{richText(t("auth.installWinStep3"))}</li>
            </ol>
            <p>{richText(t("auth.installWinNote"))}</p>
          </>
        )}
        {platform === "mac" && (
          <>
            <ol>
              <li>{richText(t("auth.installMacStep1"))}</li>
            </ol>
            <div className={styles.installCommand}>
              <code>{macCommand}</code>
              <button type="button" onClick={copyCommand} disabled={!macCommand}>
                {copied ? t("auth.installCopied") : t("auth.installCopy")}
              </button>
            </div>
            <ol start={2}>
              <li>{richText(t("auth.installMacStep2"))}</li>
              <li>{richText(t("auth.installMacStep3"))}</li>
            </ol>
            <p>{richText(t("auth.installMacNote"))}</p>
          </>
        )}
        {platform === "web" && (
          <>
            <a
              className={styles.manifestDownload}
              href="/word-addin/manifest.xml"
              download="werag-word-addin.xml"
            >
              {t("auth.installDownload")} <span>.xml</span>
            </a>
            <ol>
              <li>{richText(t("auth.installWebStep1"))}</li>
              <li>{richText(t("auth.installWebStep2"))}</li>
            </ol>
            <p>{richText(t("auth.installWebNote"))}</p>
          </>
        )}
      </div>
    </div>
  );
}
