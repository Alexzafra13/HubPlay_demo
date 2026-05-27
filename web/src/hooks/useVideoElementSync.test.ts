import { describe, it, expect } from "vitest";
import { renderHook } from "@testing-library/react";
import { useVideoElementSync } from "./useVideoElementSync";
import type { RefObject } from "react";

function makeVideoRef(
  initial: Partial<HTMLVideoElement> = {},
): {
  ref: RefObject<HTMLVideoElement | null>;
  video: Partial<HTMLVideoElement>;
} {
  const video: Partial<HTMLVideoElement> = {
    volume: 1,
    muted: false,
    playbackRate: 1,
    ...initial,
  };
  return { ref: { current: video as HTMLVideoElement }, video };
}

describe("useVideoElementSync", () => {
  it("aplica volume y mute al elemento <video> al montar", () => {
    const { ref, video } = makeVideoRef();

    renderHook(() =>
      useVideoElementSync({
        videoRef: ref,
        volume: 0.4,
        isMuted: true,
        playbackRate: 1,
        sourceKey: "src-1",
      }),
    );

    expect(video.volume).toBe(0.4);
    expect(video.muted).toBe(true);
  });

  it("aplica playbackRate al elemento <video> al montar", () => {
    const { ref, video } = makeVideoRef();

    renderHook(() =>
      useVideoElementSync({
        videoRef: ref,
        volume: 1,
        isMuted: false,
        playbackRate: 1.5,
        sourceKey: "src-1",
      }),
    );

    expect(video.playbackRate).toBe(1.5);
  });

  it("rerender con nuevo volume actualiza video.volume", () => {
    const { ref, video } = makeVideoRef();

    const { rerender } = renderHook(
      ({ volume }: { volume: number }) =>
        useVideoElementSync({
          videoRef: ref,
          volume,
          isMuted: false,
          playbackRate: 1,
          sourceKey: "src-1",
        }),
      { initialProps: { volume: 0.5 } },
    );

    expect(video.volume).toBe(0.5);

    rerender({ volume: 0.2 });
    expect(video.volume).toBe(0.2);
  });

  it("rerender con nuevo isMuted actualiza video.muted", () => {
    const { ref, video } = makeVideoRef();

    const { rerender } = renderHook(
      ({ isMuted }: { isMuted: boolean }) =>
        useVideoElementSync({
          videoRef: ref,
          volume: 1,
          isMuted,
          playbackRate: 1,
          sourceKey: "src-1",
        }),
      { initialProps: { isMuted: false } },
    );

    expect(video.muted).toBe(false);

    rerender({ isMuted: true });
    expect(video.muted).toBe(true);
  });

  it("cambio de sourceKey re-aplica playbackRate (caso del remount)", () => {
    // Simula que un cambio de URL "reseteó" video.playbackRate a 1×
    // entre rerenders — el hook debe restaurar la preferencia.
    const { ref, video } = makeVideoRef();

    const { rerender } = renderHook(
      ({ sourceKey }: { sourceKey: string }) =>
        useVideoElementSync({
          videoRef: ref,
          volume: 1,
          isMuted: false,
          playbackRate: 1.5,
          sourceKey,
        }),
      { initialProps: { sourceKey: "src-1" } },
    );

    expect(video.playbackRate).toBe(1.5);

    video.playbackRate = 1;
    rerender({ sourceKey: "src-2" });

    expect(video.playbackRate).toBe(1.5);
  });

  it("cambio sólo de volume NO re-aplica playbackRate (deps separadas)", () => {
    // Garantiza que los dos effects no comparten dependencias —
    // tocar volume no debería disparar la re-aplicación del rate.
    const { ref, video } = makeVideoRef();

    const { rerender } = renderHook(
      ({ volume }: { volume: number }) =>
        useVideoElementSync({
          videoRef: ref,
          volume,
          isMuted: false,
          playbackRate: 1.5,
          sourceKey: "src-1",
        }),
      { initialProps: { volume: 0.5 } },
    );

    expect(video.playbackRate).toBe(1.5);

    video.playbackRate = 999;
    rerender({ volume: 0.6 });

    expect(video.playbackRate).toBe(999);
    expect(video.volume).toBe(0.6);
  });

  it("no-op cuando videoRef.current es null", () => {
    const ref: RefObject<HTMLVideoElement | null> = { current: null };

    expect(() =>
      renderHook(() =>
        useVideoElementSync({
          videoRef: ref,
          volume: 0.5,
          isMuted: true,
          playbackRate: 2,
          sourceKey: "src-1",
        }),
      ),
    ).not.toThrow();
  });
});
