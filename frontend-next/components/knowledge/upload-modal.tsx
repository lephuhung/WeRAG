"use client";

import { useRef, useState } from "react";
import { Modal } from "@/components/modal";
import { IconPlus, IconDoc } from "@/components/icons";
import { uploadKnowledgeFile } from "@/lib/api/knowledge";

function fmtBytes(bytes: number): string {
  if (!bytes) return "0 B";
  const k = 1024;
  const sizes = ["B", "KB", "MB", "GB"];
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(k)), sizes.length - 1);
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(1))} ${sizes[i]}`;
}

type FileItem = {
  id: string;
  file: File;
  progress: number;
  status: "ready" | "uploading" | "done" | "error";
  error?: string;
};

export function UploadModal({
  kbId,
  open,
  onClose,
  onUploaded,
}: {
  kbId: string;
  open: boolean;
  onClose: () => void;
  onUploaded: () => void;
}) {
  const [files, setFiles] = useState<FileItem[]>([]);
  const [busy, setBusy] = useState(false);
  const [dragOver, setDragOver] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  const addFiles = (selected: File[]) => {
    const newItems: FileItem[] = selected.map((f) => ({
      id: `${f.name}-${Date.now()}-${Math.random()}`,
      file: f,
      progress: 0,
      status: "ready",
    }));
    setFiles((prev) => [...prev, ...newItems]);
  };

  const removeFile = (id: string) => {
    if (busy) return;
    setFiles((prev) => prev.filter((f) => f.id !== id));
  };

  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault();
    setDragOver(false);
    if (e.dataTransfer.files && e.dataTransfer.files.length > 0) {
      addFiles(Array.from(e.dataTransfer.files));
    }
  };

  const startUpload = async () => {
    if (files.length === 0 || busy) return;
    setBusy(true);

    let anySuccess = false;

    for (let i = 0; i < files.length; i++) {
      const item = files[i];
      if (item.status === "done") continue;

      setFiles((prev) =>
        prev.map((f, idx) => (idx === i ? { ...f, status: "uploading", progress: 0 } : f)),
      );

      try {
        await uploadKnowledgeFile(
          kbId,
          { file: item.file, fileName: item.file.name },
          (percent) => {
            setFiles((prev) =>
              prev.map((f, idx) => (idx === i ? { ...f, progress: percent } : f)),
            );
          },
        );
        anySuccess = true;
        setFiles((prev) =>
          prev.map((f, idx) => (idx === i ? { ...f, status: "done", progress: 100 } : f)),
        );
      } catch (err) {
        const msg = err instanceof Error ? err.message : "Upload failed";
        setFiles((prev) =>
          prev.map((f, idx) => (idx === i ? { ...f, status: "error", error: msg } : f)),
        );
      }
    }

    setBusy(false);
    if (anySuccess) {
      onUploaded();
    }
  };

  const handleClose = () => {
    if (busy) return;
    setFiles([]);
    onClose();
  };

  const allDone = files.length > 0 && files.every((f) => f.status === "done");

  return (
    <Modal open={open} title="Upload documents" onClose={handleClose} width="w-[560px]">
      <div className="space-y-4">
        {/* Drop zone */}
        <div
          onDragOver={(e) => {
            e.preventDefault();
            setDragOver(true);
          }}
          onDragLeave={() => setDragOver(false)}
          onDrop={handleDrop}
          onClick={() => !busy && inputRef.current?.click()}
          className={`flex min-h-[140px] cursor-pointer flex-col items-center justify-center rounded-[16px] border-2 border-dashed p-6 text-center transition-colors ${
            dragOver
              ? "border-primary bg-primary/5"
              : "border-hairline-strong hover:border-ink hover:bg-surface-strong/50"
          }`}
        >
          <input
            ref={inputRef}
            type="file"
            multiple
            className="hidden"
            onChange={(e) => {
              if (e.target.files) addFiles(Array.from(e.target.files));
              e.target.value = "";
            }}
          />
          <div className="mb-2 flex h-10 w-10 items-center justify-center rounded-full bg-surface-strong text-ink">
            <IconPlus className="h-5 w-5" />
          </div>
          <p className="text-[14px] font-medium text-ink">Click or drag & drop files here</p>
          <p className="caption mt-1 text-muted-soft">
            Supports PDF, DOCX, TXT, MD, XLSX, CSV, PPTX and more
          </p>
        </div>

        {/* Selected files list */}
        {files.length > 0 && (
          <div className="max-h-[200px] overflow-y-auto space-y-2 pr-1">
            {files.map((item) => (
              <div
                key={item.id}
                className="flex items-center gap-3 rounded-[12px] border border-hairline bg-surface-card p-3 text-[13px]"
              >
                <IconDoc className="h-4 w-4 shrink-0 text-muted" />
                <div className="min-w-0 flex-1">
                  <div className="flex items-center justify-between gap-2">
                    <span className="truncate font-medium text-ink">{item.file.name}</span>
                    <span className="shrink-0 text-muted-soft">{fmtBytes(item.file.size)}</span>
                  </div>
                  {item.status === "uploading" && (
                    <div className="mt-1.5 h-1.5 w-full overflow-hidden rounded-full bg-surface-strong">
                      <div
                        className="h-full bg-primary transition-all duration-200"
                        style={{ width: `${item.progress}%` }}
                      />
                    </div>
                  )}
                  {item.status === "error" && (
                    <p className="caption mt-0.5 text-error truncate">{item.error}</p>
                  )}
                  {item.status === "done" && (
                    <p className="caption mt-0.5 text-success">Uploaded successfully</p>
                  )}
                </div>
                {!busy && item.status !== "done" && (
                  <button
                    onClick={(e) => {
                      e.stopPropagation();
                      removeFile(item.id);
                    }}
                    className="text-muted hover:text-ink shrink-0 p-1"
                  >
                    ✕
                  </button>
                )}
              </div>
            ))}
          </div>
        )}

        {/* Actions */}
        <div className="flex items-center justify-end gap-3 pt-2">
          <button
            type="button"
            className="btn btn-outline"
            onClick={handleClose}
            disabled={busy}
          >
            {allDone ? "Done" : "Cancel"}
          </button>
          {!allDone && (
            <button
              type="button"
              className="btn btn-primary"
              disabled={files.length === 0 || busy}
              onClick={() => void startUpload()}
            >
              {busy ? "Uploading…" : `Upload ${files.length > 0 ? `(${files.length})` : ""}`}
            </button>
          )}
        </div>
      </div>
    </Modal>
  );
}
