import { lazy, type ComponentType } from "react";

// We use `any` in the generic constraint instead of `unknown` because
// React.lazy itself is `<T extends ComponentType<any>>`. Mirroring that
// avoids forcing every consumer to widen their props type to unknown.
// eslint-disable-next-line @typescript-eslint/no-explicit-any
type AnyComp = ComponentType<any>;

// After a deploy, the previous build's chunk hashes stop existing on
// the server. A tab loaded against the old index.html will throw
// "Failed to fetch dynamically imported module" the first time the
// user navigates to a lazy route. Detect that signature and force one
// hard reload — the new index.html points at the new hashes, so the
// retry succeeds on the reloaded session.
//
// Guarded by a session flag so we never reload-loop on a genuine
// network failure that just happens to look the same.
const RELOAD_FLAG = "__hubplay_chunk_reload";

function isStaleChunkError(err: unknown): boolean {
  if (!err) return false;
  const msg = err instanceof Error ? err.message : String(err);
  return (
    /Failed to fetch dynamically imported module/i.test(msg) ||
    /Loading chunk \d+ failed/i.test(msg) ||
    /error loading dynamically imported module/i.test(msg) ||
    /Importing a module script failed/i.test(msg)
  );
}

export function lazyWithRetry<T extends AnyComp>(
  importer: () => Promise<{ default: T }>,
) {
  return lazy(async () => {
    try {
      return await importer();
    } catch (err) {
      if (isStaleChunkError(err) && typeof window !== "undefined") {
        const alreadyReloaded = window.sessionStorage.getItem(RELOAD_FLAG);
        if (!alreadyReloaded) {
          window.sessionStorage.setItem(RELOAD_FLAG, "1");
          window.location.reload();
          return new Promise<{ default: T }>(() => {});
        }
      }
      throw err;
    }
  });
}

if (typeof window !== "undefined") {
  window.addEventListener("load", () => {
    window.sessionStorage.removeItem(RELOAD_FLAG);
  });
}
