import { useEffect, useState } from "react";
import { api } from "@/api/client";

interface FederatedSubTrack {
  index: number;
  language: string;
  title: string;
  default: boolean;
  forced: boolean;
}

interface UseFederatedSubsOptions {
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
 * - El render del track activo lo hace useSubtitleOverlay (por el
 *   prefijo de label "Federated:").
 *
 * El master.m3u8 que devuelve un peer no transporta `EXT-X-MEDIA
 * SUBTITLES`, así que los pintamos como `<track>` hijos del `<video>`
 * (mismo plumbing que los subs externos de OpenSubtitles).
 */
export function useFederatedSubs({
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

  // El forcing de `mode` y el render de cues viven ahora en
  // useSubtitleOverlay (PB-44): la pista federada se gestiona por su
  // prefijo de label "Federated:" igual que las de texto local.

  return {
    federatedSubs,
    activeFederatedSubIndex,
    setActiveFederatedSubIndex,
  };
}
