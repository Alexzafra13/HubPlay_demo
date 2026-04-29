import {
  describe,
  it,
  expect,
  vi,
  beforeEach,
  afterEach,
} from "vitest";
import { renderHook, act } from "@testing-library/react";
import { type RefObject } from "react";
import { useLiveHls } from "./useLiveHls";

// Minimal hls.js stand-in: records what the hook called and lets the
// test fire MANIFEST_PARSED / ERROR back. Replaces the real Hls because
// jsdom has no MediaSource + the real library wires native APIs we
// don't need to exercise here — we only care about the contract the
// hook drives against the library, not the library itself.
//
// vi.hoisted runs before the vi.mock factory is hoisted, so the class
// definition is reachable when the mocked module is constructed.
type Listener = (event: string, data: Record<string, unknown>) => void;

const { FakeHls } = vi.hoisted(() => {
  class FakeHls {
    static instances: FakeHls[] = [];
    static supported = true;
    static isSupported() {
      return FakeHls.supported;
    }
    static Events = {
      MANIFEST_PARSED: "hlsManifestParsed",
      ERROR: "hlsError",
    };
    static ErrorTypes = {
      NETWORK_ERROR: "networkError",
      MEDIA_ERROR: "mediaError",
      OTHER_ERROR: "otherError",
    };
    static ErrorDetails = {
      MANIFEST_LOAD_ERROR: "manifestLoadError",
      MANIFEST_LOAD_TIMEOUT: "manifestLoadTimeout",
      MANIFEST_PARSING_ERROR: "manifestParsingError",
    };

    loadedSource: string | null = null;
    attached: HTMLVideoElement | null = null;
    destroyed = false;
    stopLoadCalls = 0;
    startLoadCalls: number[] = [];
    recoverMediaCalls = 0;
    listeners = new Map<string, Set<Listener>>();
    config: Record<string, unknown>;

    constructor(config: Record<string, unknown>) {
      this.config = config;
      FakeHls.instances.push(this);
    }
    loadSource(url: string) {
      this.loadedSource = url;
    }
    attachMedia(v: HTMLVideoElement) {
      this.attached = v;
    }
    on(event: string, handler: Listener) {
      if (!this.listeners.has(event)) this.listeners.set(event, new Set());
      this.listeners.get(event)!.add(handler);
    }
    destroy() {
      this.destroyed = true;
    }
    stopLoad() {
      this.stopLoadCalls += 1;
    }
    startLoad(n: number) {
      this.startLoadCalls.push(n);
    }
    recoverMediaError() {
      this.recoverMediaCalls += 1;
    }
    emit(event: string, data: Record<string, unknown>) {
      this.listeners.get(event)?.forEach((h) => h(event, data));
    }
  }
  return { FakeHls };
});

vi.mock("hls.js", () => ({ default: FakeHls }));

// jsdom HTMLVideoElement lacks the few methods + properties the hook
// touches. Patch them on a per-test basis so each test starts from a
// clean slate.
function makeVideo(): HTMLVideoElement {
  const v = document.createElement("video");
  // play() returns a promise in real browsers; jsdom returns undefined,
  // and the hook does .catch() on the result. Provide a resolved one.
  Object.defineProperty(v, "play", {
    value: vi.fn().mockResolvedValue(undefined),
    configurable: true,
  });
  Object.defineProperty(v, "load", { value: vi.fn(), configurable: true });
  Object.defineProperty(v, "canPlayType", {
    value: vi.fn().mockReturnValue(""),
    configurable: true,
  });
  return v;
}

// Helper to mount the hook with a video element pre-wired into a ref
// before the effect runs. Returns the hook result + the video so tests
// can fire DOM events on it.
function mountHook(
  options: Parameters<typeof useLiveHls>[0],
  video?: HTMLVideoElement,
) {
  const v = video ?? makeVideo();
  const ref = { current: v } as RefObject<HTMLVideoElement | null>;
  const result = renderHook(() => useLiveHls({ ...options, videoRef: ref }));
  return { result, video: v, ref };
}

beforeEach(() => {
  FakeHls.instances = [];
  FakeHls.supported = true;
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  vi.clearAllMocks();
});

describe("useLiveHls", () => {
  it("does nothing while videoRef is null", () => {
    const ref = { current: null } as RefObject<HTMLVideoElement | null>;
    renderHook(() =>
      useLiveHls({
        videoRef: ref,
        streamUrl: "https://x/index.m3u8",
        unavailableMessage: "down",
      }),
    );
    expect(FakeHls.instances).toHaveLength(0);
  });

  it("does nothing when streamUrl is null", () => {
    const ref = { current: makeVideo() } as RefObject<HTMLVideoElement | null>;
    renderHook(() =>
      useLiveHls({
        videoRef: ref,
        streamUrl: null,
        unavailableMessage: "down",
      }),
    );
    expect(FakeHls.instances).toHaveLength(0);
  });

  it("creates an Hls instance and feeds it the stream URL", () => {
    const { video } = mountHook({
      videoRef: undefined as never,
      streamUrl: "https://x/live.m3u8",
      unavailableMessage: "down",
    });
    expect(FakeHls.instances).toHaveLength(1);
    expect(FakeHls.instances[0].loadedSource).toBe("https://x/live.m3u8");
    expect(FakeHls.instances[0].attached).toBe(video);
  });

  it("calls onFirstPlay exactly once on the first 'playing' event", () => {
    const onFirstPlay = vi.fn();
    const { video } = mountHook({
      videoRef: undefined as never,
      streamUrl: "https://x/a.m3u8",
      unavailableMessage: "down",
      onFirstPlay,
    });

    act(() => {
      video.dispatchEvent(new Event("playing"));
    });
    expect(onFirstPlay).toHaveBeenCalledTimes(1);

    // Pause + resume on the same attachment must not re-fire the beacon.
    act(() => {
      video.dispatchEvent(new Event("playing"));
    });
    expect(onFirstPlay).toHaveBeenCalledTimes(1);
  });

  it("flips loading=false once 'playing' fires", () => {
    const { result, video } = mountHook({
      videoRef: undefined as never,
      streamUrl: "https://x/a.m3u8",
      unavailableMessage: "down",
    });
    expect(result.result.current.loading).toBe(true);

    act(() => {
      video.dispatchEvent(new Event("playing"));
    });
    expect(result.result.current.loading).toBe(false);
  });

  it("surfaces the unavailable message + fires onFatalError('timeout') when no first frame in window", () => {
    const onFatalError = vi.fn();
    const { result } = mountHook({
      videoRef: undefined as never,
      streamUrl: "https://x/dead.m3u8",
      unavailableMessage: "no signal",
      timeoutMs: 5_000,
      onFatalError,
    });

    act(() => {
      vi.advanceTimersByTime(5_000);
    });
    expect(result.result.current.error).toBe("no signal");
    expect(onFatalError).toHaveBeenCalledWith("timeout", expect.stringContaining("5000"));
  });

  it("does NOT fire timeout when first frame arrives before the deadline", () => {
    const onFatalError = vi.fn();
    const { result, video } = mountHook({
      videoRef: undefined as never,
      streamUrl: "https://x/live.m3u8",
      unavailableMessage: "no signal",
      timeoutMs: 5_000,
      onFatalError,
    });
    act(() => {
      vi.advanceTimersByTime(2_000);
      video.dispatchEvent(new Event("playing"));
      vi.advanceTimersByTime(10_000);
    });
    expect(result.result.current.error).toBeNull();
    expect(onFatalError).not.toHaveBeenCalled();
  });

  it("recovers media errors via hls.js without surfacing them", () => {
    const onFatalError = vi.fn();
    mountHook({
      videoRef: undefined as never,
      streamUrl: "https://x/a.m3u8",
      unavailableMessage: "down",
      onFatalError,
    });
    const hls = FakeHls.instances[0];
    act(() => {
      hls.emit("hlsError", {
        fatal: true,
        type: "mediaError",
        details: "bufferStalledError",
      });
    });
    expect(hls.recoverMediaCalls).toBe(1);
    expect(onFatalError).not.toHaveBeenCalled();
  });

  it("retries the first 3 fatal network errors before giving up", () => {
    const onFatalError = vi.fn();
    mountHook({
      videoRef: undefined as never,
      streamUrl: "https://x/a.m3u8",
      unavailableMessage: "down",
      onFatalError,
    });
    const hls = FakeHls.instances[0];
    for (let i = 0; i < 3; i++) {
      act(() => {
        hls.emit("hlsError", {
          fatal: true,
          type: "networkError",
          details: "fragLoadError",
        });
      });
    }
    expect(hls.startLoadCalls.length).toBeGreaterThanOrEqual(3);
    expect(onFatalError).not.toHaveBeenCalled();

    // 4th fatal network error trips the fallback.
    act(() => {
      hls.emit("hlsError", {
        fatal: true,
        type: "networkError",
        details: "fragLoadError",
      });
    });
    expect(onFatalError).toHaveBeenCalledWith("network", expect.any(String));
    expect(hls.destroyed).toBe(true);
  });

  it("classifies manifest-load errors as kind=manifest", () => {
    const onFatalError = vi.fn();
    mountHook({
      videoRef: undefined as never,
      streamUrl: "https://x/a.m3u8",
      unavailableMessage: "down",
      onFatalError,
    });
    const hls = FakeHls.instances[0];
    // Burn through the 3 retries with frag-level errors so the next
    // manifest error is the one that surfaces.
    for (let i = 0; i < 3; i++) {
      act(() => {
        hls.emit("hlsError", {
          fatal: true,
          type: "networkError",
          details: "fragLoadError",
        });
      });
    }
    act(() => {
      hls.emit("hlsError", {
        fatal: true,
        type: "networkError",
        details: "manifestLoadError",
      });
    });
    expect(onFatalError).toHaveBeenCalledWith("manifest", expect.any(String));
  });

  it("only fires onFatalError once per stream URL", () => {
    const onFatalError = vi.fn();
    mountHook({
      videoRef: undefined as never,
      streamUrl: "https://x/a.m3u8",
      unavailableMessage: "down",
      timeoutMs: 1_000,
      onFatalError,
    });
    act(() => {
      vi.advanceTimersByTime(1_000);
    });
    expect(onFatalError).toHaveBeenCalledTimes(1);

    // A subsequent fatal hls.js error on the same attachment must not
    // re-trigger the beacon — repeated fires would defeat the dead-
    // stream signal the backend uses for channel-health.
    const hls = FakeHls.instances[0];
    for (let i = 0; i < 5; i++) {
      act(() => {
        hls.emit("hlsError", {
          fatal: true,
          type: "networkError",
          details: "fragLoadError",
        });
      });
    }
    expect(onFatalError).toHaveBeenCalledTimes(1);
  });

  it("destroys the previous Hls instance and spawns a new one on streamUrl change", () => {
    const ref = { current: makeVideo() } as RefObject<HTMLVideoElement | null>;
    const { rerender } = renderHook(
      ({ url }: { url: string | null }) =>
        useLiveHls({
          videoRef: ref,
          streamUrl: url,
          unavailableMessage: "down",
        }),
      { initialProps: { url: "https://x/a.m3u8" } },
    );
    const first = FakeHls.instances[0];
    expect(first.destroyed).toBe(false);

    rerender({ url: "https://x/b.m3u8" });
    expect(first.destroyed).toBe(true);
    expect(FakeHls.instances).toHaveLength(2);
    expect(FakeHls.instances[1].loadedSource).toBe("https://x/b.m3u8");
  });

  it("destroys Hls + detaches visibilitychange on unmount", () => {
    const removeSpy = vi.spyOn(document, "removeEventListener");
    const { result, video } = mountHook({
      videoRef: undefined as never,
      streamUrl: "https://x/a.m3u8",
      unavailableMessage: "down",
    });
    void video; // silence unused
    const hls = FakeHls.instances[0];
    act(() => result.unmount());
    expect(hls.destroyed).toBe(true);
    expect(removeSpy).toHaveBeenCalledWith(
      "visibilitychange",
      expect.any(Function),
    );
  });

  it("stops + resumes load when the document visibility flips", () => {
    mountHook({
      videoRef: undefined as never,
      streamUrl: "https://x/live.m3u8",
      unavailableMessage: "down",
    });
    const hls = FakeHls.instances[0];

    Object.defineProperty(document, "hidden", {
      configurable: true,
      get: () => true,
    });
    act(() => {
      document.dispatchEvent(new Event("visibilitychange"));
    });
    expect(hls.stopLoadCalls).toBe(1);

    Object.defineProperty(document, "hidden", {
      configurable: true,
      get: () => false,
    });
    act(() => {
      document.dispatchEvent(new Event("visibilitychange"));
    });
    // -1 = resume from live edge, the fix for background-tab stalls.
    expect(hls.startLoadCalls).toContain(-1);
  });

  it("reload() forces a re-attach", () => {
    const { result } = mountHook({
      videoRef: undefined as never,
      streamUrl: "https://x/a.m3u8",
      unavailableMessage: "down",
    });
    const first = FakeHls.instances[0];

    act(() => result.result.current.reload());
    expect(first.destroyed).toBe(true);
    expect(FakeHls.instances).toHaveLength(2);
  });

  // The hook stores the latest onFirstPlay in a ref so a parent
  // re-rendering with a fresh closure does not tear down the player.
  it("re-rendering with a new onFirstPlay closure keeps the same Hls instance", () => {
    const ref = { current: makeVideo() } as RefObject<HTMLVideoElement | null>;
    const { rerender } = renderHook(
      ({ cb }: { cb: () => void }) =>
        useLiveHls({
          videoRef: ref,
          streamUrl: "https://x/a.m3u8",
          unavailableMessage: "down",
          onFirstPlay: cb,
        }),
      { initialProps: { cb: vi.fn() } },
    );
    const first = FakeHls.instances[0];
    rerender({ cb: vi.fn() });
    expect(first.destroyed).toBe(false);
    expect(FakeHls.instances).toHaveLength(1);
  });
});
