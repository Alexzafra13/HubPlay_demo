import { useCallback, useMemo } from "react";
import type { RefObject } from "react";
import type { MediaStream } from "@/api/types";

/**
 * IDs disjuntos para los cuatro orígenes de subtítulos. El dispatcher
 * `handleSubtitleTrackChange` puede distinguir cada tipo con un único
 * if-ladder de límites, sin tablas auxiliares.
 *
 * - HLS-native (hls.js):     0 .. 9999
 * - Federados (peer):        10000 .. 19999
 * - Burn-in (transcoder):    20000 .. 29999 (BURN_SUB_TRACK_ID_BASE + perTypeIdx)
 * - Texto local (WebVTT):    30000+ (LOCAL_TEXT_TRACK_ID_BASE + perTypeIdx)
 */
const FEDERATED_TRACK_ID_BASE = 10000;
const BURN_SUB_TRACK_ID_BASE = 20000;
const LOCAL_TEXT_TRACK_ID_BASE = 30000;

/**
 * Codecs que el browser no decodifica nativamente — para ellos ffmpeg
 * los quema en el video (burn-in). SRT/WebVTT son texto y van por el
 * carril LOCAL_TEXT (extraídos a WebVTT por el backend y montados como
 * `<track>`).
 * Module-scope: la identidad del Set se mantiene estable entre renders,
 * el useMemo deps queda correcto.
 */
const BURNABLE_CODECS = new Set([
  "hdmv_pgs_subtitle", "pgs",
  "dvd_subtitle", "dvdsub",
  "dvb_subtitle", "dvbsub",
  "xsub",
  "ass", "ssa",
]);

/**
 * Codecs de subtítulo de TEXTO embebidos en el fichero (SRT del MKV,
 * mov_text del MP4…). El backend los extrae a WebVTT bajo demanda
 * (`/stream/{id}/subtitles/{absIndex}`) y el player los monta como
 * `<track>` nativo. Antes este carril NO existía: el comentario de
 * arriba asumía que llegarían como tracks de hls.js, pero el master
 * sintético no emite renditions de subtítulos — así que los SRT
 * embebidos (el caso más común) directamente no aparecían en el
 * picker. Reporte de usuario 2026-06-10 (PB-41).
 */
const TEXT_SUB_CODECS = new Set([
  "subrip", "srt",
  "webvtt", "vtt",
  "mov_text", "text",
]);

interface SubtitleTrackEntry {
  id: number;
  name: string;
  lang: string;
}

interface BurnInTrackEntry extends SubtitleTrackEntry {
  burnIn: true;
}

interface FederatedSubTrack {
  index: number;
  language: string;
  title: string;
  default: boolean;
  forced: boolean;
}

interface UseSubtitleSelectionOptions {
  videoRef: RefObject<HTMLVideoElement | null>;
  /** Tracks expuestos por hls.js (el variant-stream del playlist). */
  hlsTracks: SubtitleTrackEntry[];
  /** Track HLS actualmente activo (-1 = ninguno). */
  currentHlsTrack: number;
  /** Cambia el track HLS activo. -1 desactiva subs nativos. */
  setHlsTrack: (id: number) => void;

  peerId?: string;
  peerStreamSessionId?: string;
  federatedSubs: FederatedSubTrack[];
  activeFederatedSubIndex: number | null;
  setActiveFederatedSubIndex: (idx: number | null) => void;

  /** MediaStream rows de la DB (audio + subtitle). El hook filtra a subs. */
  subtitleStreams?: MediaStream[];
  /** Índice per-tipo del sub que se está quemando ahora (-1 = ninguno). */
  burnSubtitleIndex: number;
  /** Callback al padre para re-mountar master con `?subtitle=N`. */
  onBurnSubtitleSelected?: (idx: number, currentTimeSeconds: number) => void;

  /** Índice ABSOLUTO (ffmpeg `0:N`) del sub de texto local activo, o
   *  null. El absoluto (no per-type) es lo que consume la URL del
   *  endpoint de extracción WebVTT. */
  activeLocalSubIndex: number | null;
  setActiveLocalSubIndex: (idx: number | null) => void;
}

interface UseSubtitleSelectionReturn {
  mergedSubtitleTracks: (SubtitleTrackEntry | BurnInTrackEntry)[];
  effectiveCurrentSubtitleTrack: number;
  handleSubtitleTrackChange: (id: number) => void;
}

/**
 * Unifica los tres orígenes de subtítulos (HLS-native, federados,
 * burn-in DB) en una sola lista y un único handler de cambio. El
 * picker en PlayerControls ve un array plano; el routing por ID-base
 * lo absorbe este hook.
 */
export function useSubtitleSelection({
  videoRef,
  hlsTracks,
  currentHlsTrack,
  setHlsTrack,
  peerId,
  peerStreamSessionId,
  federatedSubs,
  activeFederatedSubIndex,
  setActiveFederatedSubIndex,
  subtitleStreams,
  burnSubtitleIndex,
  onBurnSubtitleSelected,
  activeLocalSubIndex,
  setActiveLocalSubIndex,
}: UseSubtitleSelectionOptions): UseSubtitleSelectionReturn {
  const burnInSubtitleEntries = useMemo<BurnInTrackEntry[]>(() => {
    if (!subtitleStreams || !onBurnSubtitleSelected) return [];
    const out: BurnInTrackEntry[] = [];
    let subOrd = -1;
    for (const s of subtitleStreams) {
      if (s.type !== "subtitle") continue;
      subOrd++;
      if (!BURNABLE_CODECS.has((s.codec || "").toLowerCase())) continue;
      out.push({
        id: BURN_SUB_TRACK_ID_BASE + subOrd,
        name: s.title || s.language || `Track ${subOrd + 1}`,
        lang: s.language || "",
        burnIn: true,
      });
    }
    return out;
  }, [subtitleStreams, onBurnSubtitleSelected]);

  // Pistas de texto embebidas (SRT/mov_text…). Cada entrada lleva el
  // índice ABSOLUTO del stream (s.index) — es lo que consume la URL de
  // extracción WebVTT del backend.
  const localTextEntries = useMemo<
    (SubtitleTrackEntry & { absIndex: number })[]
  >(() => {
    if (!subtitleStreams) return [];
    const out: (SubtitleTrackEntry & { absIndex: number })[] = [];
    let subOrd = -1;
    for (const s of subtitleStreams) {
      if (s.type !== "subtitle") continue;
      subOrd++;
      if (!TEXT_SUB_CODECS.has((s.codec || "").toLowerCase())) continue;
      out.push({
        id: LOCAL_TEXT_TRACK_ID_BASE + subOrd,
        name: s.title || s.language || `Track ${subOrd + 1}`,
        lang: s.language || "",
        absIndex: s.index,
      });
    }
    return out;
  }, [subtitleStreams]);

  const showFederatedTracks =
    !!peerId && !!peerStreamSessionId && federatedSubs.length > 0;

  const mergedSubtitleTracks = useMemo<
    (SubtitleTrackEntry | BurnInTrackEntry)[]
  >(
    () => [
      ...hlsTracks,
      ...(showFederatedTracks
        ? federatedSubs.map((s, i) => ({
            id: FEDERATED_TRACK_ID_BASE + i,
            name: s.title || s.language || `Track ${s.index}`,
            lang: s.language || "",
          }))
        : []),
      ...burnInSubtitleEntries,
      ...localTextEntries,
    ],
    [hlsTracks, showFederatedTracks, federatedSubs, burnInSubtitleEntries, localTextEntries],
  );

  // El activo local se guarda como índice absoluto; el picker habla en
  // IDs — reverse-lookup para marcar el check.
  const activeLocalEntryID = useMemo<number | null>(() => {
    if (activeLocalSubIndex === null) return null;
    const entry = localTextEntries.find((e) => e.absIndex === activeLocalSubIndex);
    return entry ? entry.id : null;
  }, [activeLocalSubIndex, localTextEntries]);

  const effectiveCurrentSubtitleTrack =
    activeLocalEntryID !== null
      ? activeLocalEntryID
      : activeFederatedSubIndex !== null
        ? FEDERATED_TRACK_ID_BASE + activeFederatedSubIndex
        : burnSubtitleIndex >= 0
          ? BURN_SUB_TRACK_ID_BASE + burnSubtitleIndex
          : currentHlsTrack;

  const handleSubtitleTrackChange = useCallback(
    (id: number) => {
      if (id >= LOCAL_TEXT_TRACK_ID_BASE) {
        // Texto local: el `<track>` WebVTT lo monta VideoPlayer al ver
        // el índice absoluto activo. Suprimir el resto de orígenes.
        const entry = localTextEntries.find((e) => e.id === id);
        if (!entry) return;
        setActiveFederatedSubIndex(null);
        setHlsTrack(-1);
        if (burnSubtitleIndex >= 0 && onBurnSubtitleSelected) {
          onBurnSubtitleSelected(-1, videoRef.current?.currentTime ?? 0);
        }
        setActiveLocalSubIndex(entry.absIndex);
        return;
      }
      if (id >= BURN_SUB_TRACK_ID_BASE) {
        // Burn-in: limpiar todo otro origen de subs antes de re-montar
        // el master con `?subtitle=N`. La currentTime del playhead va al
        // padre para que la nueva manifest seekee al mismo punto y la
        // seam sea invisible.
        if (!onBurnSubtitleSelected) return;
        setActiveFederatedSubIndex(null);
        setActiveLocalSubIndex(null);
        setHlsTrack(-1);
        const subIdx = id - BURN_SUB_TRACK_ID_BASE;
        onBurnSubtitleSelected(subIdx, videoRef.current?.currentTime ?? 0);
        return;
      }
      if (id >= FEDERATED_TRACK_ID_BASE) {
        // Federado: suprimir HLS, externo y local. Sólo un set de cues a la vez.
        setActiveFederatedSubIndex(id - FEDERATED_TRACK_ID_BASE);
        setActiveLocalSubIndex(null);
        setHlsTrack(-1);
        return;
      }
      // HLS path (o "off" con id=-1). Limpia federado y local para que
      // sus `<track>` desmonten; si había burn-in activo, también pide
      // al padre que apague la transcoder con `?subtitle=-1` antes de
      // cambiar al HLS o "off".
      setActiveFederatedSubIndex(null);
      setActiveLocalSubIndex(null);
      if (burnSubtitleIndex >= 0 && onBurnSubtitleSelected) {
        onBurnSubtitleSelected(-1, videoRef.current?.currentTime ?? 0);
      }
      setHlsTrack(id);
    },
    [
      videoRef,
      setHlsTrack,
      onBurnSubtitleSelected,
      burnSubtitleIndex,
      setActiveFederatedSubIndex,
      setActiveLocalSubIndex,
      localTextEntries,
    ],
  );

  return {
    mergedSubtitleTracks,
    effectiveCurrentSubtitleTrack,
    handleSubtitleTrackChange,
  };
}
