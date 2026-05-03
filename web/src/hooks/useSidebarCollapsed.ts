import { useCallback, useSyncExternalStore } from "react";

const STORAGE_KEY = "hubplay:sidebar:collapsed";
const EVENT = "hubplay:sidebar-collapsed-change";

// Default: collapsed (icons only). The premium-feel choice — content
// gets the breathing room, the topbar hamburger is the explicit
// "expand" affordance. Users who want it permanently expanded set
// the key to "0" once and we honor it forever.
function read(): boolean {
  if (typeof window === "undefined") return true;
  const stored = window.localStorage.getItem(STORAGE_KEY);
  if (stored === null) return true;
  return stored === "1";
}

function write(value: boolean) {
  if (typeof window === "undefined") return;
  window.localStorage.setItem(STORAGE_KEY, value ? "1" : "0");
  window.dispatchEvent(new Event(EVENT));
}

function subscribe(onChange: () => void) {
  window.addEventListener(EVENT, onChange);
  window.addEventListener("storage", onChange);
  return () => {
    window.removeEventListener(EVENT, onChange);
    window.removeEventListener("storage", onChange);
  };
}

// Persists the desktop sidebar collapsed state across reloads. Two
// callers (AppLayout and any future deep link) stay in sync via the
// custom event, so toggling in one place updates the other instantly
// without prop-drilling. SSR-safe via getServerSnapshot returning false.
export function useSidebarCollapsed(): [boolean, () => void, (value: boolean) => void] {
  const collapsed = useSyncExternalStore(subscribe, read, () => true);
  const toggle = useCallback(() => write(!read()), []);
  const set = useCallback((value: boolean) => write(value), []);
  return [collapsed, toggle, set];
}
