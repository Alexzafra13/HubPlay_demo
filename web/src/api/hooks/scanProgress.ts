import { useEffect, useState } from "react";

// useScanProgress — subscribes to /api/v1/events (the existing SSE
// stream that already fans every `library.scan.*` event to the
// browser) and exposes the live state of every active scan as a
// Map keyed by library_id. The admin "/libraries" page renders a
// banner per active row from this map; consumers that don't care
// just don't import the hook.
//
// Why a hook instead of a context provider: the SSE stream is
// already shared by EventSource semantics (one connection per page
// is fine), and admin views that don't render scan state shouldn't
// pay the connection cost. When we have a second admin view that
// needs the same data we'll lift to a context — for now this is
// the simplest thing that works.
//
// Lifecycle: connection opens on mount, closes on unmount. While
// connected we replay nothing — the banner shows up only once a
// new started/progress event arrives, which matches the operator's
// mental model (they pressed "Scan" → banner appears).

export interface ScanProgress {
  libraryId: string;
  libraryName: string;
  scanned: number;
  currentPath?: string;
  startedAt: number; // ms epoch
}

interface ScanEventData {
  library_id?: string;
  library_name?: string;
  scanned?: number;
  current_path?: string;
}

export function useScanProgress(): Map<string, ScanProgress> {
  const [scans, setScans] = useState<Map<string, ScanProgress>>(new Map());

  useEffect(() => {
    const es = new EventSource("/api/v1/events", { withCredentials: true });

    const handleStart = (ev: MessageEvent<string>) => {
      try {
        const payload = JSON.parse(ev.data) as { data?: ScanEventData };
        const d = payload.data;
        if (!d?.library_id) return;
        setScans((prev) => {
          const next = new Map(prev);
          next.set(d.library_id!, {
            libraryId: d.library_id!,
            libraryName: d.library_name || "—",
            scanned: 0,
            startedAt: Date.now(),
          });
          return next;
        });
      } catch {
        // Malformed payload — ignore rather than tear down.
      }
    };

    const handleProgress = (ev: MessageEvent<string>) => {
      try {
        const payload = JSON.parse(ev.data) as { data?: ScanEventData };
        const d = payload.data;
        if (!d?.library_id) return;
        setScans((prev) => {
          const next = new Map(prev);
          const existing = next.get(d.library_id!);
          next.set(d.library_id!, {
            libraryId: d.library_id!,
            libraryName: d.library_name || existing?.libraryName || "—",
            scanned: d.scanned ?? existing?.scanned ?? 0,
            currentPath: d.current_path,
            startedAt: existing?.startedAt ?? Date.now(),
          });
          return next;
        });
      } catch {
        // Same rationale as handleStart.
      }
    };

    const handleComplete = (ev: MessageEvent<string>) => {
      try {
        const payload = JSON.parse(ev.data) as { data?: ScanEventData };
        const d = payload.data;
        if (!d?.library_id) return;
        setScans((prev) => {
          const next = new Map(prev);
          next.delete(d.library_id!);
          return next;
        });
      } catch {
        // ignore
      }
    };

    es.addEventListener("library.scan.started", handleStart);
    es.addEventListener("library.scan.progress", handleProgress);
    es.addEventListener("library.scan.completed", handleComplete);

    return () => {
      es.removeEventListener("library.scan.started", handleStart);
      es.removeEventListener("library.scan.progress", handleProgress);
      es.removeEventListener("library.scan.completed", handleComplete);
      es.close();
    };
  }, []);

  return scans;
}
