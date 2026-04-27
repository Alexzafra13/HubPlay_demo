import {
  createContext,
  useContext,
  useEffect,
  useState,
  type Dispatch,
  type ReactNode,
  type SetStateAction,
} from "react";

/**
 * TopBarSlot — a single React-portal-style slot that the global TopBar
 * reads to render page-specific controls (tabs, page search, view
 * settings) inside its existing sticky+glass bar instead of forcing
 * pages to stack a second sticky bar underneath.
 *
 * Why context over prop-drilling: the slot owner is the page (a leaf
 * of the route tree), the consumer is the layout shell (an ancestor).
 * React's data flow is top-down, so a leaf-to-ancestor handoff needs
 * either a context, a portal, or a global store. Context is the
 * lightest of the three and the slot is genuinely UI-shell concern,
 * not domain state — Zustand would be overkill.
 *
 * ─── Why two contexts? ─────────────────────────────────────────────
 * Splitting the writer (`setContent`) and the reader (`content`) into
 * separate contexts prevents a re-render loop. With a single context,
 * any consumer that called `useTopBarSlot(node)` would re-render every
 * time the slot's content changed — but the page's *own* render is
 * what produced the new node JSX in the first place, which then ran
 * the effect, which called `setContent`, which changed the context
 * value, which re-rendered the page's slot consumer, which produced a
 * fresh JSX object (new identity), which re-fired the effect…
 * In practice this manifested as a busy main thread that swallowed
 * sidebar clicks: navigating away from /live-tv would silently fail
 * because the click landed during a re-render storm.
 *
 * The split keeps the writer context value stable (the dispatcher is
 * referentially stable across renders thanks to React's `useState`
 * contract), so `useTopBarSlot` consumers never re-render when content
 * changes. Only the global TopBar — which subscribes to the reader
 * context — re-renders to display the new node.
 *
 * Single-slot, last-write-wins: only one page is mounted at a time
 * (react-router unmounts the previous route before mounting the next),
 * so we don't need a stack. If two callers race during a transition,
 * the cleanup of the unmounting page restores `null` *after* the new
 * page has set its own content, which would clobber it. We guard
 * against that by only clearing if the current value is still ours
 * (identity check on the registered node).
 */

const SetContentContext = createContext<Dispatch<
  SetStateAction<ReactNode | null>
> | null>(null);
const ContentContext = createContext<ReactNode | null>(null);

export function TopBarSlotProvider({ children }: { children: ReactNode }) {
  const [content, setContent] = useState<ReactNode | null>(null);
  return (
    <SetContentContext.Provider value={setContent}>
      <ContentContext.Provider value={content}>
        {children}
      </ContentContext.Provider>
    </SetContentContext.Provider>
  );
}

/**
 * useTopBarSlot — register `node` as the current TopBar slot content
 * and return `true` if a provider was found (the slot is active),
 * `false` otherwise.
 *
 * The boolean lets components decide whether to render the controls
 * inline as a fallback. This matters for two cases:
 *   1. Unit tests that render a page component standalone, without
 *      the AppLayout's provider — they still see the controls and
 *      can assert on them.
 *   2. Future routes that opt into a different shell where there's
 *      no global TopBar at all (e.g. a fullscreen player surface).
 *
 * Callers can pass a fresh JSX object every render — the writer
 * context is stable so this hook does not cause a re-render cascade
 * (see the file-level comment for why that mattered).
 */
export function useTopBarSlot(node: ReactNode | null): boolean {
  const setContent = useContext(SetContentContext);
  useEffect(() => {
    if (!setContent) return;
    setContent(node);
    return () => {
      // Only clear if our node is still the active one — prevents the
      // unmounting page from wiping the next page's content during a
      // route transition.
      setContent((current) => (current === node ? null : current));
    };
  }, [node, setContent]);
  return setContent !== null;
}

export function useTopBarSlotContent(): ReactNode | null {
  return useContext(ContentContext);
}
