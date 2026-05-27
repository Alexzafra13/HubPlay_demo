import { describe, it, expect, vi } from "vitest";
import { renderHook } from "@testing-library/react";
import { useStartPositionSeek } from "./useStartPositionSeek";
import type { RefObject } from "react";

interface FakeVideo {
  currentTime: number;
  addEventListener: ReturnType<typeof vi.fn>;
  removeEventListener: ReturnType<typeof vi.fn>;
  _fireCanPlay: () => void;
}

function makeVideoRef(): {
  ref: RefObject<HTMLVideoElement | null>;
  video: FakeVideo;
} {
  const listeners: Array<() => void> = [];
  const video: FakeVideo = {
    currentTime: 0,
    addEventListener: vi.fn((event: string, cb: () => void) => {
      if (event === "canplay") listeners.push(cb);
    }),
    removeEventListener: vi.fn((event: string, cb: () => void) => {
      if (event === "canplay") {
        const i = listeners.indexOf(cb);
        if (i >= 0) listeners.splice(i, 1);
      }
    }),
    _fireCanPlay: () => {
      // Disparo a TODOS los listeners actualmente registrados.
      for (const cb of [...listeners]) cb();
    },
  };
  return { ref: { current: video as unknown as HTMLVideoElement }, video };
}

describe("useStartPositionSeek", () => {
  it("no registra listener cuando startPosition es 0", () => {
    const { ref, video } = makeVideoRef();

    renderHook(() =>
      useStartPositionSeek({ videoRef: ref, startPosition: 0, sourceKey: "s1" }),
    );

    expect(video.addEventListener).not.toHaveBeenCalled();
  });

  it("no registra listener cuando startPosition es undefined", () => {
    const { ref, video } = makeVideoRef();

    renderHook(() =>
      useStartPositionSeek({
        videoRef: ref,
        startPosition: undefined,
        sourceKey: "s1",
      }),
    );

    expect(video.addEventListener).not.toHaveBeenCalled();
  });

  it("registra canplay y aplica el seek cuando dispara", () => {
    const { ref, video } = makeVideoRef();

    renderHook(() =>
      useStartPositionSeek({ videoRef: ref, startPosition: 120, sourceKey: "s1" }),
    );

    expect(video.addEventListener).toHaveBeenCalledWith(
      "canplay",
      expect.any(Function),
    );

    video._fireCanPlay();

    expect(video.currentTime).toBe(120);
  });

  it("ignora canplays adicionales tras el primero (gate del ref)", () => {
    const { ref, video } = makeVideoRef();

    renderHook(() =>
      useStartPositionSeek({ videoRef: ref, startPosition: 120, sourceKey: "s1" }),
    );

    video._fireCanPlay();
    expect(video.currentTime).toBe(120);

    // Simula que el buffer recuperó y el navegador volvió a emitir
    // canplay — el seek NO debe re-aplicarse.
    video.currentTime = 200; // playhead avanzó normalmente
    video._fireCanPlay();
    expect(video.currentTime).toBe(200);
  });

  it("cambio de sourceKey resetea el gate y re-seekea al siguiente canplay", () => {
    const { ref, video } = makeVideoRef();

    const { rerender } = renderHook(
      ({ key }: { key: string }) =>
        useStartPositionSeek({
          videoRef: ref,
          startPosition: 120,
          sourceKey: key,
        }),
      { initialProps: { key: "s1" } },
    );

    video._fireCanPlay();
    expect(video.currentTime).toBe(120);

    video.currentTime = 300;
    rerender({ key: "s2" });
    video._fireCanPlay();

    expect(video.currentTime).toBe(120);
  });

  it("cambio de startPosition antes del primer canplay usa el nuevo valor", () => {
    const { ref, video } = makeVideoRef();

    const { rerender } = renderHook(
      ({ pos }: { pos: number }) =>
        useStartPositionSeek({
          videoRef: ref,
          startPosition: pos,
          sourceKey: "s1",
        }),
      { initialProps: { pos: 60 } },
    );

    rerender({ pos: 90 });
    video._fireCanPlay();

    expect(video.currentTime).toBe(90);
  });

  it("retira el listener al desmontar", () => {
    const { ref, video } = makeVideoRef();

    const { unmount } = renderHook(() =>
      useStartPositionSeek({ videoRef: ref, startPosition: 120, sourceKey: "s1" }),
    );

    unmount();

    expect(video.removeEventListener).toHaveBeenCalledWith(
      "canplay",
      expect.any(Function),
    );
  });

  it("no-op cuando videoRef.current es null", () => {
    const ref: RefObject<HTMLVideoElement | null> = { current: null };

    expect(() =>
      renderHook(() =>
        useStartPositionSeek({
          videoRef: ref,
          startPosition: 120,
          sourceKey: "s1",
        }),
      ),
    ).not.toThrow();
  });
});
