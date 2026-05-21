// DebugOverlay — opt-in floating panel that surfaces frontend state the
// user can screenshot. Activated by `?debug=1` in the URL or
// `localStorage.setItem("hubplay_debug","1")`. Always returns null in
// production unless the flag is on, so it has zero cost otherwise.
//
// What it shows is deliberately scoped to the bugs we've actually had
// to chase in the wild:
//   - Modal portals still mounted on document.body (catches a folder
//     picker leaking past its parent).
//   - Whether body scroll is currently locked (catches an overflow
//     cleanup that didn't run).
//   - Active TanStack queries with their key + state (catches a
//     prefetch firing in a loop or a query stuck pending).
//   - Current pathname (so a screenshot has context without us asking
//     "and what page were you on?").
//
// Read-only by design. We never write back to React state from here —
// the panel is a window, not a controller.

import { useEffect, useState } from "react";
import { useLocation } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import { useModalStack } from "@/store/modalStack";

const STORAGE_KEY = "hubplay_debug";

function isDebugEnabled(): boolean {
  if (typeof window === "undefined") return false;
  if (new URLSearchParams(window.location.search).get("debug") === "1") return true;
  try {
    return window.localStorage.getItem(STORAGE_KEY) === "1";
  } catch {
    return false;
  }
}

interface QuerySnapshot {
  key: string;
  status: string;
  fetchStatus: string;
}

export function DebugOverlay() {
  const enabled = isDebugEnabled();
  const location = useLocation();
  const queryClient = useQueryClient();
  const modalStack = useModalStack((s) => s.stack);
  const [tick, setTick] = useState(0);

  // Re-snapshot every 500ms while the panel is visible. cheap enough
  // (a handful of queries, querySelectorAll on body).
  useEffect(() => {
    if (!enabled) return;
    const id = window.setInterval(() => setTick((t) => t + 1), 500);
    return () => window.clearInterval(id);
  }, [enabled]);

  if (!enabled) return null;

  // Cross-check: the store's view of how many modals are open vs.
  // what's actually mounted on document.body. They should always
  // match. A drift means something rendered a portal-style overlay
  // outside the Modal component (or the other way round) and is the
  // first thing to look at if "navigation feels stuck".
  const dialogs = document.querySelectorAll('[role="dialog"]');
  const scrollLocked = document.body.style.overflow === "hidden";
  const stackCount = modalStack.length;
  const drift = stackCount !== dialogs.length;

  // Sólo nos interesan queries que no estén durmiendo en cache. Una
  // query "success + idle" es ruido; pending / fetching / error son
  // las pistas útiles. Una pasada única para mapear + filtrar.
  const queries: QuerySnapshot[] = queryClient
    .getQueryCache()
    .getAll()
    .reduce<QuerySnapshot[]>((acc, q) => {
      const status = q.state.status;
      const fetchStatus = q.state.fetchStatus;
      if (fetchStatus === "idle" && status === "success") return acc;
      acc.push({ key: JSON.stringify(q.queryKey), status, fetchStatus });
      return acc;
    }, []);

  // Reference tick so React doesn't elide the re-render while we
  // refresh the DOM-derived snapshot.
  void tick;

  return (
    <div
      className="fixed bottom-2 right-2 z-[60] max-w-[360px] max-h-[60vh] overflow-y-auto px-2.5 py-2 font-mono text-xs leading-snug text-slate-200 bg-[rgba(15,18,25,0.92)] border border-[#2a313f] rounded-md shadow-lg pointer-events-auto"
      role="status"
      aria-live="polite"
    >
      <div className="font-semibold mb-1 text-sky-300">
        debug · {location.pathname}
      </div>
      <div>
        stack: <b>{stackCount}</b> · dom: <b>{dialogs.length}</b>
        {drift && <span className="text-red-300"> ← drift</span>}
      </div>
      <div>
        body scroll lock: <b>{scrollLocked ? "ON" : "off"}</b>
        {scrollLocked && stackCount === 0 && (
          <span className="text-red-300"> ← stale</span>
        )}
      </div>
      <div className="mt-1.5 font-semibold text-sky-300">
        active queries ({queries.length})
      </div>
      {queries.length === 0 && <div className="opacity-60">(none in flight)</div>}
      {queries.map((q) => (
        // El queryKey ya es único por query (tanstack lo garantiza).
        <div key={q.key} className="break-all">
          <span
            className={
              q.status === "error"
                ? "text-red-300"
                : q.fetchStatus === "fetching"
                  ? "text-amber-200"
                  : "text-slate-300"
            }
          >
            {q.fetchStatus}/{q.status}
          </span>{" "}
          {q.key}
        </div>
      ))}
    </div>
  );
}
