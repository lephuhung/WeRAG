"use client";

/* SuperAdmin editor of one domain pack formula's wording: name, expression,
 * variable descriptions/units/sources, legal basis and worked examples.
 * The id, tool and variable names are fixed (the Go implementation is
 * bound to them) and the calculation itself never changes: the server runs
 * every example through the Go code and refuses an edit it does not
 * reproduce, listing the reasons. "Khôi phục bản gốc" drops the edit. */

import { useState, type FormEvent } from "react";
import { Modal } from "@/components/modal";
import { useT } from "@/lib/i18n";
import {
  formulaEditProblems,
  resetDomainPackFormula,
  updateDomainPackFormula,
  type DomainPackDetail,
  type DomainPackFormulaEdit,
  type DomainPackFormulaView,
} from "@/lib/api/domain-packs-admin";

type Pair = { key: string; value: string };
type ExampleDraft = { inputs: Record<string, string>; expected: Pair[] };
type Draft = {
  name: string;
  expression: string;
  variables: { name: string; description: string; unit: string; source: string }[];
  basis: { number: string; article: string }[];
  examples: ExampleDraft[];
};

const numText = (v: number | undefined) => (v === undefined || Number.isNaN(v) ? "" : String(v));

function draftFrom(f: DomainPackFormulaView | DomainPackFormulaEdit): Draft {
  return {
    name: f.name,
    expression: f.expression,
    variables: f.variables.map((v) => ({ name: v.name, description: v.description, unit: v.unit, source: v.source })),
    basis: f.basis.map((b) => ({ number: b.number, article: b.article ?? "" })),
    examples: f.examples.map((ex) => ({
      inputs: Object.fromEntries(f.variables.map((v) => [v.name, numText(ex.inputs[v.name])])),
      expected: Object.entries(ex.expected).map(([key, value]) => ({ key, value: numText(value) })),
    })),
  };
}

/* Vietnamese "500.000" / "1.234.567,5" and English "1,234.5" thousands
 * groups both read as numbers (a group never starts "0.", so "0.075" is a
 * decimal); otherwise a lone comma is the decimal mark ("1,5"). "" or
 * junk → null. */
function parseNumber(text: string): number | null {
  let s = text.trim().replace(/\s/g, "");
  if (!s) return null;
  if (/^-?[1-9]\d{0,2}(\.\d{3})+(,\d+)?$/.test(s)) s = s.replace(/\./g, "").replace(",", ".");
  else if (/^-?[1-9]\d{0,2}(,\d{3})+(\.\d+)?$/.test(s)) s = s.replace(/,/g, "");
  else if (/^-?\d+,\d+$/.test(s)) s = s.replace(",", ".");
  const n = Number(s);
  return Number.isFinite(n) ? n : null;
}

function editFrom(d: Draft): { edit?: DomainPackFormulaEdit; bad?: string } {
  const examples: DomainPackFormulaEdit["examples"] = [];
  for (const [i, ex] of d.examples.entries()) {
    const inputs: Record<string, number> = {};
    for (const [name, text] of Object.entries(ex.inputs)) {
      const n = parseNumber(text);
      if (n === null) return { bad: `#${i + 1} ${name}` };
      inputs[name] = n;
    }
    const expected: Record<string, number> = {};
    for (const p of ex.expected) {
      const key = p.key.trim();
      if (!key && !p.value.trim()) continue;
      const n = parseNumber(p.value);
      if (!key || n === null) return { bad: `#${i + 1} ${key || "→"}` };
      expected[key] = n;
    }
    examples.push({ inputs, expected });
  }
  return {
    edit: {
      name: d.name,
      expression: d.expression,
      variables: d.variables,
      basis: d.basis.filter((b) => b.number.trim()).map((b) => ({ number: b.number, article: b.article || undefined })),
      examples,
    },
  };
}

export function FormulaEditor({
  pack,
  formula,
  catalogueNumbers,
  onClose,
  onSaved,
}: {
  pack: string;
  formula: DomainPackFormulaView;
  catalogueNumbers: string[];
  onClose: () => void;
  onSaved: (detail: DomainPackDetail) => void;
}) {
  const { t } = useT();
  const [draft, setDraft] = useState<Draft>(() => draftFrom(formula));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [problems, setProblems] = useState<string[]>([]);
  const listId = `dpk-basis-${formula.id}`;

  const set = (patch: Partial<Draft>) => setDraft((d) => ({ ...d, ...patch }));
  const setExample = (i: number, ex: ExampleDraft) =>
    set({ examples: draft.examples.map((e, j) => (j === i ? ex : e)) });

  const fail = (e: unknown) => {
    setProblems(formulaEditProblems(e));
    setError(e instanceof Error && e.message ? e.message : t("dpk.edit.saveFailed"));
  };

  const save = async (e: FormEvent) => {
    e.preventDefault();
    const { edit, bad } = editFrom(draft);
    if (!edit) {
      setProblems([]);
      setError(t("dpk.edit.badNumber", { field: bad ?? "" }));
      return;
    }
    setBusy(true);
    setError("");
    setProblems([]);
    try {
      const res = await updateDomainPackFormula(pack, formula.id, edit);
      onSaved(res.data);
    } catch (err) {
      fail(err);
    } finally {
      setBusy(false);
    }
  };

  const reset = async () => {
    if (!window.confirm(t("dpk.edit.resetConfirm", { name: formula.shipped?.name ?? formula.name }))) return;
    setBusy(true);
    setError("");
    setProblems([]);
    try {
      const res = await resetDomainPackFormula(pack, formula.id);
      onSaved(res.data);
    } catch (err) {
      fail(err);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal open title={`${t("dpk.edit.title")} · ${formula.id}`} onClose={onClose} width="w-[760px]">
      <form className="space-y-5" onSubmit={save}>
        <p className="caption text-muted">{t("dpk.edit.hint", { tool: formula.tool })}</p>

        <label className="block">
          <span className="caption mb-1 block text-muted">{t("dpk.edit.name")}</span>
          <input className="input w-full" value={draft.name} onChange={(e) => set({ name: e.target.value })} required />
        </label>
        <label className="block">
          <span className="caption mb-1 block text-muted">{t("dpk.formula.expression")}</span>
          <textarea
            className="input min-h-[84px] w-full"
            value={draft.expression}
            onChange={(e) => set({ expression: e.target.value })}
            required
          />
          <span className="caption mt-1 block text-muted-soft">{t("dpk.edit.expressionHint")}</span>
        </label>

        <fieldset>
          <legend className="caption-uppercase mb-2 text-muted">{t("dpk.formula.variables")}</legend>
          <div className="space-y-2">
            {draft.variables.map((v, i) => (
              <div key={v.name} className="grid grid-cols-1 gap-2 sm:grid-cols-[150px_1fr_110px_130px] sm:items-center">
                <code className="break-all text-[13px] text-ink">{v.name}</code>
                <input
                  className="input"
                  aria-label={`${v.name} ${t("dpk.edit.description")}`}
                  placeholder={t("dpk.edit.description")}
                  value={v.description}
                  onChange={(e) => set({ variables: draft.variables.map((x, j) => (j === i ? { ...x, description: e.target.value } : x)) })}
                />
                <input
                  className="input"
                  aria-label={`${v.name} ${t("dpk.edit.unit")}`}
                  placeholder={t("dpk.edit.unit")}
                  value={v.unit}
                  onChange={(e) => set({ variables: draft.variables.map((x, j) => (j === i ? { ...x, unit: e.target.value } : x)) })}
                />
                <select
                  className="input"
                  aria-label={`${v.name} ${t("dpk.edit.source")}`}
                  value={v.source}
                  onChange={(e) => set({ variables: draft.variables.map((x, j) => (j === i ? { ...x, source: e.target.value } : x)) })}
                >
                  <option value="input">{t("dpk.formula.sourceInput")}</option>
                  <option value="kb">{t("dpk.formula.sourceKB")}</option>
                </select>
              </div>
            ))}
          </div>
        </fieldset>

        <fieldset>
          <legend className="caption-uppercase mb-2 text-muted">{t("dpk.formula.basis")}</legend>
          <datalist id={listId}>
            {catalogueNumbers.map((n) => (
              <option key={n} value={n} />
            ))}
          </datalist>
          <div className="space-y-2">
            {draft.basis.map((b, i) => (
              <div key={i} className="grid grid-cols-[1fr_1fr_auto] gap-2">
                <input
                  className="input"
                  list={listId}
                  placeholder={t("dpk.edit.number")}
                  value={b.number}
                  onChange={(e) => set({ basis: draft.basis.map((x, j) => (j === i ? { ...x, number: e.target.value } : x)) })}
                />
                <input
                  className="input"
                  placeholder={t("dpk.edit.article")}
                  value={b.article}
                  onChange={(e) => set({ basis: draft.basis.map((x, j) => (j === i ? { ...x, article: e.target.value } : x)) })}
                />
                <button type="button" className="btn btn-outline btn-sm" onClick={() => set({ basis: draft.basis.filter((_, j) => j !== i) })}>
                  {t("dpk.edit.remove")}
                </button>
              </div>
            ))}
            <button type="button" className="btn btn-outline btn-sm" onClick={() => set({ basis: [...draft.basis, { number: "", article: "" }] })}>
              + {t("dpk.edit.addBasis")}
            </button>
          </div>
        </fieldset>

        <fieldset>
          <legend className="caption-uppercase mb-2 text-muted">{t("dpk.formula.examples")}</legend>
          <p className="caption mb-2 text-muted-soft">{t("dpk.edit.examplesHint")}</p>
          <div className="space-y-3">
            {draft.examples.map((ex, i) => (
              <div key={i} className="rounded-lg border border-hairline p-3">
                <div className="mb-2 flex items-center justify-between">
                  <span className="caption font-medium text-ink">#{i + 1}</span>
                  <button
                    type="button"
                    className="btn btn-ghost btn-sm"
                    disabled={draft.examples.length <= 1}
                    onClick={() => set({ examples: draft.examples.filter((_, j) => j !== i) })}
                  >
                    {t("dpk.edit.remove")}
                  </button>
                </div>
                <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                  {draft.variables.map((v) => (
                    <label key={v.name} className="block">
                      <code className="caption mb-0.5 block text-muted">{v.name}</code>
                      <input
                        className="input w-full"
                        inputMode="decimal"
                        value={ex.inputs[v.name] ?? ""}
                        onChange={(e) => setExample(i, { ...ex, inputs: { ...ex.inputs, [v.name]: e.target.value } })}
                      />
                    </label>
                  ))}
                </div>
                <div className="caption mb-1 mt-3 text-muted">{t("dpk.edit.expected")}</div>
                <div className="space-y-2">
                  {ex.expected.map((p, k) => (
                    <div key={k} className="grid grid-cols-[1fr_1fr_auto] gap-2">
                      <input
                        className="input font-mono"
                        placeholder={t("dpk.edit.output")}
                        value={p.key}
                        onChange={(e) => setExample(i, { ...ex, expected: ex.expected.map((x, j) => (j === k ? { ...x, key: e.target.value } : x)) })}
                      />
                      <input
                        className="input"
                        inputMode="decimal"
                        placeholder={t("dpk.edit.value")}
                        value={p.value}
                        onChange={(e) => setExample(i, { ...ex, expected: ex.expected.map((x, j) => (j === k ? { ...x, value: e.target.value } : x)) })}
                      />
                      <button
                        type="button"
                        className="btn btn-ghost btn-sm"
                        onClick={() => setExample(i, { ...ex, expected: ex.expected.filter((_, j) => j !== k) })}
                      >
                        {t("dpk.edit.remove")}
                      </button>
                    </div>
                  ))}
                  <button
                    type="button"
                    className="btn btn-outline btn-sm"
                    onClick={() => setExample(i, { ...ex, expected: [...ex.expected, { key: "", value: "" }] })}
                  >
                    + {t("dpk.edit.addOutput")}
                  </button>
                </div>
              </div>
            ))}
            <button
              type="button"
              className="btn btn-outline btn-sm"
              onClick={() =>
                set({
                  examples: [
                    ...draft.examples,
                    {
                      inputs: Object.fromEntries(draft.variables.map((v) => [v.name, ""])),
                      expected: (draft.examples[0]?.expected ?? []).map((p) => ({ key: p.key, value: "" })),
                    },
                  ],
                })
              }
            >
              + {t("dpk.edit.addExample")}
            </button>
          </div>
        </fieldset>

        {error && (
          <div className="rounded-md border border-red-500/30 bg-red-500/5 px-3 py-2 text-[13px] text-red-700 dark:text-red-400">
            {problems.length > 0 ? (
              <>
                <div className="font-medium">{t("dpk.edit.refused")}</div>
                <ul className="mt-1 list-disc space-y-0.5 pl-4">
                  {problems.map((p) => (
                    <li key={p} className="break-words">
                      {p}
                    </li>
                  ))}
                </ul>
              </>
            ) : (
              error
            )}
          </div>
        )}

        <div className="flex flex-wrap items-center gap-2 border-t border-hairline pt-4">
          {(formula.edited || formula.edit_not_applied) && (
            <button type="button" className="btn btn-outline btn-sm text-rose-600" disabled={busy} onClick={() => void reset()}>
              {t("dpk.edit.reset")}
            </button>
          )}
          {formula.shipped && (
            <button type="button" className="btn btn-ghost btn-sm" disabled={busy} onClick={() => setDraft(draftFrom(formula.shipped!))}>
              {t("dpk.edit.loadShipped")}
            </button>
          )}
          <div className="ml-auto flex gap-2">
            <button type="button" className="btn btn-outline btn-sm" onClick={onClose} disabled={busy}>
              {t("dpk.edit.cancel")}
            </button>
            <button type="submit" className="btn btn-primary btn-sm" disabled={busy}>
              {busy ? t("dpk.edit.saving") : t("dpk.edit.save")}
            </button>
          </div>
        </div>
      </form>
    </Modal>
  );
}
