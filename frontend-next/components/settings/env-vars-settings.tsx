"use client";

import { useEffect, useState } from "react";
import {
  listMyEnvVars,
  setMySandboxEnv,
  deleteMySandboxEnv,
  setMySkillEnv,
  deleteMySkillEnv,
  type ConfigEnvGroup,
  type EnvVarView,
} from "@/lib/api/env-vars";
import { ApiError } from "@/lib/api-client";
import { IconSettings, IconTrash } from "@/components/icons";
import { useT } from "@/lib/i18n";
import { useConfirm } from "@/components/confirm-dialog";
import { Modal } from "@/components/modal";
import { Notice } from "@/components/settings/notice";
import { SectionHeader } from "@/components/settings/section-header";
import { Loading } from "@/components/settings/loading";
import { DeniedView } from "@/components/settings/system-section";
import { FieldLabel, RequiredMark } from "@/components/settings/field";
import { ROW_HOVER } from "@/components/settings/data-table";
import { ConsolePage } from "@/components/settings/console-page";
import { useToast } from "@/components/toast";

export function EnvVarsSettings() {
  const { t } = useT();
  const toast = useToast();
  const confirmAction = useConfirm();
  const [groups, setGroups] = useState<ConfigEnvGroup[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [denied, setDenied] = useState(false);
  const [editingVar, setEditingVar] = useState<{
    type: "sandbox" | "skill";
    targetId: string;
    name: string;
    value: string;
  } | null>(null);
  const [saving, setSaving] = useState(false);
  const [formError, setFormError] = useState("");

  const loadData = async () => {
    setLoading(true);
    setError("");
    try {
      const res = await listMyEnvVars();
      setGroups(res.data ?? []);
      setDenied(false);
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) {
        setDenied(true);
        setGroups([]);
      } else {
        setError(e instanceof Error ? e.message : t("envv.loadFailed"));
      }
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void loadData();
  }, []);

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!editingVar) return;
    setFormError("");
    setSaving(true);
    try {
      if (editingVar.type === "sandbox") {
        await setMySandboxEnv(editingVar.targetId, editingVar.name, editingVar.value);
      } else {
        await setMySkillEnv(editingVar.targetId, editingVar.name, editingVar.value);
      }
      setEditingVar(null);
      await loadData();
      toast.success(t("common.saved"));
    } catch (err) {
      if (err instanceof ApiError && err.status === 403) {
        setDenied(true);
        setEditingVar(null);
        return;
      }
      setFormError(err instanceof Error ? err.message : t("envv.updateFailed"));
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async (type: "sandbox" | "skill", targetId: string, name: string) => {
    if (!(await confirmAction({ title: t("common.delete"), message: t("envv.confirmClear", { name }), danger: true })))
      return;
    try {
      if (type === "sandbox") {
        await deleteMySandboxEnv(targetId, name);
      } else {
        await deleteMySkillEnv(targetId, name);
      }
      await loadData();
      toast.success(t("common.saved"));
    } catch (err) {
      if (err instanceof ApiError && err.status === 403) {
        setDenied(true);
        return;
      }
      setError(err instanceof Error ? err.message : t("envv.clearFailed"));
    }
  };

  if (denied) {
    return <DeniedView />;
  }

  return (
    <ConsolePage size="form">
      <div>
        <SectionHeader title={t("envv.title")} subtitle={t("envv.subtitle")} />

        {error && (
          <Notice
            tone="error"
            className="mt-4"
            action={
              <button type="button" className="btn btn-outline btn-sm" onClick={() => void loadData()}>
                {t("common.retry")}
              </button>
            }
          >
            {error}
          </Notice>
        )}

        {loading ? (
          <Loading label={t("envv.loading")} className="py-12" />
        ) : groups.length === 0 ? (
          <div className="py-12 text-center text-muted">
            <IconSettings className="mx-auto mb-2 h-8 w-8 text-muted-soft" />
            <p className="text-[14px]">{t("envv.empty")}</p>
          </div>
        ) : (
          <div className="mt-6 space-y-8">
            {groups.map((group) => (
              <div key={group.sandbox_config_id} className="space-y-4">
                <div>
                  <h3 className="text-[16px] font-semibold text-ink">
                    {group.sandbox_config_name}
                  </h3>
                  {group.description && (
                    <p className="caption text-muted">{group.description}</p>
                  )}
                </div>

                {group.vars.length > 0 && (
                  <div className="rounded-xl border border-hairline bg-surface-card p-4 space-y-3">
                    <div className="caption-uppercase text-muted-soft">{t("envv.sandboxVars")}</div>
                    <div className="divide-y divide-hairline">
                      {group.vars.map((v) => (
                        <VarRow
                          key={v.name}
                          item={v}
                          onEdit={() =>
                            setEditingVar({
                              type: "sandbox",
                              targetId: group.sandbox_config_id,
                              name: v.name,
                              value: "",
                            })
                          }
                          onDelete={() =>
                            handleDelete("sandbox", group.sandbox_config_id, v.name)
                          }
                        />
                      ))}
                    </div>
                  </div>
                )}

                {group.skills.map((s) => (
                  <div
                    key={s.skill_id}
                    className="rounded-xl border border-hairline bg-surface-card p-4 space-y-3"
                  >
                    <div className="flex items-center justify-between">
                      <span className="text-[14px] font-medium text-ink">{s.skill_name}</span>
                      {s.description && (
                        <span className="caption text-muted">{s.description}</span>
                      )}
                    </div>
                    <div className="divide-y divide-hairline">
                      {s.vars.map((v) => (
                        <VarRow
                          key={v.name}
                          item={v}
                          onEdit={() =>
                            setEditingVar({
                              type: "skill",
                              targetId: s.skill_id,
                              name: v.name,
                              value: "",
                            })
                          }
                          onDelete={() => handleDelete("skill", s.skill_id, v.name)}
                        />
                      ))}
                    </div>
                  </div>
                ))}
              </div>
            ))}
          </div>
        )}

        {/* Edit Var Modal */}
        <Modal
          open={editingVar !== null}
          title={editingVar ? t("envv.configureTitle", { name: editingVar.name }) : ""}
          onClose={() => setEditingVar(null)}
          width="w-[440px]"
        >
          {editingVar && (
            <>
              <p className="caption mb-4 text-muted">
                {t("envv.configureDesc")}
              </p>
              <form onSubmit={handleSave} className="space-y-4">
                {formError && (
                  <Notice tone="error" className="mb-4">
                    {formError}
                  </Notice>
                )}
                <div>
                  <FieldLabel htmlFor="env-var-value" required>
                    {t("envv.value")}
                  </FieldLabel>
                  <input
                    id="env-var-value"
                    required
                    aria-required="true"
                    type="password"
                    className="input h-9 w-full text-[13px]"
                    placeholder={t("envv.valuePh")}
                    value={editingVar.value}
                    onChange={(e) =>
                      setEditingVar({ ...editingVar, value: e.target.value })
                    }
                  />
                </div>

                <div className="flex justify-end gap-2 border-t border-hairline pt-4">
                  <button
                    type="button"
                    onClick={() => setEditingVar(null)}
                    className="btn btn-outline btn-sm"
                  >
                    {t("common.cancel")}
                  </button>
                  <button
                    type="submit"
                    disabled={saving}
                    className="btn btn-primary btn-sm"
                  >
                    {saving ? t("memp.saving") : t("envv.save")}
                  </button>
                </div>
              </form>
            </>
          )}
        </Modal>
      </div>
    </ConsolePage>
  );
}

function VarRow({
  item,
  onEdit,
  onDelete,
}: {
  item: EnvVarView;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const { t } = useT();
  const srcKey =
    item.source === "user" ? "envv.srcUser" : item.source === "workspace" ? "envv.srcWorkspace" : "envv.srcMissing";
  return (
    <div className={`flex items-center justify-between py-2.5 ${ROW_HOVER}`}>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="font-mono text-[13px] font-semibold text-ink">{item.name}</span>
          <span
            className={`badge-pill text-[10px] ${
              item.source === "user"
                ? "bg-accent-surface text-accent"
                : item.source === "workspace"
                ? "bg-surface-strong text-muted"
                : "bg-error-surface text-error"
            }`}
          >
            {t(srcKey)}
          </span>
          {item.required && (
            <span className="caption text-error">
              <RequiredMark />
              {t("envv.required")}
            </span>
          )}
        </div>
        {item.description && (
          <p className="caption mt-0.5 text-muted">{item.description}</p>
        )}
      </div>

      <div className="flex items-center gap-2">
        <button type="button" onClick={onEdit} className="btn btn-outline btn-sm">
          {item.source === "user" ? t("envv.update") : t("envv.set")}
        </button>
        {item.source === "user" && (
          <button
            type="button"
            onClick={onDelete}
            className="btn btn-danger btn-sm p-1.5"
            title={t("envv.clearTip")}
            aria-label={t("envv.clearTip")}
          >
            <IconTrash className="h-4 w-4" />
          </button>
        )}
      </div>
    </div>
  );
}
