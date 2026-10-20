"use client";

import Link from "next/link";
import { useT } from "@/lib/i18n";
import { BrandLockup, BrandLogo } from "@/components/brand-logo";
import { LandingAccount } from "./account";
import { LandingPricing } from "./pricing";
import styles from "./page.module.css";

export function Landing() {
  const { t } = useT();

  const features = [
    {
      number: "01",
      title: t("landing.feature1Title"),
      text: t("landing.feature1Text"),
      label: t("landing.feature1Label"),
    },
    {
      number: "02",
      title: t("landing.feature2Title"),
      text: t("landing.feature2Text"),
      label: t("landing.feature2Label"),
    },
    {
      number: "03",
      title: t("landing.feature3Title"),
      text: t("landing.feature3Text"),
      label: t("landing.feature3Label"),
    },
    {
      number: "04",
      title: t("landing.feature4Title"),
      text: t("landing.feature4Text"),
      label: t("landing.feature4Label"),
    },
    {
      number: "05",
      title: t("landing.feature5Title"),
      text: t("landing.feature5Text"),
      label: t("landing.feature5Label"),
    },
    {
      number: "06",
      title: t("landing.feature6Title"),
      text: t("landing.feature6Text"),
      label: t("landing.feature6Label"),
    },
  ];

  const steps = [
    { number: "01", title: t("landing.step1Title"), text: t("landing.step1Text") },
    { number: "02", title: t("landing.step2Title"), text: t("landing.step2Text") },
    { number: "03", title: t("landing.step3Title"), text: t("landing.step3Text") },
  ];

  const faqs = [
    { q: t("landing.faq1Q"), a: t("landing.faq1A") },
    { q: t("landing.faq2Q"), a: t("landing.faq2A") },
    { q: t("landing.faq3Q"), a: t("landing.faq3A") },
    { q: t("landing.faq4Q"), a: t("landing.faq4A") },
  ];

  return (
    <div className={styles.page} lang="vi">
      <a href="#noi-dung" className={styles.skip}>
        {t("landing.skipToContent")}
      </a>
      <header className={styles.header}>
        <Link href="/" className={styles.brand} aria-label={t("landing.brandAria")}>
          <BrandLockup size="lg" priority />
        </Link>
        <nav aria-label={t("landing.navAria")}>
          <a href="#tinh-nang">{t("landing.navFeatures")}</a>
          <a href="#cach-hoat-dong">{t("landing.navHow")}</a>
          <a href="#goi-cuoc">{t("landing.navPricing")}</a>
        </nav>
        <LandingAccount />
      </header>
      <main id="noi-dung">
        <section className={styles.hero}>
          <div>
            <p className={styles.eyebrow}>
              <span /> {t("landing.heroEyebrow")}
            </p>
            <h1>
              {t("landing.heroTitle1")}
              <br />
              <em>{t("landing.heroTitle2")}</em>
            </h1>
            <p className={styles.intro}>{t("landing.heroIntro")}</p>
            <div className={styles.actions}>
              <Link href="/register" className={styles.primary}>
                {t("landing.heroCta")} <span aria-hidden="true">↗</span>
              </Link>
              <a href="#goi-cuoc" className={styles.secondary}>
                {t("landing.heroSecondary")} <span aria-hidden="true">↓</span>
              </a>
            </div>
            <p className={styles.heroNote}>{t("landing.heroNote")}</p>
          </div>
          <div
            className={styles.preview}
            role="img"
            aria-label={t("landing.previewAria")}
          >
            <div className={styles.previewTop}>
              <BrandLogo size={24} alt="" />
              <span>{t("auth.tagline")}</span>
              <span className={styles.demoLabel}>{t("landing.demoLabel")}</span>
            </div>
            <div className={styles.previewBody}>
              <p className={styles.smallLabel}>{t("landing.sourceLabel")}</p>
              <div className={styles.files}>
                <span>{t("landing.sampleFile1")}</span>
                <span>{t("landing.sampleFile2")}</span>
              </div>
              <div className={styles.question}>{t("landing.sampleQuestion")}</div>
              <div className={styles.answer}>
                <span className={styles.answerIcon} aria-hidden="true">
                  ✳
                </span>
                <div>
                  <strong>{t("landing.answerTitle")}</strong>
                  <p>{t("landing.answerText")}</p>
                  <span className={styles.source}>{t("landing.answerSource")}</span>
                </div>
              </div>
              <div className={styles.prompt}>
                {t("landing.promptPlaceholder")} <span aria-hidden="true">↑</span>
              </div>
            </div>
            <div className={styles.previewFoot}>{t("landing.previewFoot")}</div>
          </div>
        </section>
        <div className={styles.strip}>
          <span>{t("landing.stripEyebrow")}</span>
          <p>
            {t("landing.strip1")} <b aria-hidden="true">✳</b> {t("landing.strip2")} <b aria-hidden="true">✳</b>{" "}
            {t("landing.strip3")}
          </p>
        </div>
        <section id="tinh-nang" className={styles.section}>
          <p className={styles.eyebrow}>{t("landing.featuresEyebrow")}</p>
          <div className={styles.sectionHead}>
            <h2>
              {t("landing.sectionTitle1")}
              <br />
              <em>{t("landing.sectionTitle2")}</em>
            </h2>
            <p>{t("landing.sectionLead")}</p>
          </div>
          <div className={styles.features}>
            {features.map((feature) => (
              <article key={feature.number}>
                <div className={styles.featureTop}>
                  <span>{feature.number}</span>
                  <span>{feature.label}</span>
                </div>
                <h3>{feature.title}</h3>
                <p>{feature.text}</p>
              </article>
            ))}
          </div>
        </section>
        <section id="cach-hoat-dong" className={`${styles.section} ${styles.workflow}`}>
          <p className={styles.eyebrow}>{t("landing.workflowEyebrow")}</p>
          <h2>
            {t("landing.workflowTitle1")}
            <br />
            <em>{t("landing.workflowTitle2")}</em>
          </h2>
          <div className={styles.steps}>
            {steps.map((step) => (
              <article key={step.number}>
                <span>{step.number} /</span>
                <h3>{step.title}</h3>
                <p>{step.text}</p>
              </article>
            ))}
          </div>
        </section>
        <section id="goi-cuoc" className={styles.section}>
          <p className={styles.eyebrow}>{t("landing.pricingEyebrow")}</p>
          <div className={styles.sectionHead}>
            <h2>
              {t("landing.pricingTitle1")}
              <br />
              <em>{t("landing.pricingTitle2")}</em>
            </h2>
            <p>{t("landing.pricingLead")}</p>
          </div>
          <LandingPricing />
        </section>
        <section className={`${styles.section} ${styles.faq}`}>
          <p className={styles.eyebrow}>{t("landing.faqEyebrow")}</p>
          <h2>{t("landing.faqTitle")}</h2>
          {faqs.map((faq) => (
            <details key={faq.q}>
              <summary>
                {faq.q}
                <span aria-hidden="true">+</span>
              </summary>
              <p>{faq.a}</p>
            </details>
          ))}
        </section>
        <section className={styles.cta}>
          <p className={styles.eyebrow}>{t("landing.ctaEyebrow")}</p>
          <h2>
            {t("landing.ctaTitle1")}
            <br />
            {t("landing.ctaTitle2")}
          </h2>
          <Link href="/register" className={styles.primary}>
            {t("landing.ctaButton")} <span aria-hidden="true">↗</span>
          </Link>
        </section>
      </main>
      <footer className={styles.footer}>
        <Link href="/" className={styles.brand}>
          <BrandLockup size="lg" />
        </Link>
        <span>{t("landing.footerTagline")}</span>
        <div>
          <a href="#goi-cuoc">{t("landing.navPricing")}</a>
          <Link href="/platform/creatChat">
            {t("landing.enterApp")} ↗
          </Link>
        </div>
      </footer>
    </div>
  );
}
