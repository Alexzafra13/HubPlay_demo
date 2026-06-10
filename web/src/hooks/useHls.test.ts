import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, act } from "@testing-library/react";
import "@/i18n";
import { useHls } from "./useHls";

// Stand-in mínimo de hls.js, mismo patrón que useLiveHls.test.ts:
// jsdom no tiene MediaSource y aquí solo nos interesa el contrato que
// el hook ejerce contra la librería, no la librería en sí.
type Listener = (event: string, data: Record<string, unknown>) => void;

const { FakeHls } = vi.hoisted(() => {
  class FakeHls {
    static instances: FakeHls[] = [];
    static supported = true;
    static isSupported() {
      return FakeHls.supported;
    }
    static Events = {
      MEDIA_ATTACHED: "hlsMediaAttached",
      MANIFEST_PARSED: "hlsManifestParsed",
      LEVEL_SWITCHED: "hlsLevelSwitched",
      SUBTITLE_TRACKS_UPDATED: "hlsSubtitleTracksUpdated",
      AUDIO_TRACK_SWITCHED: "hlsAudioTrackSwitched",
      SUBTITLE_TRACK_SWITCH: "hlsSubtitleTrackSwitch",
      ERROR: "hlsError",
      FRAG_LOADED: "hlsFragLoaded",
    };
    static ErrorTypes = {
      NETWORK_ERROR: "networkError",
      MEDIA_ERROR: "mediaError",
      OTHER_ERROR: "otherError",
    };

    loadedSource: string | null = null;
    attached: HTMLVideoElement | null = null;
    destroyed = false;
    startLoadCalls: number[] = [];
    recoverMediaCalls = 0;
    listeners = new Map<string, Set<Listener>>();
    config: Record<string, unknown>;

    audioTracks: unknown[] = [];
    subtitleTracks: unknown[] = [];
    levels: unknown[] = [];
    audioTrack = 0;
    subtitleTrack = -1;
    currentLevel = -1;
    autoLevelEnabled = true;

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
    startLoad(n: number) {
      this.startLoadCalls.push(n);
    }
    recoverMediaError() {
      this.recoverMediaCalls += 1;
    }
    swapAudioCodecCalls = 0;
    swapAudioCodec() {
      this.swapAudioCodecCalls += 1;
    }
    emit(event: string, data: Record<string, unknown>) {
      this.listeners.get(event)?.forEach((h) => h(event, data));
    }
  }
  return { FakeHls };
});

vi.mock("hls.js", () => ({ default: FakeHls }));

function makeVideo(): HTMLVideoElement {
  const v = document.createElement("video");
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

// jsdom no implementa HTMLMediaElement.error; lo inyectamos por test.
function setMediaError(v: HTMLVideoElement, code: number) {
  Object.defineProperty(v, "error", {
    value: { code },
    configurable: true,
  });
}

function mount(opts: {
  playbackMethod: string;
  masterPlaylistUrl?: string | null;
  directUrl?: string | null;
}) {
  const video = makeVideo();
  const videoRef = { current: video };
  const hook = renderHook(() =>
    useHls({
      videoRef,
      masterPlaylistUrl: opts.masterPlaylistUrl ?? null,
      directUrl: opts.directUrl ?? null,
      playbackMethod: opts.playbackMethod,
      sessionToken: "",
    }),
  );
  return { hook, video };
}

beforeEach(() => {
  FakeHls.instances = [];
  FakeHls.supported = true;
});

// PB-4 (audit 2026-06-10): las rutas de src directo (direct_play y HLS
// nativo de Safari/iOS) no tenían listener de `error` del <video> — un
// fallo de decode a mitad de reproducción dejaba el overlay de carga
// girando para siempre, sin mensaje.
describe("useHls — <video> error listener (PB-4)", () => {
  it("direct_play: decode error (code 3) surfaces an error message", () => {
    const { hook, video } = mount({
      playbackMethod: "direct_play",
      directUrl: "/api/v1/items/1/stream/direct",
    });
    expect(hook.result.current.error).toBeNull();

    setMediaError(video, 3);
    act(() => {
      video.dispatchEvent(new Event("error"));
    });

    expect(hook.result.current.error).toMatch(/decodificar|decoded/i);
  });

  it("direct_play: src-not-supported (code 4) surfaces a format message", () => {
    const { hook, video } = mount({
      playbackMethod: "direct_play",
      directUrl: "/api/v1/items/1/stream/direct",
    });

    setMediaError(video, 4);
    act(() => {
      video.dispatchEvent(new Event("error"));
    });

    expect(hook.result.current.error).toMatch(/no está soportado|not supported/i);
  });

  it("direct_play: abort (code 1) is ignored — teardown/zapping is not a failure", () => {
    const { hook, video } = mount({
      playbackMethod: "direct_play",
      directUrl: "/api/v1/items/1/stream/direct",
    });

    setMediaError(video, 1);
    act(() => {
      video.dispatchEvent(new Event("error"));
    });

    expect(hook.result.current.error).toBeNull();
  });

  it("native HLS path (Safari/iOS, sin MSE) also reports video errors", () => {
    FakeHls.supported = false;
    const { hook, video } = mount({
      playbackMethod: "transcode",
      masterPlaylistUrl: "/api/v1/items/1/stream/hls/master.m3u8",
    });
    Object.defineProperty(video, "canPlayType", {
      value: vi.fn().mockReturnValue("maybe"),
      configurable: true,
    });

    setMediaError(video, 3);
    act(() => {
      video.dispatchEvent(new Event("error"));
    });

    expect(hook.result.current.error).toMatch(/decodificar|decoded/i);
  });

  it("while hls.js drives playback the raw <video> error is left to hls.js", () => {
    const { hook, video } = mount({
      playbackMethod: "transcode",
      masterPlaylistUrl: "/api/v1/items/1/stream/hls/master.m3u8",
    });
    expect(FakeHls.instances).toHaveLength(1);

    setMediaError(video, 3);
    act(() => {
      video.dispatchEvent(new Event("error"));
    });

    // hls.js tiene su propio recovery vía Hls.Events.ERROR; el listener
    // crudo no debe pisarlo con un error terminal.
    expect(hook.result.current.error).toBeNull();
  });
});

describe("useHls — hls.js fatal error recovery (contrato existente)", () => {
  it("fatal network error sets a recovering message and calls startLoad", () => {
    const { hook } = mount({
      playbackMethod: "transcode",
      masterPlaylistUrl: "/api/v1/items/1/stream/hls/master.m3u8",
    });
    const hls = FakeHls.instances[0];

    act(() => {
      hls.emit(FakeHls.Events.ERROR, {
        fatal: true,
        type: FakeHls.ErrorTypes.NETWORK_ERROR,
        details: "fragLoadError",
      });
    });

    expect(hook.result.current.error).toMatch(/reintentando|retrying/i);
    expect(hls.startLoadCalls).toHaveLength(1);
  });

  it("PB-16: el cuarto network error consecutivo es terminal — destroy y sin más retries", () => {
    const { hook } = mount({
      playbackMethod: "transcode",
      masterPlaylistUrl: "/api/v1/items/1/stream/hls/master.m3u8",
    });
    const hls = FakeHls.instances[0];

    for (let i = 0; i < 4; i++) {
      act(() => {
        hls.emit(FakeHls.Events.ERROR, {
          fatal: true,
          type: FakeHls.ErrorTypes.NETWORK_ERROR,
          details: "fragLoadError",
        });
      });
    }

    // 3 recoveries permitidos; el 4º no reintenta y mata la instancia.
    expect(hls.startLoadCalls).toHaveLength(3);
    expect(hls.destroyed).toBe(true);
    expect(hook.result.current.error).toMatch(/no se pudo recuperar|could not reconnect/i);
  });

  it("PB-16: un FRAG_LOADED sano resetea el presupuesto de recovery", () => {
    const { hook } = mount({
      playbackMethod: "transcode",
      masterPlaylistUrl: "/api/v1/items/1/stream/hls/master.m3u8",
    });
    const hls = FakeHls.instances[0];
    const netError = () =>
      act(() => {
        hls.emit(FakeHls.Events.ERROR, {
          fatal: true,
          type: FakeHls.ErrorTypes.NETWORK_ERROR,
          details: "fragLoadError",
        });
      });

    netError();
    netError();
    netError(); // 3 — presupuesto agotado
    act(() => {
      hls.emit(FakeHls.Events.FRAG_LOADED, {});
    });
    expect(hook.result.current.error).toBeNull();

    netError(); // tras el reset vuelve a reintentar, no es terminal
    expect(hls.destroyed).toBe(false);
    expect(hls.startLoadCalls).toHaveLength(4);
  });

  it("PB-16: el segundo media error en <3s intenta swapAudioCodec (patrón hls.js)", () => {
    const { hook } = mount({
      playbackMethod: "transcode",
      masterPlaylistUrl: "/api/v1/items/1/stream/hls/master.m3u8",
    });
    const hls = FakeHls.instances[0];
    const mediaError = () =>
      act(() => {
        hls.emit(FakeHls.Events.ERROR, {
          fatal: true,
          type: FakeHls.ErrorTypes.MEDIA_ERROR,
          details: "bufferAppendError",
        });
      });

    mediaError();
    expect(hls.swapAudioCodecCalls).toBe(0);
    mediaError(); // inmediato → dentro de la ventana de 3s
    expect(hls.swapAudioCodecCalls).toBe(1);
    expect(hls.recoverMediaCalls).toBe(2);

    mediaError(); // 3º — último permitido
    mediaError(); // 4º — terminal
    expect(hls.destroyed).toBe(true);
    expect(hook.result.current.error).toMatch(/no pudo decodificar|could not decode/i);
  });

  it("fatal media error calls recoverMediaError", () => {
    const { hook } = mount({
      playbackMethod: "transcode",
      masterPlaylistUrl: "/api/v1/items/1/stream/hls/master.m3u8",
    });
    const hls = FakeHls.instances[0];

    act(() => {
      hls.emit(FakeHls.Events.ERROR, {
        fatal: true,
        type: FakeHls.ErrorTypes.MEDIA_ERROR,
        details: "bufferAppendError",
      });
    });

    expect(hook.result.current.error).toMatch(/recuperando|recovering/i);
    expect(hls.recoverMediaCalls).toBe(1);
  });
});
