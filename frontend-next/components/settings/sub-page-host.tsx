/* Sub-pages inside the settings modal. The settings modal never stacks a
 * second dialog on top of itself: a <Modal>, confirm dialog or drawer
 * rendered anywhere inside it becomes a page in the modal's content pane —
 * the header shows its title with a back button, and the section underneath
 * stays mounted (hidden) so its state survives. Outside the settings modal
 * no host is provided and those components render as regular overlays. */
"use client";

import {
  createContext,
  useContext,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
} from "react";
import { createPortal } from "react-dom";

export interface SubPageEntry {
  id: string;
  title: string;
  /* Back button / Escape — the component's own onClose. */
  back: () => void;
}

interface SubPageApi {
  push: (id: string, title: string, back: () => void) => void;
  setTitle: (id: string, title: string) => void;
  remove: (id: string) => void;
}

interface SubPageState {
  el: HTMLElement | null;
  topId: string | null;
}

const SubPageApiContext = createContext<SubPageApi | null>(null);
const SubPageStateContext = createContext<SubPageState>({ el: null, topId: null });

/* True when rendering inside the settings modal's sub-page host. */
export function useInSubPageHost() {
  return useContext(SubPageApiContext) != null;
}

/* State + provider for the host. `pages` is the open stack, oldest first. */
export function useSubPageHost() {
  const [pages, setPages] = useState<SubPageEntry[]>([]);
  const [el, setEl] = useState<HTMLElement | null>(null);

  const api = useMemo<SubPageApi>(
    () => ({
      push: (id, title, back) =>
        setPages((s) => [...s.filter((p) => p.id !== id), { id, title, back }]),
      setTitle: (id, title) =>
        setPages((s) => s.map((p) => (p.id === id && p.title !== title ? { ...p, title } : p))),
      remove: (id) => setPages((s) => s.filter((p) => p.id !== id)),
    }),
    [],
  );

  const top = pages.length > 0 ? pages[pages.length - 1] : null;
  const state = useMemo(() => ({ el, topId: top?.id ?? null }), [el, top?.id]);

  return { top, depth: pages.length, hostRef: setEl, api, state };
}

export function SubPageHostProvider({
  api,
  state,
  children,
}: {
  api: SubPageApi;
  state: SubPageState;
  children: React.ReactNode;
}) {
  return (
    <SubPageApiContext.Provider value={api}>
      <SubPageStateContext.Provider value={state}>{children}</SubPageStateContext.Provider>
    </SubPageApiContext.Provider>
  );
}

/* One page in the host: registers itself while mounted and portals its
 * content into the host pane; only the top page is visible. */
export function HostedSubPage({
  title,
  onClose,
  children,
}: {
  title: string;
  onClose: () => void;
  children: React.ReactNode;
}) {
  const api = useContext(SubPageApiContext);
  const { el, topId } = useContext(SubPageStateContext);
  const id = useId();
  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;

  useEffect(() => {
    if (!api) return;
    api.push(id, title, () => onCloseRef.current());
    return () => api.remove(id);
    // title changes are synced by the effect below without re-stacking.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [api, id]);

  useEffect(() => {
    api?.setTitle(id, title);
  }, [api, id, title]);

  if (!el) return null;
  return createPortal(
    <div hidden={topId !== id} className="mx-auto w-full max-w-[760px]">
      {children}
    </div>,
    el,
  );
}
