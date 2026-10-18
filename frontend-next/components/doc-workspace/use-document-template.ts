"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import type { StreamChunk } from "@/lib/api/stream";
import { readAppliedBatches, rememberAppliedBatch } from "@/lib/api/document-ops";
import { DocumentWorkspaceError, getDocumentWorkspace, storeDocumentTemplate } from "@/lib/api/document-workspace";
import type { DocumentTemplateId } from "@/lib/document-templates";
import {
  dismissTemplates,
  forgetTemplateRequest,
  templatesDismissed,
  lastTemplate,
  parseTemplateResponse,
  redoTemplateId,
  rememberLastTemplate,
  restoreTemplateRequest,
  saveTemplateRequest,
  type PendingTemplate,
} from "@/lib/template-protocol";
import type { EditorActions } from "./use-live-spelling";

const TOOL_NAME = "prepare_document_template";

const storage = () => {
  try {
    return typeof window !== "undefined" ? window.localStorage : null;
  } catch {
    return null;
  }
};

const randomId = () =>
  typeof crypto !== "undefined" && "randomUUID" in crypto
    ? crypto.randomUUID()
    : Math.random().toString(36).slice(2) + Date.now().toString(36);

export type TemplateNotice =
  | { kind: "needs"; fields: string[] }
  | { kind: "building" | "done" | "invalid" | "notEmpty" | "otherDoc" | "noData" | "skipped" }
  | { kind: "failed"; error: string };

/* A new NĐ30 document drafted from chat into the open, empty document (the
 * Word add-in's flow, docs/backend-document-template-contract.md there):
 * the user picks a type, the composer is prefilled, every turn of the
 * request carries document_template_request, and only a ready, typed
 * prepare_document_template result matching the request is built into a
 * .docx here and written into the document (server-side, which keeps the
 * empty version as a revision and reloads the editor). Prose answers are
 * never turned into edits. */
export function useDocumentTemplate({
  sessionId,
  documentId,
  tenantKey,
  enabled,
  emptyDocId,
  editorRef,
  onWritten,
}: {
  /** null on the pre-session page. */
  sessionId: string | null;
  /** The document the visible editor shows. */
  documentId: string | null;
  tenantKey: string;
  /** The picker may show (document assistant, not busy). */
  enabled: boolean;
  /** The visible document when the live spellcheck's last read found no
   * text in it (its pass runs on every editor change), else null. */
  emptyDocId: string | null;
  editorRef: React.RefObject<EditorActions | null>;
  /** The document was replaced: re-read the workspace (editor reload). */
  onWritten: () => void;
}) {
  const [request, setRequest] = useState<PendingTemplate | null>(null);
  const requestRef = useRef<PendingTemplate | null>(null);
  requestRef.current = request;
  const [notice, setNotice] = useState<TemplateNotice | null>(null);
  // documents whose template offer the user turned down (kept in storage)
  const [dismissed, setDismissed] = useState<Set<string>>(() => new Set());
  const pendingRef = useRef(new Set<string>());
  const receivedRef = useRef(false);
  const onWrittenRef = useRef(onWritten);
  onWrittenRef.current = onWritten;

  const update = (next: PendingTemplate | null) => {
    requestRef.current = next;
    setRequest(next);
  };

  // per session and document: a request left open (reload) comes back
  // for 24 h; another tab keeps its own
  useEffect(() => {
    setNotice(null);
    if (!sessionId || !documentId) {
      update(null);
      return;
    }
    let pending = restoreTemplateRequest(storage(), tenantKey, sessionId, documentId);
    if (pending && readAppliedBatches(storage(), sessionId).includes(`template:${pending.request_id}`)) {
      forgetTemplateRequest(storage(), tenantKey, pending);
      pending = null;
    }
    update(pending);
  }, [sessionId, documentId, tenantKey]);

  /** The user picked a type: the next turns carry the request. */
  const begin = useCallback(
    (templateId: DocumentTemplateId) => {
      const docId = editorRef.current?.documentId;
      if (!sessionId || !docId) return;
      forgetTemplateRequest(storage(), tenantKey, requestRef.current);
      const next: PendingTemplate = {
        schema_version: 1,
        request_id: randomId(),
        template_id: templateId,
        document_id: docId,
        sessionId,
      };
      // kept across reloads only once a turn carried it (forSend): a type
      // picked but never sent must not hide the picker after a reload
      update(next);
      setNotice(null);
    },
    [sessionId, tenantKey, editorRef],
  );

  /** The agent hand-off card was clicked: a request drafted from the
   * conversation, for the empty document just opened (not visible yet: it
   * is kept and taken up when the document shows). Returns its id. */
  const handoff = useCallback(
    (templateId: DocumentTemplateId, documentId: string) => {
      if (!sessionId) return null;
      const next: PendingTemplate = {
        schema_version: 1, request_id: randomId(), template_id: templateId, document_id: documentId, sessionId,
        from_conversation: true,
      };
      saveTemplateRequest(storage(), tenantKey, next);
      if (documentId === editorRef.current?.documentId) update(next);
      setNotice(null);
      return next.request_id;
    },
    [sessionId, tenantKey, editorRef],
  );

  /** The user turned the offer down for this document (closed it, or
   * "bỏ qua" at once): it is not offered again for it, even after a reload. */
  const dismiss = useCallback(
    (documentId?: string | null, say = false) => {
      const doc = documentId ?? editorRef.current?.documentId;
      if (!sessionId || !doc) return;
      dismissTemplates(storage(), tenantKey, sessionId, doc);
      setDismissed((prev) => new Set(prev).add(doc));
      if (say) setNotice({ kind: "skipped" });
    },
    [sessionId, tenantKey, editorRef],
  );

  const cancel = useCallback(() => {
    forgetTemplateRequest(storage(), tenantKey, requestRef.current);
    update(null);
    setNotice(null);
  }, [tenantKey]);

  /** The request a turn sent now carries (undefined: none). text is the
   * message: asking in plain chat to draft a type again ("tạo lại công
   * văn") into the visible, empty document opens a redo request. */
  const forSend = useCallback((text?: string) => {
    // the last turn's notice is about the last turn
    setNotice(null);
    let r = requestRef.current;
    const docId = editorRef.current?.documentId;
    if (!r && text && sessionId && docId && emptyDocId === docId) {
      const templateId = redoTemplateId(text, lastTemplate(storage(), tenantKey, sessionId));
      if (templateId) {
        r = { schema_version: 1, request_id: randomId(), template_id: templateId, document_id: docId, sessionId, redo: true };
        update(r);
      }
    }
    if (!r || !sessionId || r.sessionId !== sessionId) return undefined;
    saveTemplateRequest(storage(), tenantKey, r);
    receivedRef.current = false;
    return {
      schema_version: 1 as const, request_id: r.request_id, template_id: r.template_id, document_id: r.document_id,
      ...(r.redo ? { redo: true as const } : {}),
      ...(r.from_conversation ? { from_conversation: true as const } : {}),
    };
  }, [sessionId, tenantKey, emptyDocId, editorRef]);

  /** A turn of the request ended without a result: say so. */
  const turnEnded = useCallback(() => {
    if (requestRef.current && !receivedRef.current) setNotice((n) => n ?? { kind: "noData" });
  }, []);

  const write = useCallback(async (sid: string, r: PendingTemplate, build: () => Promise<Uint8Array>) => {
    const key = `template:${r.request_id}`;
    if (pendingRef.current.has(key) || readAppliedBatches(storage(), sid).includes(key)) return;
    pendingRef.current.add(key);
    setNotice({ kind: "building" });
    const still = () => requestRef.current?.request_id === r.request_id;
    try {
      const editor = editorRef.current;
      if (!editor || editor.documentId !== r.document_id) return setNotice({ kind: "otherDoc" });
      // read again right before writing: what the user typed meanwhile wins
      const texts = await editor.readParagraphs();
      if (!texts) throw new Error("the editor did not answer");
      if (texts.some((t) => t.trim())) return setNotice({ kind: "notEmpty" });
      const bytes = await build();
      const view = await getDocumentWorkspace(sid, r.document_id);
      if (!view) return setNotice({ kind: "otherDoc" });
      if (!still()) return;
      await storeDocumentTemplate(sid, r.document_id, view.revision, bytes);
      rememberAppliedBatch(storage(), sid, key);
      rememberLastTemplate(storage(), tenantKey, sid, r.template_id);
      forgetTemplateRequest(storage(), tenantKey, r);
      if (still()) update(null);
      setNotice({ kind: "done" });
      onWrittenRef.current();
    } catch (e) {
      if (e instanceof DocumentWorkspaceError && e.status === 409) setNotice({ kind: "notEmpty" });
      else setNotice({ kind: "failed", error: e instanceof Error ? e.message : String(e) });
    } finally {
      pendingRef.current.delete(key);
    }
  }, [editorRef, tenantKey]);

  /** A tool_result of the stream; true when it was the template's. */
  const onToolResult = useCallback(
    (c: StreamChunk): boolean => {
      if (c.tool_name !== TOOL_NAME) return false;
      const r = requestRef.current;
      if (!r || !sessionId || r.sessionId !== sessionId) return true;
      receivedRef.current = true;
      const result = parseTemplateResponse(c.data);
      if (!result || result.request_id !== r.request_id || result.template_id !== r.template_id || result.document_id !== r.document_id) {
        setNotice({ kind: "invalid" });
        return true;
      }
      if (result.status === "needs_input") {
        setNotice({ kind: "needs", fields: result.missing_fields });
        return true;
      }
      if (result.status === "skipped") {
        // the document stays empty: close the request, stop offering
        forgetTemplateRequest(storage(), tenantKey, r);
        update(null);
        dismiss(r.document_id);
        setNotice({ kind: "skipped" });
        return true;
      }
      const { fields, font_sizes: sizes } = result;
      void write(sessionId, r, async () => {
        const { buildDocumentTemplate } = await import("@/lib/document-templates");
        return buildDocumentTemplate(result.template_id, fields, sizes);
      });
      return true;
    },
    [sessionId, write, tenantKey, dismiss],
  );

  const offerDismissed =
    !!sessionId && !!documentId && (dismissed.has(documentId) || templatesDismissed(storage(), tenantKey, sessionId, documentId));

  return {
    request,
    notice,
    /** The visible document is empty and no request is open: offer the picker. */
    showPicker: enabled && !request && !!emptyDocId && emptyDocId === documentId && !offerDismissed,
    begin,
    handoff,
    dismiss,
    cancel,
    forSend,
    turnEnded,
    onToolResult,
    clearNotice: () => setNotice(null),
  };
}
