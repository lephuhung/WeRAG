"use client";

import { useEffect, useRef, useState } from "react";
import { Orb } from "@/components/orb";
import { Toggle } from "@/components/settings/toggle";

const SWATCHES = [
  { name: "canvas", cls: "bg-canvas" },
  { name: "canvas-soft", cls: "bg-canvas-soft" },
  { name: "surface-card", cls: "bg-surface-card" },
  { name: "surface-strong", cls: "bg-surface-strong" },
  { name: "ink", cls: "bg-ink" },
  { name: "primary", cls: "bg-primary" },
  { name: "body", cls: "bg-body" },
  { name: "muted", cls: "bg-muted" },
  { name: "hairline", cls: "bg-hairline" },
  { name: "hairline-strong", cls: "bg-hairline-strong" },
];

const ORBS = ["mint", "peach", "lavender", "sky", "rose"] as const;

const TYPE_SCALE = [
  { cls: "display-mega" },
  { cls: "display-xl" },
  { cls: "display-lg" },
  { cls: "display-md" },
  { cls: "display-sm" },
  { cls: "title-md" },
  { cls: "title-sm" },
];

export default function DesignPage() {
  const [demoToggle, setDemoToggle] = useState(true);
  const rootRef = useRef<HTMLDivElement>(null);
  const [metrics, setMetrics] = useState<Record<string, string>>({});
  const [preset, setPreset] = useState("");
  const [colors, setColors] = useState<Record<string, string>>({});
  const [font, setFont] = useState("");
  useEffect(() => {
    const sync = () => {
      const root = document.documentElement;
      const style = getComputedStyle(root);
      setPreset((root.dataset.theme === "anthropic" ? "Anthropic" : "ElevenLabs") + (root.classList.contains("dark") ? " · Dark" : " · Light"));
      setColors(Object.fromEntries(SWATCHES.map(swatch => [swatch.name, style.getPropertyValue(`--color-${swatch.name}`).trim()])));
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
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ["class", "style", "data-theme"] });
    window.addEventListener("resize", sync);
    let active = true;
    void document.fonts.ready.then(() => { if (active) sync(); });
    return () => { active = false; observer.disconnect(); window.removeEventListener("resize", sync); };
  }, []);

  return (
    <div ref={rootRef} className="min-h-screen bg-canvas">
      <div className="mx-auto max-w-[1200px] px-4 py-8 sm:px-8 sm:py-12 lg:px-12 lg:py-16">
        <div className="caption-uppercase mb-3 text-muted">Design system</div>
        <h1 className="display-mega mb-4">{preset || "WeRAG design system"}</h1>
        <p className="mb-16 max-w-[560px] text-body">
          Giá trị màu và kiểu chữ đang áp dụng trên giao diện. Kích thước bên dưới cập nhật theo màn hình và tùy chọn cỡ chữ.
          <span className="caption mt-2 block text-muted">{font}</span>
        </p>

        {/* type scale */}
        <section className="mb-16">
          <h2 className="caption-uppercase mb-6 text-muted">Type scale</h2>
          <div className="card divide-y divide-hairline p-4 sm:p-8">
            {TYPE_SCALE.map((t) => (
              <div key={t.cls} className="flex min-w-0 flex-col gap-2 py-4 sm:flex-row sm:items-baseline sm:justify-between sm:gap-8">
                <span className={`${t.cls} min-w-0 break-words`} data-type={t.cls}>Tri thức dẫn lối tương lai</span>
                <span className="caption shrink-0 text-muted">{t.cls} · {metrics[t.cls] ?? "…"}</span>
              </div>
            ))}
            <div className="flex min-w-0 flex-col gap-2 py-4 sm:flex-row sm:items-baseline sm:justify-between sm:gap-8">
              <span className="text-[16px] text-body" data-type="body-md">
                Văn bản có dấu tiếng Việt — tài liệu, cơ sở tri thức và câu trả lời.
              </span>
              <span className="caption shrink-0 text-muted">body-md · {metrics["body-md"] ?? "…"}</span>
            </div>
          </div>
        </section>

        {/* colors */}
        <section className="mb-16">
          <h2 className="caption-uppercase mb-6 text-muted">Colors</h2>
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-5">
            {SWATCHES.map((s) => (
              <div key={s.name} className="card overflow-hidden">
                <div className={`h-20 ${s.cls} border-b border-hairline`} />
                <div className="p-3">
                  <div className="text-[13px] font-medium text-ink">{s.name}</div>
                  <div className="caption text-muted">{colors[s.name] || "…"}</div>
                </div>
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
            <span className="badge-pill">Badge</span>
            <span className="badge-pill">gpt-4o</span>
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
