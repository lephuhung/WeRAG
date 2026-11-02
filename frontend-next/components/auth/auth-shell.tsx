"use client";

import Link from "next/link";
import type { ReactNode } from "react";
import { useT } from "@/lib/i18n";
import { BrandLockup } from "@/components/brand-logo";
import styles from "./auth-shell.module.css";

export type AuthMode = "login" | "register" | "word";

/**
 * Renders a translated string that carries **bold** markers — the
 * translations keep the emphasis of the original markup without shipping
 * HTML through the dictionary.
 */
export function richText(text: string): ReactNode[] {
  return text.split(/(\*\*[^*]+\*\*)/g).map((part, index) =>
    part.startsWith("**") && part.endsWith("**") ? (
      <b key={index}>{part.slice(2, -2)}</b>
    ) : (
      <span key={index}>{part}</span>
    ),
  );
}

/**
 * The frame of every public authentication screen: the marketing column
 * (sign-in and sign-up only) and the form side. All three auth pages use
 * it, so they share one layout, one palette and one focus treatment.
 */
export function AuthShell({
  children,
  mode,
  showcase = true,
}: {
  children: ReactNode;
  mode: AuthMode;
  /** Hide the marketing column (the Word add-in approval screen). */
  showcase?: boolean;
}) {
  const { t } = useT();
  const year = new Date().getFullYear();
  const formAria =
    mode === "login"
      ? t("auth.formAriaLogin")
      : mode === "register"
        ? t("auth.formAriaRegister")
        : t("auth.formAriaWord");

  return (
    <main className={`${styles.shell} ${showcase ? "" : styles.shellSolo}`}>
      {showcase && (
        <section className={styles.showcase} aria-label={t("auth.showcaseAria")}>
          <Link href="/" className={styles.brand} aria-label={t("auth.brandAria")}>
            <BrandLockup size="lg" priority />
          </Link>
          <div className={styles.story}>
            <p className={styles.eyebrow}>
              <span /> {t("auth.eyebrow")}
            </p>
            <h1>
              {t("auth.titleLine1")}
              <br />
              <em>{t("auth.titleLine2")}</em>
            </h1>
            <p className={styles.description}>{t("auth.description")}</p>
            <div
              className={styles.illustration}
              role="img"
              aria-label={t("auth.illustrationAria")}
            >
              <div className={styles.orbit} />
              <div className={styles.document}>
                <div className={styles.docTop}>
                  <span>§</span>
                  <span>
                    {t("auth.docLibrary")}
                    <br />
                    <small>{t("auth.docReference")}</small>
                  </span>
                </div>
                <div className={styles.docRule} />
                <h2>{t("auth.docTitle")}</h2>
                <p>{t("auth.docTypes")}</p>
                <div className={styles.lines}>
                  <i />
                  <i />
                  <i />
                  <i />
                </div>
                <div className={styles.docHighlight}>{t("auth.docHighlight")}</div>
                <div className={styles.lines}>
                  <i />
                  <i />
                  <i />
                  <i />
                </div>
              </div>
              <div className={styles.question}>
                <span aria-hidden="true">⌕</span> {t("auth.searchPrompt")}
              </div>
              <div className={styles.answer}>
                <div className={styles.answerTop}>
                  <span className={styles.sparkle} aria-hidden="true">
                    ✦
                  </span>
                  <strong>{t("auth.answerTitle")}</strong>
                  <span className={styles.ai}>AI</span>
                </div>
                <p>{t("auth.answerText")}</p>
                <div className={styles.citations}>
                  <span>{t("auth.citeOriginal")}</span>
                  <span>{t("auth.citeClause")}</span>
                </div>
              </div>
              <span className={styles.demoLabel}>{t("auth.demoLabel")}</span>
            </div>
            <div className={styles.wordAddin}>
              <span className={styles.wordIcon} aria-hidden="true">
                W
              </span>
              <div>
                <span className={styles.wordLabel}>{t("auth.addinEyebrow")}</span>
                <strong>{t("auth.addinTitle")}</strong>
                <p>{t("auth.addinText")}</p>
              </div>
              <details className={styles.wordInstall}>
                <summary>
                  {t("auth.addinInstall")} <span aria-hidden="true">↗</span>
                </summary>
                <div className={styles.installGuide}>
                  <strong>{t("auth.installTitle")}</strong>
                  <a
                    className={styles.manifestDownload}
                    href="/word-addin/manifest.xml"
                    download="werag-word-addin.xml"
                  >
                    {t("auth.installDownload")} <span>.xml</span>
                  </a>
                  <ol>
                    <li>{richText(t("auth.installStep1"))}</li>
                    <li>{richText(t("auth.installStep2"))}</li>
                    <li>{richText(t("auth.installStep3"))}</li>
                    <li>{richText(t("auth.installStep4"))}</li>
                  </ol>
                  <p>{richText(t("auth.installCatalog"))}</p>
                </div>
              </details>
            </div>
            <div className={styles.features}>
              <div>
                <span>01 /</span>
                <strong>{t("auth.feature1Title")}</strong>
                <p>{t("auth.feature1Text")}</p>
              </div>
              <div>
                <span>02 /</span>
                <strong>{t("auth.feature2Title")}</strong>
                <p>{t("auth.feature2Text")}</p>
              </div>
              <div>
                <span>03 /</span>
                <strong>{t("auth.feature3Title")}</strong>
                <p>{t("auth.feature3Text")}</p>
              </div>
            </div>
          </div>
          <footer className={styles.showcaseFooter}>
            <span>{t("auth.footerTagline")}</span>
            <span>{t("auth.copyright", { year })}</span>
          </footer>
        </section>
      )}
      <section className={styles.formSide} aria-label={formAria}>
        {!showcase && (
          <Link href="/" className={styles.brand} aria-label={t("auth.brandAria")}>
            <BrandLockup size="md" priority />
          </Link>
        )}
        <div className={styles.topLink}>
          {mode === "word" ? (
            <Link href="/">{t("auth.wordHome")}</Link>
          ) : mode === "login" ? (
            <>
              {t("auth.noAccount")}{" "}
              <Link href="/register">
                {t("auth.signUp")} <span aria-hidden="true">↗</span>
              </Link>
            </>
          ) : (
            <>
              {t("auth.hasAccount")}{" "}
              <Link href="/login">
                {t("auth.signIn")} <span aria-hidden="true">↗</span>
              </Link>
            </>
          )}
        </div>
        <div className={styles.formContent}>{children}</div>
        <p className={styles.formFooter}>{t("auth.formFooter")}</p>
      </section>
    </main>
  );
}

export { styles as authStyles };
