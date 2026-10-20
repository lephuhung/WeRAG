/* The knowledge-graph palette — the single source of truth for every
 * colour the graph canvas, its legend and the node-type maps use.
 *
 * The values live as CSS custom properties in graph-palette.module.css so
 * the dark mode and the Anthropic preset can override the whole set;
 * this module only names them and maps the graph's node types onto the
 * colour families. */

import "./graph-palette.module.css";

export type GraphFamily =
  | "slate"
  | "slateDeep"
  | "purple"
  | "sky"
  | "skyDeep"
  | "amber"
  | "rose"
  | "teal"
  | "emerald"
  | "indigo"
  | "red";

/** The edges (and the label halo) colour. */
export const GRAPH_EDGE_COLOR = "var(--graph-edge)";

/** Fill + stroke of every colour family. */
export const GRAPH_FAMILY: Record<GraphFamily, { fill: string; stroke: string }> = {
  slate: { fill: "var(--graph-fill-slate)", stroke: "var(--graph-stroke-slate)" },
  slateDeep: { fill: "var(--graph-fill-slate-deep)", stroke: "var(--graph-stroke-slate-deep)" },
  purple: { fill: "var(--graph-fill-purple)", stroke: "var(--graph-stroke-purple)" },
  sky: { fill: "var(--graph-fill-sky)", stroke: "var(--graph-stroke-sky)" },
  skyDeep: { fill: "var(--graph-fill-sky-deep)", stroke: "var(--graph-stroke-sky-deep)" },
  amber: { fill: "var(--graph-fill-amber)", stroke: "var(--graph-stroke-amber)" },
  rose: { fill: "var(--graph-fill-rose)", stroke: "var(--graph-stroke-rose)" },
  teal: { fill: "var(--graph-fill-teal)", stroke: "var(--graph-stroke-teal)" },
  emerald: { fill: "var(--graph-fill-emerald)", stroke: "var(--graph-stroke-emerald)" },
  indigo: { fill: "var(--graph-fill-indigo)", stroke: "var(--graph-stroke-indigo)" },
  red: { fill: "var(--graph-fill-red)", stroke: "var(--graph-stroke-red)" },
};
