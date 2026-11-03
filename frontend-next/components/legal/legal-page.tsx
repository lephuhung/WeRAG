"use client";

import Link from "next/link";
import { useEffect } from "react";
import { useT, type Locale } from "@/lib/i18n";
import { BrandLockup } from "@/components/brand-logo";
import { LEGAL_OPERATOR, legalDocument, type LegalPageKey } from "@/lib/legal-content";

const LINKS: { page: LegalPageKey; href: string; label: string }[] = [
  { page: "privacy", href: "/chinh-sach-bao-mat", label: "legal.navPrivacy" },
  { page: "terms", href: "/dieu-khoan", label: "legal.navTerms" },
  { page: "support", href: "/ho-tro", label: "legal.navSupport" },
];

/**
 * Privacy, terms and support: public pages, readable without signing in,
 * with a vi/en switch (Microsoft's reviewers read the English one;
 * `?lang=en` opens it directly).
 */
export function LegalPage({ page }: { page: LegalPageKey }) {
  const { t, locale, setLocale } = useT();

  useEffect(() => {
    const lang = new URLSearchParams(window.location.search).get("lang");
    if (lang === "en" || lang === "vi") setLocale(lang, false);
  }, [setLocale]);

  const doc = legalDocument(page, locale);
  const otherLocale: Locale = locale === "vi" ? "en" : "vi";

  return (
    <div className="min-h-screen bg-[var(--color-canvas)] text-[var(--color-ink)]">
      <header className="mx-auto flex max-w-3xl items-center justify-between gap-4 px-6 py-6">
        <Link href="/gioi-thieu" aria-label="weRAG">
          <BrandLockup size="md" />
        </Link>
        <button
          type="button"
          onClick={() => setLocale(otherLocale)}
          className="rounded-md border border-[var(--color-hairline-strong)] px-3 py-1.5 text-xs font-medium hover:bg-[var(--color-surface-strong)]"
        >
          {otherLocale === "en" ? "English" : "Tiếng Việt"}
        </button>
      </header>
      <nav className="mx-auto flex max-w-3xl flex-wrap gap-2 px-6" aria-label={t("legal.navAria")}>
        {LINKS.map((link) => (
          <Link
            key={link.page}
            href={link.href}
            aria-current={link.page === page ? "page" : undefined}
            className={`rounded-md px-3 py-1.5 text-sm ${
              link.page === page
                ? "bg-[var(--color-surface-strong)] font-semibold"
                : "text-[var(--color-muted)] hover:text-[var(--color-ink)]"
            }`}
          >
            {t(link.label as Parameters<typeof t>[0])}
          </Link>
        ))}
      </nav>
      <main className="mx-auto max-w-3xl px-6 pb-16 pt-8">
        <h1 className="text-3xl font-semibold">{doc.title}</h1>
        <p className="mt-2 text-xs text-[var(--color-muted)]">
          {t("legal.updated", { date: LEGAL_OPERATOR.updated })}
        </p>
        <p className="mt-6 leading-7 text-[var(--color-body)]">{doc.intro}</p>
        {doc.sections.map((section) => (
          <section key={section.heading} className="mt-8">
            <h2 className="text-lg font-semibold">{section.heading}</h2>
            {section.paragraphs?.map((text) => (
              <p key={text} className="mt-3 leading-7 text-[var(--color-body)]">
                {text}
              </p>
            ))}
            {section.bullets && (
              <ul className="mt-3 list-disc space-y-2 pl-6 leading-7 text-[var(--color-body)]">
                {section.bullets.map((text) => (
                  <li key={text}>{text}</li>
                ))}
              </ul>
            )}
          </section>
        ))}
      </main>
    </div>
  );
}
