"use client";

/* SuperAdmin editor of one domain pack formula's wording: name, expression,
 * variable descriptions/units/sources, legal basis and worked examples.
 * The id, tool and variable names are fixed (the Go implementation is
 * bound to them) and the calculation itself never changes: the server runs
 * every example through the Go code and refuses an edit it does not
 * reproduce, listing the reasons. "Khôi phục bản gốc" drops the edit. */

import { useMemo, useState, type FormEvent } from "react";
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
import { useConfirm } from "@/components/confirm-dialog";
import { ConsolePage } from "@/components/settings/console-page";
import { FieldLabel } from "@/components/settings/field";
import { Notice } from "@/components/settings/notice";
import { useToast } from "@/components/toast";

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
  const confirmAction = useConfirm();
  const toast = useToast();
  const [draft, setDraft] = useState<Draft>(() => draftFrom(formula));
  const [saved, setSaved] = useState<Draft>(() => draftFrom(formula));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [problems, setProblems] = useState<string[]>([]);
  const listId = `dpk-basis-${formula.id}`;
  /* The editor saves exactly what changed: the save button stays disabled
   * until the draft differs from the loaded formula. */
  const dirty = useMemo(
    () => JSON.stringify(draft) !== JSON.stringify(saved),
    [draft, saved],
  );

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
      setSaved(draftFrom(edit));
      toast.success(t("common.saved"));
      onSaved(res.data);
    } catch (err) {
      fail(err);
    } finally {
      setBusy(false);
    }
  };

  const reset = async () => {
    const ok = await confirmAction({
      title: t("common.confirm"),
      message: t("dpk.edit.resetConfirm", { name: formula.shipped?.name ?? formula.name }),
      danger: true,
    });
    if (!ok) return;
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
      <ConsolePage size="form">
      <form className="space-y-5" onSubmit={save}>
        <p className="caption text-muted">{t("dpk.edit.hint", { tool: formula.tool })}</p>

        <div className="block">
          <FieldLabel htmlFor="dpk-name" required>{t("dpk.edit.name")}</FieldLabel>
          <input
            id="dpk-name"
            className="input w-full"
            value={draft.name}
            onChange={(e) => set({ name: e.target.value })}
            required
            aria-required="true"
          />
        </div>
        <div className="block">
          <FieldLabel htmlFor="dpk-expression" required>{t("dpk.formula.expression")}</FieldLabel>
          <textarea
            id="dpk-expression"
            className="input min-h-[84px] w-full"
            value={draft.expression}
            onChange={(e) => set({ expression: e.target.value })}
            required
            aria-required="true"
          />
          <span className="caption mt-1 block text-muted-soft">{t("dpk.edit.expressionHint")}</span>
        </div>

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
                  aria-label={t("dpk.edit.number")}
                  list={listId}
                  placeholder={t("dpk.edit.number")}
                  value={b.number}
                  onChange={(e) => set({ basis: draft.basis.map((x, j) => (j === i ? { ...x, number: e.target.value } : x)) })}
                />
                <input
                  className="input"
                  aria-label={t("dpk.edit.article")}
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
                    <div key={v.name} className="block">
                      <FieldLabel htmlFor={`dpk-ex-${i}-${v.name}`}>
                        <code className="caption text-muted">{v.name}</code>
                      </FieldLabel>
                      <input
                        id={`dpk-ex-${i}-${v.name}`}
                        className="input w-full"
                        inputMode="decimal"
                        value={ex.inputs[v.name] ?? ""}
                        onChange={(e) => setExample(i, { ...ex, inputs: { ...ex.inputs, [v.name]: e.target.value } })}
                      />
                    </div>
                  ))}
                </div>
                <div className="caption mb-1 mt-3 text-muted">{t("dpk.edit.expected")}</div>
                <div className="space-y-2">
                  {ex.expected.map((p, k) => (
                    <div key={k} className="grid grid-cols-[1fr_1fr_auto] gap-2">
                      <input
                        className="input font-mono"
                        aria-label={t("dpk.edit.output")}
                        placeholder={t("dpk.edit.output")}
                        value={p.key}
                        onChange={(e) => setExample(i, { ...ex, expected: ex.expected.map((x, j) => (j === k ? { ...x, key: e.target.value } : x)) })}
                      />
                      <input
                        className="input"
                        aria-label={t("dpk.edit.value")}
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
          <Notice tone="error" title={problems.length > 0 ? t("dpk.edit.refused") : undefined}>
            {problems.length > 0 ? (
              <ul className="mt-1 list-disc space-y-0.5 pl-4">
                {problems.map((p) => (
                  <li key={p} className="break-words">
                    {p}
                  </li>
                ))}
              </ul>
            ) : (
              error
            )}
          </Notice>
        )}

        <div className="flex flex-wrap items-center gap-2 border-t border-hairline pt-4">
          {(formula.edited || formula.edit_not_applied) && (
            <button type="button" className="btn btn-danger btn-sm" disabled={busy} onClick={() => void reset()}>
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
            <button type="submit" className="btn btn-primary btn-sm" disabled={!dirty || busy}>
              {busy ? t("dpk.edit.saving") : t("dpk.edit.save")}
            </button>
          </div>
        </div>
      </form>
      </ConsolePage>
    </Modal>
  );
}
