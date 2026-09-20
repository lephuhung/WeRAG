"use client";

import { useEffect, useState } from "react";
import { IconRefresh, IconPlus } from "@/components/icons";
import {
  checkOllamaStatus,
  listOllamaModels,
  downloadOllamaModel,
  type OllamaModelInfo,
} from "@/lib/api/initialization";

export function OllamaSettings() {
  const [running, setRunning] = useState<boolean | null>(null);
  const [url, setUrl] = useState<string>("http://localhost:11434");
  const [version, setVersion] = useState<string>("");
  const [models, setModels] = useState<OllamaModelInfo[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [pullModelName, setPullModelName] = useState("");
  const [pulling, setPulling] = useState(false);
  const [pullMsg, setPullMsg] = useState("");

  const refresh = async () => {
    setBusy(true);
    setError("");
    try {
      const st = await checkOllamaStatus();
      setRunning(st.available);
      if (st.baseUrl) setUrl(st.baseUrl);
      if (st.version) setVersion(st.version);

      if (st.available) {
        const m = await listOllamaModels();
        setModels(m);
      } else {
        setModels([]);
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to connect to Ollama");
      setRunning(false);
    } finally {
      setBusy(false);
    }
  };

  useEffect(() => {
    void refresh();
  }, []);

  const handlePull = async () => {
    if (!pullModelName.trim()) return;
    setPulling(true);
    setPullMsg("Pull request initiated…");
    try {
      await downloadOllamaModel(pullModelName.trim());
      setPullMsg(`Downloading ${pullModelName} in background. Check Ollama terminal for progress.`);
      setPullModelName("");
      setTimeout(() => void refresh(), 3000);
    } catch (e) {
      setPullMsg(e instanceof Error ? e.message : "Pull failed");
    } finally {
      setPulling(false);
    }
  };

  return (
    <div>
      <div className="mb-6 flex items-center justify-between">
        <div>
          <h3 className="title-md text-ink">Ollama Local AI</h3>
          <p className="body-sm mt-1 text-muted">
            Connect to a local or on-premises Ollama server for private model inference.
          </p>
        </div>
        <button
          type="button"
          className="btn btn-outline btn-sm"
          disabled={busy}
          onClick={() => void refresh()}
        >
          <IconRefresh className={`h-4 w-4 ${busy ? "animate-spin" : ""}`} /> Check Status
        </button>
      </div>

      {error && <div className="card mb-4 p-4 text-error text-xs">{error}</div>}

      {/* Connection status card */}
      <div className="card p-5 mb-6">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <span
              className={`h-3 w-3 rounded-full ${
                running === true
                  ? "bg-success shadow-[0_0_8px_rgba(34,197,94,0.6)]"
                  : running === false
                  ? "bg-error"
                  : "bg-muted"
              }`}
            />
            <div>
              <div className="text-sm font-medium text-ink">
                {running === true ? "Ollama Connected" : running === false ? "Ollama Offline" : "Checking…"}
              </div>
              <div className="caption text-muted font-mono mt-0.5">{url}</div>
            </div>
          </div>
          {version && <span className="badge-pill">v{version}</span>}
        </div>
      </div>

      {/* Pull model input */}
      <div className="card p-5 mb-6">
        <h4 className="text-xs font-semibold uppercase text-muted mb-2">Pull New Model</h4>
        <div className="flex gap-2">
          <input
            className="input flex-1 text-xs"
            placeholder="e.g. llama3.2:3b, qwen2.5:7b, nomic-embed-text"
            value={pullModelName}
            onChange={(e) => setPullModelName(e.target.value)}
          />
          <button
            type="button"
            className="btn btn-primary btn-sm"
            disabled={!pullModelName.trim() || pulling || !running}
            onClick={() => void handlePull()}
          >
            <IconPlus className="h-4 w-4" /> Pull Model
          </button>
        </div>
        {pullMsg && <p className="caption mt-2 text-muted">{pullMsg}</p>}
      </div>

      {/* Installed models */}
      <div>
        <h4 className="text-xs font-semibold uppercase text-muted mb-3">Available Models ({models.length})</h4>
        {models.length === 0 ? (
          <div className="card p-6 text-center text-muted text-xs">
            {running ? "No models found in Ollama repository." : "Start Ollama service to view installed models."}
          </div>
        ) : (
          <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
            {models.map((m) => (
              <div key={m.name} className="card p-3 flex items-center justify-between">
                <div className="min-w-0 flex-1">
                  <div className="text-xs font-medium text-ink truncate">{m.name}</div>
                  <div className="caption text-muted-soft font-mono">
                    {m.size ? `${(m.size / (1024 * 1024 * 1024)).toFixed(2)} GB` : "—"}
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
