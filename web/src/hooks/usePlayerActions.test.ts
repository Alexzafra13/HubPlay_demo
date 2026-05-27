import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { usePlayerActions } from "./usePlayerActions";
import type { RefObject } from "react";

interface FakeVideo {
  paused: boolean;
  currentTime: number;
  disablePictureInPicture: boolean;
  play: ReturnType<typeof vi.fn>;
  pause: ReturnType<typeof vi.fn>;
  requestPictureInPicture: ReturnType<typeof vi.fn>;
}

interface FakeContainer {
  requestFullscreen: ReturnType<typeof vi.fn>;
}

function makeRefs(videoInit: Partial<FakeVideo> = {}): {
  videoRef: RefObject<HTMLVideoElement | null>;
  containerRef: RefObject<HTMLDivElement | null>;
  video: FakeVideo;
  container: FakeContainer;
} {
  const video: FakeVideo = {
    paused: true,
    currentTime: 0,
    disablePictureInPicture: false,
    play: vi.fn().mockResolvedValue(undefined),
    pause: vi.fn(),
    requestPictureInPicture: vi.fn().mockResolvedValue(undefined),
    ...videoInit,
  };
  const container: FakeContainer = {
    requestFullscreen: vi.fn().mockResolvedValue(undefined),
  };
  return {
    videoRef: { current: video as unknown as HTMLVideoElement },
    containerRef: { current: container as unknown as HTMLDivElement },
    video,
    container,
  };
}

function makeOptions(overrides: Partial<Parameters<typeof usePlayerActions>[0]> = {}) {
  const refs = makeRefs();
  return {
    refs,
    base: {
      videoRef: refs.videoRef,
      containerRef: refs.containerRef,
      isMobile: false,
      controlsVisible: false,
      showControls: vi.fn(),
      hideControls: vi.fn(),
      isMuted: false,
      setVolume: vi.fn(),
      toggleMute: vi.fn(),
      onClose: vi.fn(),
      ...overrides,
    },
  };
}

beforeEach(() => {
  Object.defineProperty(document, "fullscreenElement", {
    value: null,
    writable: true,
    configurable: true,
  });
  Object.defineProperty(document, "pictureInPictureElement", {
    value: null,
    writable: true,
    configurable: true,
  });
  Object.defineProperty(document, "pictureInPictureEnabled", {
    value: true,
    writable: true,
    configurable: true,
  });
  document.exitFullscreen = vi.fn().mockResolvedValue(undefined);
  document.exitPictureInPicture = vi.fn().mockResolvedValue(undefined);
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("usePlayerActions", () => {
  describe("togglePlayPause", () => {
    it("llama play() cuando está paused", () => {
      const { refs, base } = makeOptions();
      const { result } = renderHook(() => usePlayerActions(base));

      act(() => result.current.togglePlayPause());

      expect(refs.video.play).toHaveBeenCalledOnce();
      expect(refs.video.pause).not.toHaveBeenCalled();
    });

    it("llama pause() cuando está playing", () => {
      const { refs, base } = makeOptions();
      refs.video.paused = false;
      const { result } = renderHook(() => usePlayerActions(base));

      act(() => result.current.togglePlayPause());

      expect(refs.video.pause).toHaveBeenCalledOnce();
      expect(refs.video.play).not.toHaveBeenCalled();
    });

    it("no-op cuando videoRef.current es null", () => {
      const { base } = makeOptions();
      base.videoRef = { current: null };
      const { result } = renderHook(() => usePlayerActions(base));

      expect(() => act(() => result.current.togglePlayPause())).not.toThrow();
    });

    it("swallow del rechazo de play() (autoplay-policy)", async () => {
      const { refs, base } = makeOptions();
      refs.video.play.mockRejectedValueOnce(new Error("blocked"));
      const { result } = renderHook(() => usePlayerActions(base));

      act(() => result.current.togglePlayPause());
      await Promise.resolve();

      expect(refs.video.play).toHaveBeenCalledOnce();
    });
  });

  describe("handleSurfaceTap", () => {
    it("en desktop cae a togglePlayPause", () => {
      const { refs, base } = makeOptions({ isMobile: false });
      const { result } = renderHook(() => usePlayerActions(base));

      act(() => result.current.handleSurfaceTap());

      expect(refs.video.play).toHaveBeenCalledOnce();
    });

    it("en mobile con controles ocultos muestra controles (sin pausa)", () => {
      const { refs, base } = makeOptions({
        isMobile: true,
        controlsVisible: false,
      });
      const { result } = renderHook(() => usePlayerActions(base));

      act(() => result.current.handleSurfaceTap());

      expect(base.showControls).toHaveBeenCalledOnce();
      expect(base.hideControls).not.toHaveBeenCalled();
      expect(refs.video.play).not.toHaveBeenCalled();
    });

    it("en mobile con controles visibles los oculta", () => {
      const { base } = makeOptions({
        isMobile: true,
        controlsVisible: true,
      });
      const { result } = renderHook(() => usePlayerActions(base));

      act(() => result.current.handleSurfaceTap());

      expect(base.hideControls).toHaveBeenCalledOnce();
      expect(base.showControls).not.toHaveBeenCalled();
    });
  });

  describe("handleSeek", () => {
    it("escribe currentTime en el <video>", () => {
      const { refs, base } = makeOptions();
      const { result } = renderHook(() => usePlayerActions(base));

      act(() => result.current.handleSeek(42));

      expect(refs.video.currentTime).toBe(42);
    });

    it("no-op cuando videoRef.current es null", () => {
      const { base } = makeOptions();
      base.videoRef = { current: null };
      const { result } = renderHook(() => usePlayerActions(base));

      expect(() => act(() => result.current.handleSeek(42))).not.toThrow();
    });
  });

  describe("handleVolumeChange", () => {
    it("hace clamp del volumen al rango [0, 1] (superior)", () => {
      const { base } = makeOptions();
      const { result } = renderHook(() => usePlayerActions(base));

      act(() => result.current.handleVolumeChange(1.5));

      expect(base.setVolume).toHaveBeenCalledWith(1);
    });

    it("hace clamp del volumen al rango [0, 1] (inferior)", () => {
      const { base } = makeOptions();
      const { result } = renderHook(() => usePlayerActions(base));

      act(() => result.current.handleVolumeChange(-0.3));

      expect(base.setVolume).toHaveBeenCalledWith(0);
    });

    it("subir el volumen desde mute auto-desmutea", () => {
      const { base } = makeOptions({ isMuted: true });
      const { result } = renderHook(() => usePlayerActions(base));

      act(() => result.current.handleVolumeChange(0.5));

      expect(base.setVolume).toHaveBeenCalledWith(0.5);
      expect(base.toggleMute).toHaveBeenCalledOnce();
    });

    it("ajustar el volumen a 0 NO desmutea (el usuario quiere mute)", () => {
      const { base } = makeOptions({ isMuted: true });
      const { result } = renderHook(() => usePlayerActions(base));

      act(() => result.current.handleVolumeChange(0));

      expect(base.setVolume).toHaveBeenCalledWith(0);
      expect(base.toggleMute).not.toHaveBeenCalled();
    });
  });

  describe("handleToggleFullscreen", () => {
    it("entra en fullscreen cuando no hay element fullscreen", () => {
      const { refs, base } = makeOptions();
      const { result } = renderHook(() => usePlayerActions(base));

      act(() => result.current.handleToggleFullscreen());

      expect(refs.container.requestFullscreen).toHaveBeenCalledOnce();
      expect(document.exitFullscreen).not.toHaveBeenCalled();
    });

    it("sale de fullscreen cuando hay element fullscreen", () => {
      const { refs, base } = makeOptions();
      Object.defineProperty(document, "fullscreenElement", {
        value: document.documentElement,
        writable: true,
        configurable: true,
      });
      const { result } = renderHook(() => usePlayerActions(base));

      act(() => result.current.handleToggleFullscreen());

      expect(document.exitFullscreen).toHaveBeenCalledOnce();
      expect(refs.container.requestFullscreen).not.toHaveBeenCalled();
    });

    it("no-op cuando containerRef.current es null", () => {
      const { base } = makeOptions();
      base.containerRef = { current: null };
      const { result } = renderHook(() => usePlayerActions(base));

      expect(() => act(() => result.current.handleToggleFullscreen())).not.toThrow();
    });
  });

  describe("handleClose", () => {
    it("llama onClose directamente cuando no estamos en fullscreen", () => {
      const { base } = makeOptions();
      const { result } = renderHook(() => usePlayerActions(base));

      act(() => result.current.handleClose());

      expect(base.onClose).toHaveBeenCalledOnce();
      expect(document.exitFullscreen).not.toHaveBeenCalled();
    });

    it("sale de fullscreen primero y luego onClose cuando estamos en fullscreen", async () => {
      const { base } = makeOptions();
      Object.defineProperty(document, "fullscreenElement", {
        value: document.documentElement,
        writable: true,
        configurable: true,
      });
      const { result } = renderHook(() => usePlayerActions(base));

      act(() => result.current.handleClose());
      await Promise.resolve();

      expect(document.exitFullscreen).toHaveBeenCalledOnce();
      expect(base.onClose).toHaveBeenCalledOnce();
    });

    it("llama onClose igualmente si exitFullscreen falla", async () => {
      const { base } = makeOptions();
      Object.defineProperty(document, "fullscreenElement", {
        value: document.documentElement,
        writable: true,
        configurable: true,
      });
      vi.mocked(document.exitFullscreen).mockRejectedValueOnce(new Error("fail"));
      const { result } = renderHook(() => usePlayerActions(base));

      act(() => result.current.handleClose());
      await Promise.resolve();
      await Promise.resolve();

      expect(base.onClose).toHaveBeenCalledOnce();
    });
  });

  describe("handleTogglePiP", () => {
    it("entra en PiP cuando no hay element PiP", async () => {
      const { refs, base } = makeOptions();
      const { result } = renderHook(() => usePlayerActions(base));

      await act(async () => {
        await result.current.handleTogglePiP();
      });

      expect(refs.video.requestPictureInPicture).toHaveBeenCalledOnce();
      expect(document.exitPictureInPicture).not.toHaveBeenCalled();
    });

    it("sale de PiP cuando hay element PiP", async () => {
      const { refs, base } = makeOptions();
      Object.defineProperty(document, "pictureInPictureElement", {
        value: refs.video,
        writable: true,
        configurable: true,
      });
      const { result } = renderHook(() => usePlayerActions(base));

      await act(async () => {
        await result.current.handleTogglePiP();
      });

      expect(document.exitPictureInPicture).toHaveBeenCalledOnce();
      expect(refs.video.requestPictureInPicture).not.toHaveBeenCalled();
    });

    it("no-op cuando el browser no soporta PiP", async () => {
      const { refs, base } = makeOptions();
      Object.defineProperty(document, "pictureInPictureEnabled", {
        value: false,
        writable: true,
        configurable: true,
      });
      const { result } = renderHook(() => usePlayerActions(base));

      await act(async () => {
        await result.current.handleTogglePiP();
      });

      expect(refs.video.requestPictureInPicture).not.toHaveBeenCalled();
    });

    it("no-op cuando disablePictureInPicture es true en el <video>", async () => {
      const { refs, base } = makeOptions({});
      refs.video.disablePictureInPicture = true;
      const { result } = renderHook(() => usePlayerActions(base));

      await act(async () => {
        await result.current.handleTogglePiP();
      });

      expect(refs.video.requestPictureInPicture).not.toHaveBeenCalled();
    });

    it("swallow del rechazo (autoplay-policy / sin gesture)", async () => {
      const { refs, base } = makeOptions();
      refs.video.requestPictureInPicture.mockRejectedValueOnce(
        new Error("gesture missing"),
      );
      const { result } = renderHook(() => usePlayerActions(base));

      await expect(
        act(async () => {
          await result.current.handleTogglePiP();
        }),
      ).resolves.not.toThrow();
    });

    it("no-op cuando videoRef.current es null", async () => {
      const { base } = makeOptions();
      base.videoRef = { current: null };
      const { result } = renderHook(() => usePlayerActions(base));

      await expect(
        act(async () => {
          await result.current.handleTogglePiP();
        }),
      ).resolves.not.toThrow();
    });
  });

  describe("handleToggleMute", () => {
    it("invoca toggleMute del store", () => {
      const { base } = makeOptions();
      const { result } = renderHook(() => usePlayerActions(base));

      act(() => result.current.handleToggleMute());

      expect(base.toggleMute).toHaveBeenCalledOnce();
    });
  });
});
