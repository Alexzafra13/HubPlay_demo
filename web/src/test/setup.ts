import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach, vi } from "vitest";

// jsdom does not implement window.matchMedia. Components that read it
// (e.g. usePrefersReducedMotion, useIsMobile) crash without this stub,
// so we provide a no-op default. Individual tests can override it
// (vi.stubGlobal / direct reassignment) for cases where they need to
// emulate a specific media-query state.
if (typeof window !== "undefined" && !window.matchMedia) {
  window.matchMedia = vi.fn().mockImplementation((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }));
}

// jsdom does not implement EventSource. Hooks built on the SSE event
// bus (useEventStream, useUserEventStream) instantiate `new EventSource`
// at mount; without a stub, any component that calls a hook which
// internally subscribes to SSE crashes in unrelated tests.
//
// This default is a no-op: it accepts add/remove listener calls and
// close, never emits anything. Tests that need to drive events stub
// their own implementation via vi.stubGlobal("EventSource", ...) and
// drive .emit() manually — see e.g. useUserDataSync.test.tsx.
if (typeof globalThis !== "undefined" && typeof (globalThis as { EventSource?: unknown }).EventSource === "undefined") {
  class NoopEventSource {
    url: string;
    readyState = 0;
    withCredentials = false;
    constructor(url: string) {
      this.url = url;
    }
    addEventListener() {}
    removeEventListener() {}
    close() {}
    onopen: ((e: Event) => void) | null = null;
    onmessage: ((e: MessageEvent) => void) | null = null;
    onerror: ((e: Event) => void) | null = null;
  }
  (globalThis as { EventSource: unknown }).EventSource = NoopEventSource;
}

afterEach(() => {
  cleanup();
  localStorage.clear();
});
