import { useEffect, useState } from "react";
import type { RefObject } from "react";
import { api } from "@/api/client";

interface FederatedSubTrack {
  index: number;
  language: string;
  title: string;
  default: boolean;
  forced: boolean;
}

interface UseFederatedSubsOptions {
  videoRef: RefObject<HTMLVideoElement | null>;
  peerId?: string;
  peerStreamSessionId?: string;
}

interface UseFederatedSubsReturn {
  federatedSubs: FederatedSubTrack[];
  activeFederatedSubIndex: number | null;
  setActiveFederatedSubIndex: (idx: number | null) => void;
}

/**
 * Encapsula la integración de subtítulos federados:
 *
 * - Fetch del listado al montar cuando hay `peerId + peerStreamSessionId`
 *   (con cleanup por `cancelled` para evitar setState en componente
 *   desmontado).
 * - Estado del track activo (índice o null).
 * - Effect que fuerza `track.mode = "showing"` tras montar un
 *   `<track kind="subtitles" label="Federated:...">` — el navegador
 *   deja todos los tracks en "disabled" por defecto y necesitamos
 *   activar el elegido y desactivar el resto para que no se solapen
 *   con un sub externo / HLS.
 *
 * El master.m3u8 que devuelve un peer no transporta `EXT-X-MEDIA
 * SUBTITLES`, así que los pintamos como `<track>` hijos del `<video>`
 * (mismo plumbing que los subs externos de OpenSubtitles).
 */
export function useFederatedSubs({
  videoRef,
  peerId,
  peerStreamSessionId,
}: UseFederatedSubsOptions): UseFederatedSubsReturn {
  const [federatedSubs, setFederatedSubs] = useState<FederatedSubTrack[]>([]);
  const [activeFederatedSubIndex, setActiveFederatedSubIndex] = useState<
    number | null
  >(null);

  useEffect(() => {
    if (!peerId || !peerStreamSessionId) return;
    let cancelled = false;
    api
      .listFederatedSubtitles(peerId, peerStreamSessionId)
      .then((tracks) => {
        if (!cancelled) setFederatedSubs(tracks);
      })
      .catch(() => {
        // Silent: si falla el fetch, el dropdown sólo mostrará los
        // HLS tracks (típicamente vacíos en federado) y el usuario
        // mantiene la opción de subs externos via OpenSubtitles.
      });
    return () => {
      cancelled = true;
    };
  }, [peerId, peerStreamSessionId]);

  useEffect(() => {
    const video = videoRef.current;
    if (!video || activeFederatedSubIndex === null) return;
    const rafID = window.requestAnimationFrame(() => {
      const tracks = Array.from(video.textTracks);
      const target = tracks.find((t) => t.label.startsWith("Federated:"));
      if (target) target.mode = "showing";
      for (const t of tracks) {
        if (t !== target && t.mode === "showing") {
          t.mode = "disabled";
        }
      }
    });
    return () => window.cancelAnimationFrame(rafID);
  }, [videoRef, activeFederatedSubIndex]);

  return {
    federatedSubs,
    activeFederatedSubIndex,
    setActiveFederatedSubIndex,
  };
}
