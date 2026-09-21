"use client";

import { useCallback, useEffect, useState } from "react";
import { Modal } from "@/components/modal";
import { IconPlus, IconRefresh, IconTrash } from "@/components/icons";
import {
  listSkillCatalog,
  registerSkillCatalogFromFile,
  registerSkillCatalogFromSource,
  installSkillCatalog,
  deleteSkillCatalog,
  listCatalogSkillFiles,
  getCatalogSkillFile,
  type SkillCatalogItem,
} from "@/lib/api/skills";
import { listSandboxConfigs, type SandboxConfigRecord } from "@/lib/api/sandbox";
import { useAuth } from "@/lib/auth";

export function SkillsSettings() {
  const auth = useAuth();
  const activeTenantId = Number(auth.selectedTenantId ?? auth.tenant?.id ?? 0);
  const currentRole =
    auth.memberships.find((m) => String(m.tenant_id) === String(activeTenantId))?.role ?? "";
  const isSystemAdmin = auth.user?.is_system_admin === true;
  const canManage = currentRole === "owner" || isSystemAdmin;

  const [catalog, setCatalog] = useState<SkillCatalogItem[]>([]);
  const [sandboxes, setSandboxes] = useState<SandboxConfigRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");

  // Register Modal
  const [registerModalOpen, setRegisterModalOpen] = useState(false);
  const [regTab, setRegTab] = useState<"file" | "source">("file");
  const [selectedFile, setSelectedFile] = useState<File | null>(null);
  const [sourceUrl, setSourceUrl] = useState("");
  const [uploadProgress, setUploadProgress] = useState(0);
  const [registering, setRegistering] = useState(false);

  // Install Modal
  const [installSkill, setInstallSkill] = useState<SkillCatalogItem | null>(null);
  const [targetSandboxIds, setTargetSandboxIds] = useState<string[]>([]);
  const [installing, setInstalling] = useState(false);

  // Files Modal
  const [viewFilesSkill, setViewFilesSkill] = useState<SkillCatalogItem | null>(null);
  const [files, setFiles] = useState<{ path: string; size: number }[]>([]);
  const [selectedFilePath, setSelectedFilePath] = useState<string>("");
  const [fileContent, setFileContent] = useState<string>("");
  const [loadingFiles, setLoadingFiles] = useState(false);

  // Delete Confirm Modal
  const [deletingSkill, setDeletingSkill] = useState<SkillCatalogItem | null>(null);
  const [deletingBusy, setDeletingBusy] = useState(false);

  const loadData = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const [cRes, sRes] = await Promise.all([
        listSkillCatalog(),
        listSandboxConfigs().catch(() => ({ data: [] })),
      ]);
      setCatalog(cRes.data ?? []);
      setSandboxes(sRes.data ?? []);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load skills catalog");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadData();
  }, [loadData]);

  // Handle register
  const handleRegister = async (e: React.FormEvent) => {
    e.preventDefault();
    setRegistering(true);
    setError("");
    try {
      if (regTab === "file" && selectedFile) {
        await registerSkillCatalogFromFile(selectedFile, (p) => setUploadProgress(p));
        setSuccess(`Registered skill bundle: ${selectedFile.name}`);
      } else if (regTab === "source" && sourceUrl.trim()) {
        await registerSkillCatalogFromSource(sourceUrl.trim());
        setSuccess(`Registered skill from source`);
      }
      setRegisterModalOpen(false);
      setSelectedFile(null);
      setSourceUrl("");
      await loadData();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to register skill");
    } finally {
      setRegistering(false);
      setUploadProgress(0);
    }
  };

  // Handle install
  const handleInstall = async () => {
    if (!installSkill || targetSandboxIds.length === 0) return;
    setInstalling(true);
    setError("");
    try {
      await installSkillCatalog(installSkill.id, targetSandboxIds);
      setSuccess(`Installed ${installSkill.name} to selected sandboxes`);
      setInstallSkill(null);
      await loadData();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to install skill");
    } finally {
      setInstalling(false);
    }
  };

  // Handle open files viewer
  const handleOpenFiles = async (skill: SkillCatalogItem) => {
    setViewFilesSkill(skill);
    setLoadingFiles(true);
    setSelectedFilePath("");
    setFileContent("");
    try {
      const res = await listCatalogSkillFiles(skill.id);
      setFiles(res.data ?? []);
      if (res.data && res.data.length > 0) {
        const first = res.data[0].path;
        setSelectedFilePath(first);
        const cRes = await getCatalogSkillFile(skill.id, first);
        setFileContent(cRes.data?.content ?? "");
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to inspect skill bundle files");
    } finally {
      setLoadingFiles(false);
    }
  };

  // Handle file select in viewer
  const handleSelectFile = async (path: string) => {
    if (!viewFilesSkill) return;
    setSelectedFilePath(path);
    setLoadingFiles(true);
    try {
      const cRes = await getCatalogSkillFile(viewFilesSkill.id, path);
      setFileContent(cRes.data?.content ?? "");
    } catch (e) {
      setFileContent("Failed to load file contents");
    } finally {
      setLoadingFiles(false);
    }
  };

  // Handle delete
  const handleDelete = async () => {
    if (!deletingSkill) return;
    setDeletingBusy(true);
    setError("");
    try {
      await deleteSkillCatalog(deletingSkill.id);
      setSuccess(`Deleted skill ${deletingSkill.name}`);
      setDeletingSkill(null);
      await loadData();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to delete skill");
    } finally {
      setDeletingBusy(false);
    }
  };

  return (
    <div className="space-y-8">
      {/* Header */}
      <div className="flex flex-wrap items-center justify-between gap-4 border-b border-hairline pb-5">
        <div>
          <h2 className="title-md font-semibold text-ink">Skill Catalog</h2>
          <p className="caption text-muted mt-1">
            Registered tool packages and execution scripts that can be baked into sandbox runtime environments.
          </p>
        </div>

        <div className="flex items-center gap-2">
          {canManage && (
            <button
              type="button"
              onClick={() => setRegisterModalOpen(true)}
              className="btn btn-primary btn-sm flex items-center gap-1.5"
            >
              <IconPlus className="h-3.5 w-3.5" />
              <span>Register skill</span>
            </button>
          )}
          <button
            type="button"
            onClick={() => void loadData()}
            className="btn btn-outline btn-sm p-1.5"
            title="Refresh"
          >
            <IconRefresh className="h-3.5 w-3.5" />
          </button>
        </div>
      </div>

      {error && (
        <div className="rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700 dark:border-rose-900/50 dark:bg-rose-950/30 dark:text-rose-400">
          {error}
        </div>
      )}
      {success && (
        <div className="rounded-xl border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-700 dark:border-emerald-900/50 dark:bg-emerald-950/30 dark:text-emerald-400">
          {success}
        </div>
      )}

      {/* Catalog Grid */}
      {loading ? (
        <p className="caption text-muted text-center py-12">Loading skill catalog…</p>
      ) : catalog.length === 0 ? (
        <div className="rounded-xl border border-hairline py-16 text-center text-sm text-muted">
          No skills registered in catalog yet. Register a skill package (.zip or repository) to provide
          agent execution tools.
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          {catalog.map((sk) => (
            <div
              key={sk.id}
              className="card p-5 hover:border-ink/20 transition-all flex flex-col justify-between gap-4"
            >
              <div>
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <h3 className="text-base font-semibold text-ink leading-snug">
                      {sk.name}
                    </h3>
                    {sk.version && (
                      <span className="badge-pill text-[10.5px] mt-1 font-mono">
                        v{sk.version}
                      </span>
                    )}
                  </div>

                  {canManage && (
                    <div className="flex items-center gap-1">
                      <button
                        type="button"
                        onClick={() => void handleOpenFiles(sk)}
                        className="btn btn-outline btn-sm text-xs py-1"
                      >
                        Files
                      </button>
                      <button
                        type="button"
                        onClick={() => {
                          setInstallSkill(sk);
                          setTargetSandboxIds(
                            sk.installations?.map((i) => i.sandbox_config_id) ?? [],
                          );
                        }}
                        className="btn btn-outline btn-sm text-xs py-1"
                      >
                        Install
                      </button>
                      <button
                        type="button"
                        onClick={() => setDeletingSkill(sk)}
                        className="btn btn-ghost btn-sm p-1 text-muted hover:text-rose-600"
                        title="Delete skill"
                      >
                        <IconTrash className="h-3.5 w-3.5" />
                      </button>
                    </div>
                  )}
                </div>

                <p className="text-sm text-muted mt-2 line-clamp-3 leading-relaxed">
                  {sk.description || "No description provided for this skill."}
                </p>
              </div>

              {/* Installations */}
              <div className="border-t border-hairline pt-3 space-y-1.5 text-xs">
                <span className="caption font-medium text-muted block">Active Installations:</span>
                {!sk.installations || sk.installations.length === 0 ? (
                  <span className="caption text-muted-soft block">
                    Not installed in any sandbox configs.
                  </span>
                ) : (
                  <div className="flex flex-wrap gap-1.5">
                    {sk.installations.map((inst) => (
                      <span
                        key={inst.sandbox_config_id}
                        className={`badge-pill text-[11px] font-medium ${
                          inst.status === "installed"
                            ? "bg-emerald-500/10 text-emerald-700 border border-emerald-500/20"
                            : inst.status === "failed"
                            ? "bg-rose-500/10 text-rose-700 border border-rose-500/20"
                            : ""
                        }`}
                      >
                        {inst.sandbox_config_name || inst.sandbox_config_id} ({inst.status})
                      </span>
                    ))}
                  </div>
                )}
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Register Skill Modal */}
      <Modal
        open={registerModalOpen}
        title="Register Skill Package"
        onClose={() => setRegisterModalOpen(false)}
      >
        <div className="space-y-4">
          <div className="flex border-b border-hairline pb-2 gap-4">
            <button
              type="button"
              onClick={() => setRegTab("file")}
              className={`text-sm font-medium pb-2 transition-colors border-b-2 -mb-2.5 ${
                regTab === "file"
                  ? "border-brand text-brand font-semibold"
                  : "border-transparent text-muted hover:text-ink"
              }`}
            >
              Upload Archive (.zip)
            </button>
            <button
              type="button"
              onClick={() => setRegTab("source")}
              className={`text-sm font-medium pb-2 transition-colors border-b-2 -mb-2.5 ${
                regTab === "source"
                  ? "border-brand text-brand font-semibold"
                  : "border-transparent text-muted hover:text-ink"
              }`}
            >
              Repository URL
            </button>
          </div>

          <form onSubmit={handleRegister} className="space-y-4 pt-2">
            {regTab === "file" ? (
              <label className="block">
                <span className="caption mb-1.5 block text-muted">Skill Archive</span>
                <input
                  type="file"
                  accept=".zip"
                  required
                  className="input file:mr-3 file:py-1 file:px-2.5 file:rounded-md file:border-0 file:bg-surface-strong file:text-xs"
                  onChange={(e) => setSelectedFile(e.target.files?.[0] || null)}
                />
                <span className="caption text-muted block text-xs mt-1">
                  Must contain a valid skill descriptor (`SKILL.md` or metadata YAML).
                </span>
              </label>
            ) : (
              <label className="block">
                <span className="caption mb-1.5 block text-muted">Git Source Repository</span>
                <input
                  type="url"
                  required
                  placeholder="https://github.com/org/repo.git"
                  className="input"
                  value={sourceUrl}
                  onChange={(e) => setSourceUrl(e.target.value)}
                />
              </label>
            )}

            {uploadProgress > 0 && uploadProgress < 100 && (
              <div className="w-full bg-surface-strong rounded-full h-2">
                <div
                  className="bg-brand h-2 rounded-full transition-all"
                  style={{ width: `${uploadProgress}%` }}
                />
              </div>
            )}

            <div className="flex justify-end gap-3 pt-3">
              <button
                type="button"
                className="btn btn-outline"
                onClick={() => setRegisterModalOpen(false)}
              >
                Cancel
              </button>
              <button
                type="submit"
                disabled={registering || (regTab === "file" && !selectedFile) || (regTab === "source" && !sourceUrl.trim())}
                className="btn btn-primary"
              >
                {registering ? "Registering…" : "Register Skill"}
              </button>
            </div>
          </form>
        </div>
      </Modal>

      {/* Install Skill Modal */}
      <Modal
        open={installSkill !== null}
        title={`Install ${installSkill?.name || "Skill"}`}
        onClose={() => setInstallSkill(null)}
      >
        <div className="space-y-4">
          <p className="caption text-muted">
            Select the sandbox configurations where this skill will be baked in and made available
            to agents.
          </p>

          <div className="space-y-2 max-h-60 overflow-y-auto">
            {sandboxes.length === 0 ? (
              <p className="caption text-muted text-center py-4">
                No sandbox configurations found. Configure a sandbox first.
              </p>
            ) : (
              sandboxes.map((sb) => {
                const checked = targetSandboxIds.includes(sb.id);
                return (
                  <label
                    key={sb.id}
                    className="flex items-center gap-3 rounded-lg border border-hairline p-3 cursor-pointer hover:bg-surface-strong/40"
                  >
                    <input
                      type="checkbox"
                      checked={checked}
                      onChange={(e) => {
                        if (e.target.checked) {
                          setTargetSandboxIds([...targetSandboxIds, sb.id]);
                        } else {
                          setTargetSandboxIds(targetSandboxIds.filter((id) => id !== sb.id));
                        }
                      }}
                      className="rounded accent-brand"
                    />
                    <div>
                      <span className="font-medium text-sm text-ink block">{sb.name}</span>
                      <span className="caption text-muted text-xs block">
                        Type: {sb.sandbox_type}
                      </span>
                    </div>
                  </label>
                );
              })
            )}
          </div>

          <div className="flex justify-end gap-3 pt-2">
            <button
              type="button"
              className="btn btn-outline"
              onClick={() => setInstallSkill(null)}
            >
              Cancel
            </button>
            <button
              type="button"
              disabled={installing || targetSandboxIds.length === 0}
              onClick={() => void handleInstall()}
              className="btn btn-primary"
            >
              {installing ? "Installing…" : "Install to Selected"}
            </button>
          </div>
        </div>
      </Modal>

      {/* View Bundle Files Modal */}
      <Modal
        open={viewFilesSkill !== null}
        title={`Files in ${viewFilesSkill?.name || "Skill"}`}
        onClose={() => setViewFilesSkill(null)}
      >
        <div className="flex gap-4 h-96">
          <div className="w-1/3 border-r border-hairline pr-3 overflow-y-auto space-y-1">
            {files.map((f) => (
              <button
                key={f.path}
                type="button"
                onClick={() => void handleSelectFile(f.path)}
                className={`w-full text-left px-2.5 py-1.5 rounded text-xs truncate transition-colors ${
                  selectedFilePath === f.path
                    ? "bg-brand text-white font-medium"
                    : "text-muted hover:bg-surface-strong hover:text-ink"
                }`}
              >
                {f.path}
              </button>
            ))}
          </div>

          <div className="w-2/3 overflow-auto bg-surface-strong/30 rounded-lg p-3 font-mono text-xs text-ink whitespace-pre-wrap">
            {loadingFiles ? (
              <span className="text-muted">Loading file content…</span>
            ) : (
              fileContent || "Empty file or binary content"
            )}
          </div>
        </div>
      </Modal>

      {/* Delete Skill Modal */}
      <Modal
        open={deletingSkill !== null}
        title="Delete Skill"
        onClose={() => setDeletingSkill(null)}
      >
        <div className="space-y-4">
          <p className="body-sm text-body">
            Are you sure you want to delete <strong>{deletingSkill?.name}</strong> from the catalog?
            Active sandboxes will retain their current container image until rebuilt.
          </p>
          <div className="flex justify-end gap-3 pt-2">
            <button
              type="button"
              className="btn btn-outline"
              onClick={() => setDeletingSkill(null)}
            >
              Cancel
            </button>
            <button
              type="button"
              disabled={deletingBusy}
              onClick={() => void handleDelete()}
              className="btn btn-primary bg-rose-600 hover:bg-rose-700 text-white"
            >
              {deletingBusy ? "Deleting…" : "Delete Skill"}
            </button>
          </div>
        </div>
      </Modal>
    </div>
  );
}
