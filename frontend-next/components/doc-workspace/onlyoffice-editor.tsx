"use client";

import { forwardRef, useEffect, useId, useImperativeHandle, useRef } from "react";

/* Minimal typing of the ONLYOFFICE Docs API surface we use. */
export interface DocsApiEditor {
  destroyEditor: () => void;
  /** DS ≥ 8.3: swap the document under the open editor without reload. */
  refreshFile?: (config: Record<string, unknown>) => void;
}
export interface DocsApiNamespace {
  DocEditor: new (containerId: string, config: Record<string, unknown>) => DocsApiEditor;
}
declare global {
  interface Window {
    DocsAPI?: DocsApiNamespace;
  }
}

export type OnlyOfficeEditorEvents = {
  onDocumentReady?: () => void;
  /** true = unsaved changes pending, false = all changes saved. */
  onDocumentStateChange?: (dirty: boolean) => void;
  /** Editor asks for a fresh config (version change / reconnect). */
  onRequestRefreshFile?: () => void;
  onError?: (code: number | undefined, description: string) => void;
  onWarning?: (code: number | undefined, description: string) => void;
  onInfo?: (mode: string | undefined) => void;
};

export type OnlyOfficeEditorHandle = {
  refreshFile: (config: Record<string, unknown>) => void;
  destroy: () => void;
  /** postMessage to the editor iframe (DocsAPI frameEditor) with an exact
   * target origin. False when the iframe is not there yet. */
  postToEditor: (message: unknown, targetOrigin: string) => boolean;
  /** Whether a postMessage came from inside this editor (its iframe or a
   * plugin frame nested in it) — several editors can be open at once. */
  ownsMessageSource: (source: MessageEventSource | null) => boolean;
};

/* Keyed by api.js src: each Document Server URL gets its own load. */
const scriptPromises = new Map<string, Promise<DocsApiNamespace>>();

function apiScriptUrl(documentServerUrl: string): string {
  return `${documentServerUrl.replace(/\/+$/, "")}/web-apps/apps/api/documents/api.js`;
}

function findScript(src: string): HTMLScriptElement | null {
  for (const el of Array.from(document.getElementsByTagName("script"))) {
    if (el.src === src) return el;
  }
  return null;
}

/* Inject api.js once per Document Server URL. An existing window.DocsAPI is
 * reused only when it was loaded from this same src; a failed load is
 * evicted so a later mount can retry. */
export function loadDocsApi(documentServerUrl: string): Promise<DocsApiNamespace> {
  const src = new URL(apiScriptUrl(documentServerUrl), window.location.href).href;
  const cached = scriptPromises.get(src);
  if (cached) return cached;
  const p = new Promise<DocsApiNamespace>((resolve, reject) => {
    const existing = findScript(src);
    if (existing && window.DocsAPI && existing.dataset.weragLoaded === "1") {
      resolve(window.DocsAPI);
      return;
    }
    existing?.remove();
    const el = document.createElement("script");
    el.src = src;
    el.async = true;
    el.onload = () => {
      el.dataset.weragLoaded = "1";
      if (window.DocsAPI) resolve(window.DocsAPI);
      else reject(new Error("DocsAPI not available after loading api.js"));
    };
    el.onerror = () => {
      el.remove();
      reject(new Error(`Failed to load ${src}`));
    };
    document.head.appendChild(el);
  });
  p.catch(() => scriptPromises.delete(src));
  scriptPromises.set(src, p);
  return p;
}

type Props = {
  documentServerUrl: string;
  config: Record<string, unknown>;
  events?: OnlyOfficeEditorEvents;
  onLoadError?: (err: Error) => void;
};

/* Mounts a DocsAPI.DocEditor into a private container. The editor is created
 * once per (documentServerUrl, editor key); later config swaps go through the
 * imperative refreshFile() so the user's view is not torn down. */
export const OnlyOfficeEditor = forwardRef<OnlyOfficeEditorHandle, Props>(function OnlyOfficeEditor(
  { documentServerUrl, config, events, onLoadError },
  ref,
) {
  const reactId = useId();
  const containerId = `onlyoffice-${reactId.replace(/[^a-zA-Z0-9_-]/g, "")}`;
  const editorRef = useRef<DocsApiEditor | null>(null);
  // Namespace from this editor's own api.js (not whatever window.DocsAPI is now).
  const apiRef = useRef<DocsApiNamespace | null>(null);
  const eventsRef = useRef(events);
  eventsRef.current = events;
  const configRef = useRef(config);
  configRef.current = config;
  const onLoadErrorRef = useRef(onLoadError);
  onLoadErrorRef.current = onLoadError;

  const docKey = (() => {
    const doc = config.document as { key?: unknown } | undefined;
    return typeof doc?.key === "string" ? doc.key : "";
  })();

  const create = (api: DocsApiNamespace, cfg: Record<string, unknown>) => {
    // DocEditor replaces the placeholder node; give it a fresh one each time.
    const host = document.getElementById(`${containerId}-host`);
    if (!host) return;
    host.innerHTML = "";
    const slot = document.createElement("div");
    slot.id = containerId;
    host.appendChild(slot);
    editorRef.current = new api.DocEditor(containerId, {
      ...cfg,
      width: "100%",
      height: "100%",
      events: {
        onDocumentReady: () => eventsRef.current?.onDocumentReady?.(),
        onDocumentStateChange: (e: { data?: boolean }) =>
          eventsRef.current?.onDocumentStateChange?.(Boolean(e?.data)),
        onRequestRefreshFile: () => eventsRef.current?.onRequestRefreshFile?.(),
        onError: (e: { data?: { errorCode?: number; errorDescription?: string } }) =>
          eventsRef.current?.onError?.(e?.data?.errorCode, e?.data?.errorDescription ?? ""),
        onWarning: (e: { data?: { warningCode?: number; warningDescription?: string } }) =>
          eventsRef.current?.onWarning?.(e?.data?.warningCode, e?.data?.warningDescription ?? ""),
        onInfo: (e: { data?: { mode?: string } }) => eventsRef.current?.onInfo?.(e?.data?.mode),
      },
    });
  };

  const destroy = () => {
    try {
      editorRef.current?.destroyEditor();
    } catch {
      /* already gone */
    }
    editorRef.current = null;
  };

  useEffect(() => {
    let cancelled = false;
    loadDocsApi(documentServerUrl)
      .then((api) => {
        if (cancelled) return;
        apiRef.current = api;
        create(api, configRef.current);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        onLoadErrorRef.current?.(err instanceof Error ? err : new Error(String(err)));
      });
    return () => {
      cancelled = true;
      destroy();
    };
    // Recreate only when the server or the document identity changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [documentServerUrl, docKey]);

  useImperativeHandle(ref, () => ({
    refreshFile: (cfg: Record<string, unknown>) => {
      configRef.current = cfg;
      const ed = editorRef.current;
      if (ed && typeof ed.refreshFile === "function") {
        try {
          ed.refreshFile(cfg);
          return;
        } catch {
          /* fall through to recreate */
        }
      }
      destroy();
      const api = apiRef.current;
      if (api) create(api, cfg);
    },
    destroy,
    ownsMessageSource: (source: MessageEventSource | null) => {
      const frame = document.getElementById(`${containerId}-host`)?.querySelector("iframe");
      const win = frame?.contentWindow;
      if (!win || !source) return false;
      // parent is readable on a cross-origin window; plugins sit a few
      // frames below the editor iframe
      let w = source as Window;
      for (let depth = 0; depth < 6 && w; depth++) {
        if (w === win) return true;
        if (w === window || w.parent === w) return false;
        w = w.parent;
      }
      return false;
    },
    postToEditor: (message: unknown, targetOrigin: string) => {
      const frame = document.getElementById(`${containerId}-host`)?.querySelector("iframe");
      const win = frame?.contentWindow;
      if (!win) return false;
      try {
        win.postMessage(message, targetOrigin);
        return true;
      } catch {
        return false;
      }
    },
  }));

  return <div id={`${containerId}-host`} className="h-full w-full [&>div]:h-full [&_iframe]:block" />;
});
