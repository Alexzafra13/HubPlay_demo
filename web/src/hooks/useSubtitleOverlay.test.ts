import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook } from "@testing-library/react";
import { useSubtitleOverlay } from "./useSubtitleOverlay";
import type { RefObject } from "react";

// PB-44 (reporte de usuario 2026-06-10): el render nativo de WebVTT
// pintaba los cues en el borde inferior del ELEMENTO de vídeo —
// pisando los controles, recortados en móvil y solapándose. El hook
// pone la pista activa en "hidden" y pinta los cues en un overlay
// propio.

type Listener = () => void;

type FakeCue = { text: string };

class FakeTrack {
  label: string;
  mode: "showing" | "disabled" | "hidden" = "disabled";
  activeCues: FakeCue[] = [];
  private listeners = new Set<Listener>();

  constructor(label: string) {
    this.label = label;
  }
  addEventListener(_ev: string, fn: Listener) {
    this.listeners.add(fn);
  }
  removeEventListener(_ev: string, fn: Listener) {
    this.listeners.delete(fn);
  }
  fireCueChange(cues: FakeCue[]) {
    this.activeCues = cues;
    for (const fn of this.listeners) fn();
  }
}

function makeRefs(tracks: FakeTrack[]): {
  videoRef: RefObject<HTMLVideoElement | null>;
  overlayRef: RefObject<HTMLDivElement | null>;
  overlay: HTMLDivElement;
} {
  const video = { textTracks: tracks } as unknown as HTMLVideoElement;
  const overlay = document.createElement("div");
  return { videoRef: { current: video }, overlayRef: { current: overlay }, overlay };
}

beforeEach(() => {
  vi.useFakeTimers();
  // jsdom no implementa rAF de manera consistente — lo apuntamos a
  // setTimeout(0) para que el flush sea determinista con runAllTimers.
  vi.stubGlobal(
    "requestAnimationFrame",
    (cb: FrameRequestCallback) => setTimeout(() => cb(performance.now()), 0),
  );
  vi.stubGlobal("cancelAnimationFrame", (id: number) =>
    clearTimeout(id as unknown as NodeJS.Timeout),
  );
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("useSubtitleOverlay", () => {
  it("pone la pista gestionada en 'hidden' (nunca showing — el overlay es el único render)", () => {
    const local = new FakeTrack("Local:3");
    const { videoRef, overlayRef } = makeRefs([new FakeTrack("English (HLS)"), local]);

    renderHook(() =>
      useSubtitleOverlay({ videoRef, overlayRef, activeKey: "local:3" }),
    );
    vi.runAllTimers();

    expect(local.mode).toBe("hidden");
  });

  it("desactiva cualquier otra pista en showing (origen del solapamiento de cues)", () => {
    const stale = new FakeTrack("English (HLS)");
    stale.mode = "showing";
    const local = new FakeTrack("Local:3");
    const { videoRef, overlayRef } = makeRefs([stale, local]);

    renderHook(() =>
      useSubtitleOverlay({ videoRef, overlayRef, activeKey: "local:3" }),
    );
    vi.runAllTimers();

    expect(stale.mode).toBe("disabled");
  });

  it("pinta los cues activos en el overlay y los actualiza en cuechange", () => {
    const local = new FakeTrack("Local:3");
    const { videoRef, overlayRef, overlay } = makeRefs([local]);

    renderHook(() =>
      useSubtitleOverlay({ videoRef, overlayRef, activeKey: "local:3" }),
    );
    vi.runAllTimers();

    local.fireCueChange([{ text: "—¡Vamos, vamos!" }, { text: "—Vale, vale." }]);
    const lines = Array.from(overlay.querySelectorAll(".hp-cue"));
    expect(lines.map((l) => l.textContent)).toEqual(["—¡Vamos, vamos!", "—Vale, vale."]);

    local.fireCueChange([]);
    expect(overlay.children).toHaveLength(0);
  });

  it("gestiona también las pistas federadas por su prefijo de label", () => {
    const fed = new FakeTrack("Federated:es");
    const { videoRef, overlayRef, overlay } = makeRefs([fed]);

    renderHook(() =>
      useSubtitleOverlay({ videoRef, overlayRef, activeKey: "fed:0" }),
    );
    vi.runAllTimers();
    fed.fireCueChange([{ text: "hola" }]);

    expect(fed.mode).toBe("hidden");
    expect(overlay.textContent).toBe("hola");
  });

  it("sin activeKey limpia el overlay y no toca pistas", () => {
    const local = new FakeTrack("Local:3");
    const { videoRef, overlayRef, overlay } = makeRefs([local]);
    overlay.append(document.createElement("div"));

    renderHook(() =>
      useSubtitleOverlay({ videoRef, overlayRef, activeKey: null }),
    );
    vi.runAllTimers();

    expect(overlay.children).toHaveLength(0);
    expect(local.mode).toBe("disabled");
  });

  it("al desmontar limpia el overlay y suelta el listener", () => {
    const local = new FakeTrack("Local:3");
    const { videoRef, overlayRef, overlay } = makeRefs([local]);

    const hook = renderHook(() =>
      useSubtitleOverlay({ videoRef, overlayRef, activeKey: "local:3" }),
    );
    vi.runAllTimers();
    local.fireCueChange([{ text: "hola" }]);
    expect(overlay.children).toHaveLength(1);

    hook.unmount();
    expect(overlay.children).toHaveLength(0);
    // El listener se soltó: un cuechange posterior no repinta nada.
    local.fireCueChange([{ text: "adiós" }]);
    expect(overlay.children).toHaveLength(0);
  });
});
