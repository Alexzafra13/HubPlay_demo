import { describe, it, expect, vi, afterEach } from "vitest";
import { renderHook } from "@testing-library/react";
import { useFullscreenSync } from "./useFullscreenSync";

function fireFullscreenChange() {
  document.dispatchEvent(new Event("fullscreenchange"));
}

afterEach(() => {
  // Limpiar mocks de `document.fullscreenElement` entre tests.
  Object.defineProperty(document, "fullscreenElement", {
    value: null,
    writable: true,
    configurable: true,
  });
});

describe("useFullscreenSync", () => {
  it("llama al setter con true cuando entramos en fullscreen", () => {
    const setFullscreen = vi.fn();
    Object.defineProperty(document, "fullscreenElement", {
      value: document.documentElement,
      writable: true,
      configurable: true,
    });

    renderHook(() => useFullscreenSync(setFullscreen));
    fireFullscreenChange();

    expect(setFullscreen).toHaveBeenCalledWith(true);
  });

  it("llama al setter con false cuando salimos de fullscreen", () => {
    const setFullscreen = vi.fn();
    Object.defineProperty(document, "fullscreenElement", {
      value: null,
      writable: true,
      configurable: true,
    });

    renderHook(() => useFullscreenSync(setFullscreen));
    fireFullscreenChange();

    expect(setFullscreen).toHaveBeenCalledWith(false);
  });

  it("no llama al setter en el montaje (sólo registra listener)", () => {
    const setFullscreen = vi.fn();

    renderHook(() => useFullscreenSync(setFullscreen));

    expect(setFullscreen).not.toHaveBeenCalled();
  });

  it("retira el listener al desmontar", () => {
    const setFullscreen = vi.fn();

    const { unmount } = renderHook(() => useFullscreenSync(setFullscreen));

    unmount();
    fireFullscreenChange();

    expect(setFullscreen).not.toHaveBeenCalled();
  });

  it("rerender con un setter nuevo re-conecta el listener al nuevo callback", () => {
    const first = vi.fn();
    const second = vi.fn();

    const { rerender } = renderHook(
      ({ setter }: { setter: (b: boolean) => void }) =>
        useFullscreenSync(setter),
      { initialProps: { setter: first } },
    );

    rerender({ setter: second });
    fireFullscreenChange();

    expect(first).not.toHaveBeenCalled();
    expect(second).toHaveBeenCalledOnce();
  });
});
