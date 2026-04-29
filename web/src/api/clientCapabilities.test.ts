import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import {
  getClientCapabilitiesHeader,
  resetClientCapabilitiesCacheForTests,
} from "./clientCapabilities";

// jsdom doesn't ship MediaSource. We attach a fake on `window` for the
// duration of each test and tear it down afterwards. The function is
// memoised — every test resets the cache via the exported helper.

declare global {
  interface Window {
    MediaSource?: { isTypeSupported: (mime: string) => boolean };
  }
}

describe("getClientCapabilitiesHeader", () => {
  beforeEach(() => {
    resetClientCapabilitiesCacheForTests();
  });

  afterEach(() => {
    delete (window as Window).MediaSource;
    resetClientCapabilitiesCacheForTests();
  });

  it("returns null when MediaSource is unavailable (SSR or pre-MSE browser)", () => {
    expect((window as Window).MediaSource).toBeUndefined();
    expect(getClientCapabilitiesHeader()).toBeNull();
  });

  it("returns null when MediaSource.isTypeSupported throws", () => {
    (window as Window).MediaSource = {
      isTypeSupported: () => {
        throw new Error("noisy environment");
      },
    };
    // Per-MIME try/catch returns false on throw; if every MIME throws,
    // both probe arrays come back empty and we fall through to the
    // null branch (no point sending an empty header).
    expect(getClientCapabilitiesHeader()).toBeNull();
  });

  it("emits only the codecs MediaSource declares supported", () => {
    // Modern Chrome-like: H264 + VP9 + Opus + AAC + MP3.
    (window as Window).MediaSource = {
      isTypeSupported: (mime: string) => {
        return (
          mime.includes('codecs="avc1') ||
          mime.includes('codecs="vp9"') ||
          mime === "audio/mpeg" ||
          mime.includes('codecs="mp4a') ||
          mime.includes('codecs="opus"')
        );
      },
    };
    const header = getClientCapabilitiesHeader();
    expect(header).not.toBeNull();
    // Has the codecs the env supports …
    expect(header).toContain("video=");
    expect(header).toContain("h264");
    expect(header).toContain("vp9");
    expect(header).toContain("audio=");
    expect(header).toContain("aac");
    expect(header).toContain("mp3");
    expect(header).toContain("opus");
    // … and not the ones it doesn't.
    expect(header).not.toContain("hevc");
    expect(header).not.toContain("eac3");
  });

  it("always advertises the standard container set when at least one codec works", () => {
    (window as Window).MediaSource = {
      isTypeSupported: (mime: string) => mime.includes('codecs="avc1'),
    };
    const header = getClientCapabilitiesHeader();
    expect(header).toContain("container=mp4,webm,mov");
  });

  it("memoises the result across calls", () => {
    let callCount = 0;
    (window as Window).MediaSource = {
      isTypeSupported: () => {
        callCount += 1;
        return true;
      },
    };
    const first = getClientCapabilitiesHeader();
    const second = getClientCapabilitiesHeader();
    expect(first).toBe(second);
    // Probe ran at most once; second call hit the cache.
    const probesPerCall = callCount;
    getClientCapabilitiesHeader();
    expect(callCount).toBe(probesPerCall);
  });

  it("emits both hevc and h265 aliases when MediaSource decodes the family", () => {
    // FFprobe normalises to "hevc"; some legacy clients see "h265".
    // Listing both means the server's set lookup matches whichever
    // wire codec name ends up on the item.
    (window as Window).MediaSource = {
      isTypeSupported: (mime: string) => mime.includes('codecs="hev1'),
    };
    const header = getClientCapabilitiesHeader();
    expect(header).toContain("hevc");
    expect(header).toContain("h265");
  });

  it("does not crash when MediaSource.isTypeSupported is missing", () => {
    (window as Window).MediaSource = {} as Window["MediaSource"];
    expect(getClientCapabilitiesHeader()).toBeNull();
  });

  it("is silenced (returns null) when probes match nothing", () => {
    // No video AND no audio matched — instead of sending a useless
    // "container=mp4,webm,mov" header that lies about decoding ability,
    // suppress entirely so the server falls back to its conservative
    // defaults.
    (window as Window).MediaSource = {
      isTypeSupported: () => false,
    };
    expect(getClientCapabilitiesHeader()).toBeNull();
  });
});

// Spot-check that the header actually rides on a real fetch through
// the api client.
describe("client request — sends X-Hubplay-Client-Capabilities", () => {
  it("attaches the header to GET requests when MediaSource is wired", async () => {
    resetClientCapabilitiesCacheForTests();
    (window as Window).MediaSource = {
      isTypeSupported: (mime: string) => mime.includes('codecs="avc1'),
    };

    const fetchSpy = vi.spyOn(window, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ ok: true }), { status: 200 }),
    );

    // Lazy-import so the client picks up the mocked window.MediaSource.
    const { api } = await import("./client");
    await api.getMe().catch(() => {
      // /me may not match the mocked response shape — we only care
      // that the request *fired* with the header.
    });

    expect(fetchSpy).toHaveBeenCalled();
    const init = fetchSpy.mock.calls[0][1] as RequestInit;
    const headers = init.headers as Record<string, string>;
    expect(headers["X-Hubplay-Client-Capabilities"]).toBeDefined();
    expect(headers["X-Hubplay-Client-Capabilities"]).toContain("h264");

    fetchSpy.mockRestore();
    delete (window as Window).MediaSource;
    resetClientCapabilitiesCacheForTests();
  });
});
