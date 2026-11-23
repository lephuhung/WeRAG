/* The one table skin for the settings/admin surfaces.
 *
 * Every admin table used to hand-roll its own <table> markup with four
 * different header/row/empty styles. This component owns the canonical
 * skin (card wrapper, hairline header, hover row, empty + loading rows)
 * while each caller keeps its own columns, cells and actions.
 */
"use client";

import { Fragment, type ReactNode } from "react";
import { useT } from "@/lib/i18n";

/* One hover treatment for every table row (and every list row that wants
 * to look like one). */
export const ROW_HOVER = "transition-colors hover:bg-surface-strong/20";

export interface DataTableColumn {
  key: string;
  header: ReactNode;
  /* Extra classes for the <th> (width, alignment, padding tweaks). */
  className?: string;
  /* Right-aligned header (actions column). */
  align?: "left" | "right";
}

export function DataTable<T>({
  columns,
  rows,
  rowKey,
  renderRow,
  loading = false,
  loadingLabel,
  empty,
  emptyLabel,
  minWidth = "min-w-[900px]",
  fixedLayout = false,
  dense = false,
  onRowClick,
  rowClassName = "",
  caption,
  renderGroupRow,
}: {
  columns: DataTableColumn[];
  rows: T[];
  rowKey: (row: T) => string;
  /* Returns the <td> cells of one row (without the <tr>). */
  renderRow: (row: T) => ReactNode;
  loading?: boolean;
  loadingLabel?: string;
  /* Empty-state node; falls back to `emptyLabel`. */
  empty?: ReactNode;
  emptyLabel?: string;
  minWidth?: string;
  /* Fixed layout: column widths come only from the `className` widths below,
   * so a long text column keeps its share instead of being squeezed by the
   * content of its neighbours. Give every column but one a width. */
  fixedLayout?: boolean;
  /* Tighter cell padding, for tables with many narrow columns. Rows must use
   * the same `px-3 py-2.5` as the header. */
  dense?: boolean;
  onRowClick?: (row: T) => void;
  rowClassName?: string;
  caption?: ReactNode;
  /* Optional full-width row rendered *before* the row (a group heading).
   * It must render a complete <tr>. */
  renderGroupRow?: (row: T) => ReactNode;
}) {
  const showEmpty = !loading && rows.length === 0;
  const { t } = useT();
  const pad = dense ? "px-3 py-2.5" : "px-5 py-3";
  return (
    <div className="card overflow-x-auto shadow-sm">
      <table className={`w-full ${minWidth} ${fixedLayout ? "table-fixed" : ""} border-collapse text-left`}>
        {caption ? <caption className="sr-only">{caption}</caption> : null}
        <thead>
          <tr className="caption-uppercase border-b border-hairline bg-surface-strong/30 text-muted">
            {columns.map((c) => (
              <th
                key={c.key}
                scope="col"
                className={`${pad} font-medium ${c.align === "right" ? "text-right" : ""} ${c.className ?? ""}`}
              >
                {c.header}
              </th>
            ))}
          </tr>
        </thead>
        <tbody className="divide-y divide-hairline">
          {rows.map((row) => (
            <Fragment key={rowKey(row)}>
              {renderGroupRow?.(row)}
              <tr
                onClick={onRowClick ? () => onRowClick(row) : undefined}
                className={`${onRowClick ? "cursor-pointer " : ""}${ROW_HOVER} ${rowClassName}`}
              >
                {renderRow(row)}
              </tr>
            </Fragment>
          ))}
          {loading && (
            <tr>
              <td colSpan={columns.length} className="px-5 py-12 text-center text-[14px] text-muted">
                {loadingLabel ?? t("common.loading")}
              </td>
            </tr>
          )}
          {showEmpty && (
            <tr>
              <td colSpan={columns.length} className="px-5 py-12 text-center text-[14px] text-muted">
                {empty ?? emptyLabel ?? "—"}
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  );
}

export default DataTable;
