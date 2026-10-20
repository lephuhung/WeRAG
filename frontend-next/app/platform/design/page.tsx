"use client";

import { useEffect, useRef, useState } from "react";
import { Orb } from "@/components/orb";
import { Toggle } from "@/components/settings/toggle";

/* The token sheet of the app. It lives under /platform, so it is behind
 * the sign-in: it is an internal reference, not a public page. */

/* Every colour token of app/globals.css (@theme) — the list is read from
 * the stylesheet at runtime, so a new token only has to be added here. */
const SWATCHES = [
  "canvas",
  "canvas-soft",
  "canvas-deep",
  "surface-card",
  "surface-strong",
  "surface-dark",
  "surface-dark-elevated",
  "primary",
  "primary-active",
  "ink",
  "body",
  "body-strong",
  "muted",
  "muted-soft",
  "on-primary",
  "on-dark",
  "on-dark-soft",
  "hairline",
  "hairline-soft",
  "hairline-strong",
  "success",
  "error",
  "warning",
  "info",
  "accent",
  "running",
  "scrim",
  "orb-mint",
  "orb-peach",
  "orb-lavender",
  "orb-sky",
  "orb-rose",
] as const;

/* The translucent tints share the RGB of their tone — they are shown
 * over the canvas, not as a flat colour. */
const TINTS = [
  "success-surface",
  "error-surface",
  "warning-surface",
  "info-surface",
  "accent-surface",
] as const;

const RADII = ["xs", "sm2", "md2", "lg2", "xl2", "xxl"] as const;

const ORBS = ["mint", "peach", "lavender", "sky", "rose"] as const;

const TYPE_SCALE = [
  { cls: "display-mega" },
  { cls: "display-xl" },
  { cls: "display-lg" },
  { cls: "display-md" },
  { cls: "display-sm" },
  { cls: "title-md" },
  { cls: "title-sm" },
  { cls: "body-sm" },
  { cls: "caption" },
  { cls: "caption-uppercase" },
];

export default function DesignPage() {
  const [demoToggle, setDemoToggle] = useState(true);
  const rootRef = useRef<HTMLDivElement>(null);
  const [metrics, setMetrics] = useState<Record<string, string>>({});
  const [preset, setPreset] = useState("");
  const [colors, setColors] = useState<Record<string, string>>({});
  const [radii, setRadii] = useState<Record<string, string>>({});
  const [font, setFont] = useState("");
  /* Local preview of the theme presets — the page can show how the tokens
   * look in the Anthropic preset and in dark mode. */
  const [preview, setPreview] = useState<{ preset: "elevenlabs" | "anthropic"; dark: boolean }>({
    preset: "elevenlabs",
    dark: false,
  });

  useEffect(() => {
    const root = document.documentElement;
    const previousTheme = root.getAttribute("data-theme");
    const previousDark = root.classList.contains("dark");
    const apply = () => {
      if (preview.preset === "anthropic") root.setAttribute("data-theme", "anthropic");
      else root.removeAttribute("data-theme");
      root.classList.toggle("dark", preview.dark);
    };
    apply();
    /* The global theme manager applies its own preset after boot — the
     * preview has to win over it. */
    const timer = window.setTimeout(apply, 600);
    window.addEventListener("weknora:ui-theme-changed", apply);
    return () => {
      window.clearTimeout(timer);
      window.removeEventListener("weknora:ui-theme-changed", apply);
      if (previousTheme) root.setAttribute("data-theme", previousTheme);
      else root.removeAttribute("data-theme");
      root.classList.toggle("dark", previousDark);
    };
  }, [preview]);

  useEffect(() => {
    const sync = () => {
      const root = document.documentElement;
      const style = getComputedStyle(root);
      setPreset(
        (root.dataset.theme === "anthropic" ? "Anthropic" : "ElevenLabs") +
          (root.classList.contains("dark") ? " · Dark" : " · Light"),
      );
      setColors(
        Object.fromEntries(
          [...SWATCHES, ...TINTS].map((name) => [
            name,
            style.getPropertyValue(`--color-${name}`).trim(),
          ]),
        ),
      );
      setRadii(
        Object.fromEntries(
          RADII.map((name) => [name, style.getPropertyValue(`--radius-${name}`).trim()]),
        ),
      );
      const values: Record<string, string> = {};
      for (const element of rootRef.current?.querySelectorAll<HTMLElement>("[data-type]") ?? []) {
        const computed = getComputedStyle(element);
        values[element.dataset.type!] = `${computed.fontSize} / ${computed.fontWeight}`;
        setFont(computed.fontFamily);
      }
      setMetrics(values);
    };
    sync();
    const observer = new MutationObserver(sync);
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["class", "style", "data-theme"],
    });
    window.addEventListener("resize", sync);
    let active = true;
    void document.fonts.ready.then(() => {
      if (active) sync();
    });
    return () => {
      active = false;
      observer.disconnect();
      window.removeEventListener("resize", sync);
    };
  }, []);

  return (
    <div ref={rootRef} className="relative flex-1 overflow-y-auto bg-canvas">
      <div className="mx-auto max-w-[1200px] px-4 py-8 sm:px-8 sm:py-12 lg:px-12 lg:py-16">
        <div className="caption-uppercase mb-3 text-muted">Design system</div>
        <h1 className="display-mega mb-4">{preset || "WeRAG design system"}</h1>
        <p className="mb-10 max-w-[560px] text-body">
          Giá trị màu và kiểu chữ đang áp dụng trên giao diện. Kích thước bên dưới cập nhật theo màn hình và tùy chọn cỡ chữ.
          <span className="caption mt-2 block text-muted">{font}</span>
        </p>

        {/* preview switch */}
        <section className="card mb-16 flex flex-wrap items-center gap-6 p-6">
          <div className="flex items-center gap-3">
            <span className="caption text-muted">Theme</span>
            <div className="inline-flex gap-1 rounded-lg bg-surface-strong p-1">
              <button
                type="button"
                aria-pressed={preview.preset === "elevenlabs"}
                onClick={() => setPreview((value) => ({ ...value, preset: "elevenlabs" }))}
                className={`rounded-md px-4 py-2 text-[12px] ${
                  preview.preset === "elevenlabs"
                    ? "bg-surface-card text-ink shadow-soft-drop"
                    : "text-muted"
                }`}
              >
                ElevenLabs
              </button>
              <button
                type="button"
                aria-pressed={preview.preset === "anthropic"}
                onClick={() => setPreview((value) => ({ ...value, preset: "anthropic" }))}
                className={`rounded-md px-4 py-2 text-[12px] ${
                  preview.preset === "anthropic"
                    ? "bg-surface-card text-ink shadow-soft-drop"
                    : "text-muted"
                }`}
              >
                Anthropic
              </button>
            </div>
          </div>
          <div className="flex items-center gap-3">
            <Toggle
              checked={preview.dark}
              onChange={(dark) => setPreview((value) => ({ ...value, dark }))}
              label="Dark mode"
            />
            <span className="caption text-muted">{preview.dark ? "Dark" : "Light"}</span>
          </div>
          <p className="caption max-w-[320px] text-muted">
            Xem trước cục bộ: preset và chế độ tối chỉ áp dụng khi trang này đang mở.
          </p>
        </section>

        {/* type scale */}
        <section className="mb-16">
          <h2 className="caption-uppercase mb-6 text-muted">Type scale</h2>
          <div className="card divide-y divide-hairline p-4 sm:p-8">
            {TYPE_SCALE.map((t) => (
              <div
                key={t.cls}
                className="flex min-w-0 flex-col gap-2 py-4 sm:flex-row sm:items-baseline sm:justify-between sm:gap-8"
              >
                <span className={`${t.cls} min-w-0 break-words`} data-type={t.cls}>
                  Tri thức dẫn lối tương lai
                </span>
                <span className="caption shrink-0 text-muted">
                  {t.cls} · {metrics[t.cls] ?? "…"}
                </span>
              </div>
            ))}
          </div>
        </section>

        {/* colors */}
        <section className="mb-16">
          <h2 className="caption-uppercase mb-6 text-muted">Colors</h2>
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-5">
            {SWATCHES.map((name) => (
              <div key={name} className="card overflow-hidden">
                <div
                  className="h-20 border-b border-hairline"
                  style={{ background: `var(--color-${name})` }}
                />
                <div className="p-3">
                  <div className="text-[13px] font-medium text-ink">{name}</div>
                  <div className="caption text-muted">{colors[name] || "…"}</div>
                </div>
              </div>
            ))}
          </div>
        </section>

        {/* translucent tints */}
        <section className="mb-16">
          <h2 className="caption-uppercase mb-6 text-muted">Surface tints</h2>
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-5">
            {TINTS.map((name) => (
              <div key={name} className="card overflow-hidden">
                <div
                  className="h-20 border-b border-hairline"
                  style={{ background: `var(--color-${name})` }}
                />
                <div className="p-3">
                  <div className="text-[13px] font-medium text-ink">{name}</div>
                  <div className="caption text-muted">{colors[name] || "…"}</div>
                </div>
              </div>
            ))}
          </div>
        </section>

        {/* radii */}
        <section className="mb-16">
          <h2 className="caption-uppercase mb-6 text-muted">Radii</h2>
          <div className="card grid grid-cols-2 gap-4 p-6 sm:grid-cols-3 lg:grid-cols-6">
            {RADII.map((name) => (
              <div key={name}>
                <div
                  className="mb-2 h-16 w-full border border-hairline-strong bg-surface-strong"
                  style={{ borderRadius: `var(--radius-${name})` }}
                />
                <div className="text-[13px] font-medium text-ink">{name}</div>
                <div className="caption text-muted">{radii[name] || "…"}</div>
              </div>
            ))}
          </div>
        </section>

        {/* orbs */}
        <section className="mb-16">
          <h2 className="caption-uppercase mb-6 text-muted">Atmospheric orbs</h2>
          <div className="card relative h-[260px] overflow-hidden">
            {ORBS.map((o, i) => (
              <Orb
                key={o}
                color={o}
                size={280}
                style={{ left: `${8 + i * 18}%`, top: `${i % 2 ? -30 : 10}%` }}
              />
            ))}
            <div className="absolute inset-0 flex items-center justify-center">
              <span className="display-md px-4 text-center">mint · peach · lavender · sky · rose</span>
            </div>
          </div>
        </section>

        {/* buttons & inputs */}
        <section className="mb-16">
          <h2 className="caption-uppercase mb-6 text-muted">Controls</h2>
          <div className="card flex flex-wrap items-center gap-4 p-8">
            <button className="btn btn-primary">Primary pill</button>
            <button className="btn btn-outline">Outline pill</button>
            <button className="btn btn-tertiary">Tertiary text</button>
            <button className="btn btn-ghost">Ghost</button>
            <button className="btn btn-danger">Danger</button>
            <span className="badge-pill">Badge</span>
            <span className="badge-pill badge-success">success</span>
            <span className="badge-pill badge-error">error</span>
            <input className="input max-w-[280px]" placeholder="Text input…" />
            <div className="flex items-center gap-2">
              <Toggle checked={demoToggle} onChange={setDemoToggle} label="Interactive toggle" />
              <span className="caption text-muted">
                {demoToggle ? "Active (On)" : "Inactive (Off)"}
              </span>
            </div>
            <div className="flex items-center gap-2">
              <Toggle checked={false} disabled label="Disabled toggle" />
              <span className="caption text-muted">Disabled</span>
            </div>
          </div>
        </section>

        {/* cards */}
        <section className="mb-16">
          <h2 className="caption-uppercase mb-6 text-muted">Cards</h2>
          <div className="grid grid-cols-1 gap-5 md:grid-cols-3">
            <div className="card card-hover p-6">
              <h3 className="title-md">Feature card</h3>
              <p className="body-sm mt-2 text-body">
                White surface, hairline border, 16px radius, soft drop on hover.
              </p>
            </div>
            <div className="card relative overflow-hidden rounded-[24px] bg-canvas-soft p-8">
              <Orb color="mint" size={200} className="-right-10 -top-10" />
              <h3 className="display-sm relative">Gradient orb card</h3>
              <p className="body-sm relative mt-2 text-body">
                24px radius, orb purely as atmosphere.
              </p>
            </div>
            <div className="rounded-[16px] bg-surface-dark p-6 text-on-dark">
              <h3 className="title-md text-on-dark!">Dark card</h3>
              <p className="body-sm mt-2 text-on-dark-soft">
                Rare dark inversion — featured tiers, hero bands.
              </p>
              <button className="btn btn-outline mt-5 border-on-dark-soft! text-on-dark!">
                Action
              </button>
            </div>
          </div>
        </section>
      </div>
    </div>
  );
}
