"use client";

import { useEffect, useRef, useState } from "react";
import {
  deleteTemporaryAttachment,
  getTemporaryAttachment,
  uploadTemporaryAttachment,
  type TemporaryAttachment,
} from "@/lib/api/attachments";
import { useChatContext } from "@/lib/chat-context";

/* Ports AttachmentUpload.vue: hidden file input + preview bar with
 * upload→poll(ready/failed) lifecycle against
 * POST /api/v1/sessions/:id/attachments. Files stay local until a sessionId
 * exists (create-chat page uploads after navigation, like the Vue flow).
 */

export type PendingAttachment = {
  localId: string;
  file: File;
  name: string;
  size: number;
  documentId?: string;
  status: "local" | "uploading" | "uploaded" | "processing" | "ready" | "failed";
  progress?: number;
  error?: string;
};

const MAX_FILES = 5;
const MAX_SIZE_MB = 50;

export function useAttachments(sessionId?: string) {
  const { settings } = useChatContext();
  const [items, setItems] = useState<PendingAttachment[]>([]);
  const inputRef = useRef<HTMLInputElement>(null);
  const timers = useRef(new Map<string, ReturnType<typeof setTimeout>>());
  const itemsRef = useRef(items);
  itemsRef.current = items;

  useEffect(() => () => timers.current.forEach((t) => clearTimeout(t)), []);

  // Attachments are session-scoped: moving to another session drops them
  // (their documentIds would 404/poll-forever or leak across sessions).
  const sessionKey = useRef(sessionId);
  useEffect(() => {
    if (sessionKey.current !== sessionId) {
      sessionKey.current = sessionId;
      timers.current.forEach((t) => clearTimeout(t));
      timers.current.clear();
      setItems([]);
    }
  }, [sessionId]);

  const patch = (localId: string, p: Partial<PendingAttachment>) =>
    setItems((prev) => prev.map((a) => (a.localId === localId ? { ...a, ...p } : a)));

  const poll = (localId: string, documentId: string) => {
    if (!sessionId) return;
    timers.current.set(
      localId,
      setTimeout(async () => {
        try {
          const res = await getTemporaryAttachment(sessionId, documentId);
          if (!itemsRef.current.some((a) => a.localId === localId)) return;
          patch(localId, { status: res.data.status, error: res.data.error_message });
          if (res.data.status !== "ready" && res.data.status !== "failed") poll(localId, documentId);
        } catch {
          patch(localId, { status: "failed", error: "Parse status check failed" });
        }
      }, 800),
    );
  };

  const upload = async (localId: string, file: File) => {
    if (!sessionId) return;
    patch(localId, { status: "uploading", progress: 0 });
    try {
      const res = await uploadTemporaryAttachment(
        sessionId,
        file,
        settings.selectedAgentId || undefined,
        settings.selectedAgentSourceTenantId ?? undefined,
        "auto",
        (progress) => patch(localId, { progress }),
      );
      const data: TemporaryAttachment = res.data;
      if (!itemsRef.current.some((a) => a.localId === localId)) {
        await deleteTemporaryAttachment(sessionId, data.id).catch(() => undefined);
        return;
      }
      patch(localId, { documentId: data.id, status: data.status, progress: 100 });
      if (data.status !== "ready" && data.status !== "failed") poll(localId, data.id);
    } catch (e) {
      patch(localId, { status: "failed", error: e instanceof Error ? e.message : "Upload failed" });
    }
  };

  const addFiles = (files: File[]) => {
    setItems((prev) => {
      const next = [...prev];
      for (const file of files) {
        if (next.length >= MAX_FILES) break;
        if (file.size > MAX_SIZE_MB * 1024 * 1024) continue;
        const localId = `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
        next.push({ localId, file, name: file.name, size: file.size, status: sessionId ? "uploading" : "local" });
        if (sessionId) void upload(localId, file);
      }
      return next;
    });
  };

  const remove = (localId: string) => {
    const found = itemsRef.current.find((a) => a.localId === localId);
    const timer = timers.current.get(localId);
    if (timer) clearTimeout(timer);
    timers.current.delete(localId);
    setItems((prev) => prev.filter((a) => a.localId !== localId));
    if (sessionId && found?.documentId) void deleteTemporaryAttachment(sessionId, found.documentId).catch(() => undefined);
  };

  const clear = () => {
    timers.current.forEach((t) => clearTimeout(t));
    timers.current.clear();
    setItems([]);
  };

  return { items, setItems, inputRef, addFiles, remove, clear, trigger: () => inputRef.current?.click() };
}

export function formatFileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}
