"use client";

import { useEffect, useRef, useState } from "react";
import { Modal } from "@/components/modal";
import { IconPlus, IconDoc } from "@/components/icons";
import { useUploadTasks } from "@/lib/upload-tasks";
import { buildUploadFileName } from "@/lib/upload-queue";

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
};

/* File picker feeding the global upload queue — transfers, parse status and
 * retries are reported by the floating UploadTasksPanel, which survives
 * navigation, instead of inside this modal. Ported to match the Vue flow
 * (confirm → uploadTasksStore.enqueue → panel). */
export function UploadModal({
  kbId,
  kbName,
  open,
  onClose,
  initialFiles,
  onProceed,
}: {
  kbId: string;
  kbName: string;
  open: boolean;
  onClose: () => void;
  /** Files arriving from the global drop zone, merged in when the modal opens. */
  initialFiles?: File[];
  /* When set, the Upload button hands the picked files to the caller (which
   * opens the parse-settings dialog) instead of enqueueing directly —
   * matching the Vue confirm-dialog flow. */
  onProceed?: (files: File[]) => void;
}) {
  const [files, setFiles] = useState<FileItem[]>([]);
  const [dragOver, setDragOver] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const { enqueue } = useUploadTasks();

  useEffect(() => {
    if (open && initialFiles && initialFiles.length > 0) addFiles(initialFiles);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, initialFiles]);

  /* Closed programmatically (e.g. handing off to the parse-settings dialog)
   * still resets the picker; reopening re-seeds via initialFiles. */
  useEffect(() => {
    if (!open) setFiles([]);
  }, [open]);

  const addFiles = (selected: File[]) => {
    const newItems: FileItem[] = selected.map((f) => ({
      id: `${f.name}-${Date.now()}-${Math.random()}`,
      file: f,
    }));
    setFiles((prev) => [...prev, ...newItems]);
  };

  const removeFile = (id: string) => {
    setFiles((prev) => prev.filter((f) => f.id !== id));
  };

  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault();
    setDragOver(false);
    if (e.dataTransfer.files && e.dataTransfer.files.length > 0) {
      addFiles(Array.from(e.dataTransfer.files));
    }
  };

  const startUpload = () => {
    if (files.length === 0) return;
    if (onProceed) {
      onProceed(files.map((item) => item.file));
      return;
    }
    enqueue({
      kbId,
      kbName,
      uploads: files.map((item) => ({
        file: item.file,
        fileName: buildUploadFileName(item.file, ""),
      })),
    });
    setFiles([]);
    onClose();
  };

  const handleClose = () => {
    setFiles([]);
    onClose();
  };

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
          onClick={() => inputRef.current?.click()}
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
                </div>
                <button
                  onClick={(e) => {
                    e.stopPropagation();
                    removeFile(item.id);
                  }}
                  className="text-muted hover:text-ink shrink-0 p-1"
                >
                  ✕
                </button>
              </div>
            ))}
          </div>
        )}

        {/* Actions */}
        <div className="flex items-center justify-end gap-3 pt-2">
          <button type="button" className="btn btn-outline" onClick={handleClose}>
            Cancel
          </button>
          <button
            type="button"
            className="btn btn-primary"
            disabled={files.length === 0}
            onClick={startUpload}
          >
            {`Upload${files.length > 0 ? ` (${files.length})` : ""}`}
          </button>
        </div>
      </div>
    </Modal>
  );
}
