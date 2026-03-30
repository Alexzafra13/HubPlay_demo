import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook } from "@testing-library/react";
import { usePlayerKeyboard } from "./usePlayerKeyboard";
import type { RefObject } from "react";

function fireKey(key: string) {
  window.dispatchEvent(new KeyboardEvent("keydown", { key, bubbles: true }));
}

describe("usePlayerKeyboard", () => {
  let mockVideo: Partial<HTMLVideoElement>;
  let videoRef: RefObject<HTMLVideoElement | null>;
  let handlers: {
    onTogglePlay: () => void;
    onToggleFullscreen: () => void;
    onToggleMute: () => void;
    onVolumeChange: (v: number) => void;
    onClose: () => void;
    onActivity: () => void;
  };

  beforeEach(() => {
    mockVideo = {
      currentTime: 50,
      duration: 100,
      volume: 0.8,
    };
    videoRef = { current: mockVideo as HTMLVideoElement };
    handlers = {
      onTogglePlay: vi.fn(),
      onToggleFullscreen: vi.fn(),
      onToggleMute: vi.fn(),
      onVolumeChange: vi.fn(),
      onClose: vi.fn(),
      onActivity: vi.fn(),
    };
  });

  it("space toggles play/pause", () => {
    renderHook(() => usePlayerKeyboard({ videoRef, ...handlers }));

    fireKey(" ");

    expect(handlers.onTogglePlay).toHaveBeenCalledOnce();
  });

  it("f toggles fullscreen", () => {
    renderHook(() => usePlayerKeyboard({ videoRef, ...handlers }));

    fireKey("f");

    expect(handlers.onToggleFullscreen).toHaveBeenCalledOnce();
  });

  it("m toggles mute", () => {
    renderHook(() => usePlayerKeyboard({ videoRef, ...handlers }));

    fireKey("m");

    expect(handlers.onToggleMute).toHaveBeenCalledOnce();
  });

  it("ArrowLeft seeks back 10 seconds", () => {
    renderHook(() => usePlayerKeyboard({ videoRef, ...handlers }));

    fireKey("ArrowLeft");

    expect(mockVideo.currentTime).toBe(40);
    expect(handlers.onActivity).toHaveBeenCalledOnce();
  });

  it("ArrowRight seeks forward 10 seconds", () => {
    renderHook(() => usePlayerKeyboard({ videoRef, ...handlers }));

    fireKey("ArrowRight");

    expect(mockVideo.currentTime).toBe(60);
    expect(handlers.onActivity).toHaveBeenCalledOnce();
  });

  it("ArrowLeft does not go below 0", () => {
    mockVideo.currentTime = 3;
    renderHook(() => usePlayerKeyboard({ videoRef, ...handlers }));

    fireKey("ArrowLeft");

    expect(mockVideo.currentTime).toBe(0);
  });

  it("ArrowRight does not exceed duration", () => {
    mockVideo.currentTime = 95;
    renderHook(() => usePlayerKeyboard({ videoRef, ...handlers }));

    fireKey("ArrowRight");

    expect(mockVideo.currentTime).toBe(100);
  });

  it("ArrowUp increases volume", () => {
    renderHook(() => usePlayerKeyboard({ videoRef, ...handlers }));

    fireKey("ArrowUp");

    expect(vi.mocked(handlers.onVolumeChange).mock.calls[0][0]).toBeCloseTo(0.85);
  });

  it("ArrowDown decreases volume", () => {
    renderHook(() => usePlayerKeyboard({ videoRef, ...handlers }));

    fireKey("ArrowDown");

    expect(vi.mocked(handlers.onVolumeChange).mock.calls[0][0]).toBeCloseTo(0.75);
  });

  it("Escape calls onClose when not fullscreen", () => {
    renderHook(() => usePlayerKeyboard({ videoRef, ...handlers }));

    fireKey("Escape");

    expect(handlers.onClose).toHaveBeenCalledOnce();
  });

  it("ignores keys when typing in an input", () => {
    renderHook(() => usePlayerKeyboard({ videoRef, ...handlers }));

    const input = document.createElement("input");
    document.body.appendChild(input);
    input.focus();

    const event = new KeyboardEvent("keydown", { key: " ", bubbles: true });
    Object.defineProperty(event, "target", { value: input });
    window.dispatchEvent(event);

    expect(handlers.onTogglePlay).not.toHaveBeenCalled();
    document.body.removeChild(input);
  });

  it("does nothing when videoRef is null", () => {
    const nullRef: RefObject<HTMLVideoElement | null> = { current: null };
    renderHook(() =>
      usePlayerKeyboard({ videoRef: nullRef, ...handlers }),
    );

    fireKey(" ");

    expect(handlers.onTogglePlay).not.toHaveBeenCalled();
  });

  it("cleans up event listener on unmount", () => {
    const { unmount } = renderHook(() =>
      usePlayerKeyboard({ videoRef, ...handlers }),
    );

    unmount();

    fireKey(" ");

    expect(handlers.onTogglePlay).not.toHaveBeenCalled();
  });
});
