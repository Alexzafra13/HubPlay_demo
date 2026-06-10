import { useEffect } from "react";
import { api } from "@/api/client";

/**
 * Si el usuario cierra la pestaña o navega fuera (back button, address
 * bar) sin pulsar el botón de cerrar del player, se filtraría la
 * sesión de transcode durante la ventana de idle del servidor (~90 s).
 * Engancha `pagehide` (más fiable que `beforeunload`; también dispara
 * en iOS Safari y al evict del bfcache) y lanza un DELETE best-effort
 * con `keepalive: true` (lo añade `api.stopStreamSession`) para que
 * sobreviva al unload. El reaper idle del servidor sigue ahí como
 * backstop si incluso keepalive cae.
 */
export function useStreamSessionCleanup(itemId: string, peerId?: string): void {
  useEffect(() => {
    // Federado: itemId es el id REMOTO — el DELETE local 404eaba y la
    // sesión de transcode del peer quedaba viva hasta su reaper. No
    // hay endpoint de stop remoto (decisión: el origen no sabe quién
    // mira); el idle-reaper del peer es el mecanismo correcto (PB-17).
    if (peerId) return;
    const onPageHide = () => {
      void api.stopStreamSession(itemId).catch(() => {
        // Best-effort: el navegador puede haber tirado fetch ya.
      });
    };
    window.addEventListener("pagehide", onPageHide);
    return () => window.removeEventListener("pagehide", onPageHide);
  }, [itemId, peerId]);
}
