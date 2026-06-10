import { describe, it, expect, vi } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { useSubtitleSelection } from "./useSubtitleSelection";
import type { MediaStream } from "@/api/types";

// Reporte de usuario 2026-06-10 (PB-41): los SRT embebidos en el MKV no
// aparecían en el picker — el hook solo fusionaba hls.js (que en el HLS
// sintético nunca trae subtítulos), federados y burn-in (PGS/ASS). El
// carril nuevo de "texto local" los expone vía extracción WebVTT.

function makeSubStream(over: Partial<MediaStream>): MediaStream {
  return {
    index: 0,
    type: "subtitle",
    codec: "subrip",
    language: null,
    title: null,
    channels: null,
    width: null,
    height: null,
    bitrate: null,
    is_default: false,
    is_forced: false,
    hdr_type: null,
    ...over,
  };
}

// El fichero de la captura: 3 pistas SUBRIP (índices absolutos 2,3,4
// tras vídeo+audio) + una PGS burnable para verificar la coexistencia.
const streams: MediaStream[] = [
  makeSubStream({ index: 2, codec: "subrip", language: "spa", title: "Castellano Forzados", is_forced: true, is_default: true }),
  makeSubStream({ index: 3, codec: "subrip", language: "spa", title: "Castellano Completos" }),
  makeSubStream({ index: 4, codec: "subrip", language: "eng", title: "Inglés Completos" }),
  makeSubStream({ index: 5, codec: "hdmv_pgs_subtitle", language: "eng", title: "PGS" }),
];

function mount(overrides: Partial<Parameters<typeof useSubtitleSelection>[0]> = {}) {
  const video = document.createElement("video");
  const setHlsTrack = vi.fn();
  const setActiveFederatedSubIndex = vi.fn();
  const setActiveLocalSubIndex = vi.fn();
  const onBurnSubtitleSelected = vi.fn();
  const hook = renderHook(() =>
    useSubtitleSelection({
      videoRef: { current: video },
      hlsTracks: [],
      currentHlsTrack: -1,
      setHlsTrack,
      federatedSubs: [],
      activeFederatedSubIndex: null,
      setActiveFederatedSubIndex,
      subtitleStreams: streams,
      burnSubtitleIndex: -1,
      onBurnSubtitleSelected,
      activeLocalSubIndex: null,
      setActiveLocalSubIndex,
      ...overrides,
    }),
  );
  return {
    hook,
    setHlsTrack,
    setActiveLocalSubIndex,
    setActiveFederatedSubIndex,
    onBurnSubtitleSelected,
  };
}

describe("useSubtitleSelection — pistas de texto locales (PB-41)", () => {
  it("lista las pistas SUBRIP embebidas junto a las burn-in", () => {
    const { hook } = mount();
    const names = hook.result.current.mergedSubtitleTracks.map((t) => t.name);
    expect(names).toContain("Castellano Forzados");
    expect(names).toContain("Castellano Completos");
    expect(names).toContain("Inglés Completos");
    expect(names).toContain("PGS"); // burn-in sigue presente
    expect(hook.result.current.mergedSubtitleTracks).toHaveLength(4);
  });

  it("seleccionar una pista de texto activa su índice ABSOLUTO y suprime el resto de orígenes", () => {
    const { hook, setHlsTrack, setActiveLocalSubIndex } = mount();
    const completos = hook.result.current.mergedSubtitleTracks.find(
      (t) => t.name === "Castellano Completos",
    )!;

    act(() => {
      hook.result.current.handleSubtitleTrackChange(completos.id);
    });

    // index absoluto 3 (no el ordinal per-tipo 1) — es lo que consume
    // la URL del extractor WebVTT del backend.
    expect(setActiveLocalSubIndex).toHaveBeenCalledWith(3);
    expect(setHlsTrack).toHaveBeenCalledWith(-1);
  });

  it("la pista local activa se refleja como seleccionada en el picker", () => {
    const { hook } = mount({ activeLocalSubIndex: 4 });
    const ingles = hook.result.current.mergedSubtitleTracks.find(
      (t) => t.name === "Inglés Completos",
    )!;
    expect(hook.result.current.effectiveCurrentSubtitleTrack).toBe(ingles.id);
  });

  it("apagar los subtítulos (id -1) limpia la pista local", () => {
    const { hook, setActiveLocalSubIndex, setHlsTrack } = mount({ activeLocalSubIndex: 3 });

    act(() => {
      hook.result.current.handleSubtitleTrackChange(-1);
    });

    expect(setActiveLocalSubIndex).toHaveBeenCalledWith(null);
    expect(setHlsTrack).toHaveBeenCalledWith(-1);
  });

  it("seleccionar burn-in (PGS) limpia la pista local", () => {
    const { hook, setActiveLocalSubIndex, onBurnSubtitleSelected } = mount({ activeLocalSubIndex: 3 });
    const pgs = hook.result.current.mergedSubtitleTracks.find((t) => t.name === "PGS")!;

    act(() => {
      hook.result.current.handleSubtitleTrackChange(pgs.id);
    });

    expect(setActiveLocalSubIndex).toHaveBeenCalledWith(null);
    // PGS es el sub per-tipo 3 (cuarto subtítulo del fichero).
    expect(onBurnSubtitleSelected).toHaveBeenCalledWith(3, expect.any(Number));
  });

  it("sin streams en DB no aparecen pistas locales (item federado/peer)", () => {
    const { hook } = mount({ subtitleStreams: undefined });
    expect(hook.result.current.mergedSubtitleTracks).toHaveLength(0);
  });
});
