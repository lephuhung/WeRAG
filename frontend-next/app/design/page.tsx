import { Orb } from "@/components/orb";

const SWATCHES = [
  { name: "canvas", hex: "#f5f5f5", cls: "bg-canvas" },
  { name: "canvas-soft", hex: "#fafafa", cls: "bg-canvas-soft" },
  { name: "surface-card", hex: "#ffffff", cls: "bg-surface-card" },
  { name: "surface-strong", hex: "#f0efed", cls: "bg-surface-strong" },
  { name: "ink", hex: "#0c0a09", cls: "bg-ink" },
  { name: "primary", hex: "#292524", cls: "bg-primary" },
  { name: "body", hex: "#4e4e4e", cls: "bg-body" },
  { name: "muted", hex: "#777169", cls: "bg-muted" },
  { name: "hairline", hex: "#e7e5e4", cls: "bg-hairline" },
  { name: "hairline-strong", hex: "#d6d3d1", cls: "bg-hairline-strong" },
];

const ORBS = ["mint", "peach", "lavender", "sky", "rose"] as const;
const ORB_HEX: Record<(typeof ORBS)[number], string> = {
  mint: "#a7e5d3",
  peach: "#f4c5a8",
  lavender: "#c8b8e0",
  sky: "#a8c8e8",
  rose: "#e8b8c4",
};

const TYPE_SCALE = [
  { cls: "display-mega", label: "display-mega · 64/300" },
  { cls: "display-xl", label: "display-xl · 48/300" },
  { cls: "display-lg", label: "display-lg · 36/300" },
  { cls: "display-md", label: "display-md · 32/300" },
  { cls: "display-sm", label: "display-sm · 24/300" },
  { cls: "title-md", label: "title-md · 20/500" },
  { cls: "title-sm", label: "title-sm · 18/500" },
];

export default function DesignPage() {
  return (
    <div className="min-h-screen bg-canvas">
      <div className="mx-auto max-w-[1200px] px-12 py-16">
        <div className="caption-uppercase mb-3 text-muted">Design system</div>
        <h1 className="display-mega mb-4">ElevenLabs editorial</h1>
        <p className="mb-16 max-w-[560px] text-body">
          Off-white canvas, warm near-black ink, Waldenburg-style display at weight
          300, Inter body, pastel atmospheric orbs as the only color moments.
        </p>

        {/* type scale */}
        <section className="mb-16">
          <h2 className="caption-uppercase mb-6 text-muted">Type scale</h2>
          <div className="card divide-y divide-hairline p-8">
            {TYPE_SCALE.map((t) => (
              <div key={t.cls} className="flex items-baseline justify-between gap-8 py-4">
                <span className={t.cls}>The quick brown fox</span>
                <span className="caption shrink-0 text-muted-soft">{t.label}</span>
              </div>
            ))}
            <div className="flex items-baseline justify-between gap-8 py-4">
              <span className="text-[16px] text-body">
                Body — Inter 400 with +0.16px tracking, the editorial dialect for running text.
              </span>
              <span className="caption shrink-0 text-muted-soft">body-md · 16/400</span>
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
                  <div className="caption text-muted">{s.hex}</div>
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
              <span className="display-md">mint · peach · lavender · sky · rose</span>
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
