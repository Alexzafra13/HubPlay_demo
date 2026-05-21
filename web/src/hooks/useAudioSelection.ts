import { useCallback, useMemo } from "react";
import type { RefObject } from "react";
import type { i18n as I18nInstance } from "i18next";
import {
  buildPickerTracksFromDB,
  type AudioTrack,
} from "@/components/player/audioTracks";
import type { MediaStream } from "@/api/types";

interface UseAudioSelectionOptions {
  videoRef: RefObject<HTMLVideoElement | null>;
  i18n: I18nInstance;
  /** Tracks expuestos por hls.js (uno solo en sesiones transcoded). */
  hlsAudioTracks: AudioTrack[];
  currentHlsAudioTrack: number;
  setHlsAudioTrack: (id: number) => void;
  /** MediaStream rows de la DB (audio + subtitle). El hook filtra a audio. */
  audioStreams?: MediaStream[];
  /** Índice per-tipo del audio que está reproduciéndose (-1 = default). */
  audioStreamIndex: number;
  /** Callback al padre para re-mountar master con `?audio=N`. */
  onAudioStreamSelected?: (idx: number, currentTimeSeconds: number) => void;
}

interface UseAudioSelectionReturn {
  /** Lista a mostrar en el picker (DB-driven cuando hay rica metadata). */
  displayAudioTracks: AudioTrack[];
  /** Índice del checked en el picker. */
  displayCurrentAudioTrack: number;
  /** True si el picker está usando la lista DB-driven con labels ricos. */
  useDbAudioPicker: boolean;
  handleAudioTrackChange: (id: number) => void;
}

/**
 * Resuelve la asimetría entre "hls.js sólo ve UN audio track en sesiones
 * transcoded" y "el fichero realmente tiene N tracks en DB". Cuando el
 * padre provee `audioStreams` + `onAudioStreamSelected` construimos el
 * picker desde la DB (labels ricos tipo "Castellano · DD+ 5.1 ·
 * Predeterminado"); fallback al listado de hls.js si no.
 */
export function useAudioSelection({
  videoRef,
  i18n,
  hlsAudioTracks,
  currentHlsAudioTrack,
  setHlsAudioTrack,
  audioStreams,
  audioStreamIndex,
  onAudioStreamSelected,
}: UseAudioSelectionOptions): UseAudioSelectionReturn {
  const audioLocale: "es" | "en" = i18n.language?.startsWith("en")
    ? "en"
    : "es";

  const dbDrivenAudioTracks = useMemo<AudioTrack[]>(() => {
    if (!audioStreams || !onAudioStreamSelected) return [];
    return buildPickerTracksFromDB(
      audioStreams,
      audioLocale,
      audioLocale === "es" ? "Predeterminado" : "Default",
    );
  }, [audioStreams, onAudioStreamSelected, audioLocale]);

  const useDbAudioPicker = dbDrivenAudioTracks.length > 1;

  // -1 del padre significa "usar default del fichero". Resolvemos al
  // row con `is_default` para que el picker muestre el check; sin
  // default flag, primer audio. (Mismo UX que Jellyfin.)
  const defaultStreamPerTypeIndex = useMemo<number>(() => {
    if (!audioStreams) return 0;
    let idx = -1;
    let firstAudio = -1;
    for (const s of audioStreams) {
      if (s.type !== "audio") continue;
      idx++;
      if (firstAudio === -1) firstAudio = idx;
      if (s.is_default) return idx;
    }
    return firstAudio === -1 ? 0 : firstAudio;
  }, [audioStreams]);

  const displayAudioTracks = useDbAudioPicker
    ? dbDrivenAudioTracks
    : hlsAudioTracks;
  const displayCurrentAudioTrack = useDbAudioPicker
    ? audioStreamIndex >= 0
      ? audioStreamIndex
      : defaultStreamPerTypeIndex
    : currentHlsAudioTrack;

  const handleAudioTrackChange = useCallback(
    (id: number) => {
      if (useDbAudioPicker && onAudioStreamSelected) {
        // Capturar playhead live (no del state) para que la nueva
        // manifest resuma exactamente donde estaba el usuario.
        const at = videoRef.current?.currentTime ?? 0;
        onAudioStreamSelected(id, at);
        return;
      }
      setHlsAudioTrack(id);
    },
    [useDbAudioPicker, onAudioStreamSelected, setHlsAudioTrack, videoRef],
  );

  return {
    displayAudioTracks,
    displayCurrentAudioTrack,
    useDbAudioPicker,
    handleAudioTrackChange,
  };
}
