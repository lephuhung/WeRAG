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
import { IconSettings, IconTrash } from "@/components/icons";

export function EnvVarsSettings() {
  const [groups, setGroups] = useState<ConfigEnvGroup[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [editingVar, setEditingVar] = useState<{
    type: "sandbox" | "skill";
    targetId: string;
    name: string;
    value: string;
  } | null>(null);
  const [saving, setSaving] = useState(false);

  const loadData = async () => {
    setLoading(true);
    setError("");
    try {
      const res = await listMyEnvVars();
      setGroups(res.data ?? []);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load environment variables");
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
    setSaving(true);
    try {
      if (editingVar.type === "sandbox") {
        await setMySandboxEnv(editingVar.targetId, editingVar.name, editingVar.value);
      } else {
        await setMySkillEnv(editingVar.targetId, editingVar.name, editingVar.value);
      }
      setEditingVar(null);
      await loadData();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to update variable");
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async (type: "sandbox" | "skill", targetId: string, name: string) => {
    if (!confirm(`Clear your custom value for ${name}?`)) return;
    try {
      if (type === "sandbox") {
        await deleteMySandboxEnv(targetId, name);
      } else {
        await deleteMySkillEnv(targetId, name);
      }
      await loadData();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to clear variable");
    }
  };

  return (
    <div>
      <div className="border-b border-hairline pb-4">
        <h2 className="title-md">Environment Variables</h2>
        <p className="caption mt-1 text-muted">
          Manage your personal credentials and execution variables for sandboxes and skills. User values override workspace defaults.
        </p>
      </div>

      {error && (
        <div className="mt-4 rounded-xl bg-error/10 p-3 text-[13px] text-error">
          {error}
        </div>
      )}

      {loading ? (
        <div className="py-12 text-center text-muted">Loading environment variables…</div>
      ) : groups.length === 0 ? (
        <div className="py-12 text-center text-muted">
          <IconSettings className="mx-auto mb-2 h-8 w-8 text-muted-soft" />
          <p className="text-[14px]">No environment variables required or configured.</p>
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
                <div className="rounded-xl border border-hairline bg-surface p-4 space-y-3">
                  <div className="caption-uppercase text-muted-soft">Sandbox Variables</div>
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
                  className="rounded-xl border border-hairline bg-surface p-4 space-y-3"
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
      {editingVar && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4 backdrop-blur-sm">
          <div className="w-full max-w-[440px] rounded-2xl border border-hairline bg-surface-card p-6 shadow-2xl">
            <h3 className="title-sm mb-2">Configure {editingVar.name}</h3>
            <p className="caption mb-4 text-muted">
              Enter your personal value. It will be stored securely and only used during your own executions.
            </p>
            <form onSubmit={handleSave} className="space-y-4">
              <div>
                <label className="caption mb-1 block font-medium text-ink">Value</label>
                <input
                  required
                  type="password"
                  className="input h-9 w-full text-[13px]"
                  placeholder="Enter secret or credential"
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
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={saving}
                  className="btn btn-primary btn-sm"
                >
                  {saving ? "Saving…" : "Save Value"}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
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
  return (
    <div className="flex items-center justify-between py-2.5">
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="font-mono text-[13px] font-semibold text-ink">{item.name}</span>
          <span
            className={`badge-pill text-[10px] ${
              item.source === "user"
                ? "bg-accent/15 text-accent"
                : item.source === "workspace"
                ? "bg-surface-strong text-muted"
                : "bg-error/15 text-error"
            }`}
          >
            {item.source}
          </span>
          {item.required && <span className="caption text-error">*required</span>}
        </div>
        {item.description && (
          <p className="caption mt-0.5 text-muted">{item.description}</p>
        )}
      </div>

      <div className="flex items-center gap-2">
        <button onClick={onEdit} className="btn btn-outline btn-sm">
          {item.source === "user" ? "Update" : "Set"}
        </button>
        {item.source === "user" && (
          <button
            onClick={onDelete}
            className="rounded-lg p-1.5 text-muted hover:bg-surface-strong hover:text-error"
            title="Clear my custom value"
          >
            <IconTrash className="h-4 w-4" />
          </button>
        )}
      </div>
    </div>
  );
}
