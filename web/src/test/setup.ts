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

afterEach(() => {
  cleanup();
  localStorage.clear();
});
