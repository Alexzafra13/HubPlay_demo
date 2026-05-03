import { useCallback, useSyncExternalStore } from "react";

const STORAGE_KEY = "hubplay:sidebar:collapsed";
const EVENT = "hubplay:sidebar-collapsed-change";

function read(): boolean {
  if (typeof window === "undefined") return false;
  return window.localStorage.getItem(STORAGE_KEY) === "1";
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
  const collapsed = useSyncExternalStore(subscribe, read, () => false);
  const toggle = useCallback(() => write(!read()), []);
  const set = useCallback((value: boolean) => write(value), []);
  return [collapsed, toggle, set];
}
