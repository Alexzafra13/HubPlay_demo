import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook } from "@testing-library/react";
import { useExternalSubMode } from "./useExternalSubMode";
import type { RefObject } from "react";

type FakeTrack = {
  label: string;
  mode: "showing" | "disabled" | "hidden";
};

function makeVideoRef(tracks: FakeTrack[]): {
  ref: RefObject<HTMLVideoElement | null>;
} {
  const video = {
    textTracks: tracks,
  } as unknown as HTMLVideoElement;
  return { ref: { current: video } };
}

beforeEach(() => {
  vi.useFakeTimers();
  // jsdom no implementa rAF de manera consistente — lo apuntamos
  // a setTimeout(0) para que el flush sea determinista con
  // vi.runAllTimers().
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

describe("useExternalSubMode", () => {
  it("activa el track externo en el rAF cuando hay activeKey", () => {
    const tracks: FakeTrack[] = [
      { label: "English (HLS)", mode: "disabled" },
      { label: "External:es", mode: "disabled" },
    ];
    const { ref } = makeVideoRef(tracks);

    renderHook(() =>
      useExternalSubMode({ videoRef: ref, activeKey: "opensub:fid-1" }),
    );

    expect(tracks[1].mode).toBe("disabled"); // aún no, rAF pendiente
    vi.runAllTimers();
    expect(tracks[1].mode).toBe("showing");
  });

  it("suprime cualquier otro track que estuviera en showing", () => {
    const tracks: FakeTrack[] = [
      { label: "English (HLS)", mode: "showing" },
      { label: "External:es", mode: "disabled" },
    ];
    const { ref } = makeVideoRef(tracks);

    renderHook(() =>
      useExternalSubMode({ videoRef: ref, activeKey: "opensub:fid-1" }),
    );

    vi.runAllTimers();

    expect(tracks[0].mode).toBe("disabled");
    expect(tracks[1].mode).toBe("showing");
  });

  it("no toca nada si no hay track con prefijo External:", () => {
    const tracks: FakeTrack[] = [
      { label: "English (HLS)", mode: "showing" },
      { label: "Spanish (HLS)", mode: "disabled" },
    ];
    const { ref } = makeVideoRef(tracks);

    renderHook(() =>
      useExternalSubMode({ videoRef: ref, activeKey: "opensub:fid-1" }),
    );

    vi.runAllTimers();

    // El loop "suprime cualquier otro en showing" SÍ corre — sin
    // target todo lo que estaba en showing pasa a disabled. Lo
    // documentamos como comportamiento intencional: si el caller
    // activó la rama pero el <track> aún no existe en el DOM,
    // dejamos al menos los HLS limpios para que no compitan.
    expect(tracks[0].mode).toBe("disabled");
    expect(tracks[1].mode).toBe("disabled");
  });

  it("no-op cuando activeKey es null", () => {
    const tracks: FakeTrack[] = [
      { label: "External:es", mode: "disabled" },
    ];
    const { ref } = makeVideoRef(tracks);

    renderHook(() =>
      useExternalSubMode({ videoRef: ref, activeKey: null }),
    );

    vi.runAllTimers();

    expect(tracks[0].mode).toBe("disabled");
  });

  it("no-op cuando videoRef.current es null", () => {
    const ref: RefObject<HTMLVideoElement | null> = { current: null };

    expect(() =>
      renderHook(() =>
        useExternalSubMode({ videoRef: ref, activeKey: "k1" }),
      ),
    ).not.toThrow();
    vi.runAllTimers();
  });

  it("cancela el rAF en unmount si aún no se disparó", () => {
    const tracks: FakeTrack[] = [
      { label: "External:es", mode: "disabled" },
    ];
    const { ref } = makeVideoRef(tracks);

    const { unmount } = renderHook(() =>
      useExternalSubMode({ videoRef: ref, activeKey: "k1" }),
    );

    unmount();
    vi.runAllTimers();

    // Si el cancel funcionó, el track sigue en disabled.
    expect(tracks[0].mode).toBe("disabled");
  });

  it("rerender con nueva activeKey re-ejecuta el effect", () => {
    const tracks: FakeTrack[] = [
      { label: "External:es", mode: "disabled" },
    ];
    const { ref } = makeVideoRef(tracks);

    const { rerender } = renderHook(
      ({ k }: { k: string }) =>
        useExternalSubMode({ videoRef: ref, activeKey: k }),
      { initialProps: { k: "k1" } },
    );

    vi.runAllTimers();
    expect(tracks[0].mode).toBe("showing");

    // Usuario picks un sub diferente — track sigue siendo el mismo
    // con el prefijo "External:" pero el effect debe re-ejecutarse.
    tracks[0].mode = "disabled";
    rerender({ k: "k2" });
    vi.runAllTimers();

    expect(tracks[0].mode).toBe("showing");
  });
});
