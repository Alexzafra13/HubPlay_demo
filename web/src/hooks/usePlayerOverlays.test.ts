import { describe, it, expect, vi } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { usePlayerOverlays } from "./usePlayerOverlays";
import type { ExternalSubtitleResult } from "@/api/types";

const sampleSub: ExternalSubtitleResult = {
  source: "opensubtitles",
  file_id: "fid-1",
  language: "es",
  format: "srt",
  score: 0.9,
};

describe("usePlayerOverlays", () => {
  it("starts with all overlays closed", () => {
    const { result } = renderHook(() =>
      usePlayerOverlays({ itemId: "i1", hasNextUp: false }),
    );

    expect(result.current.upNextActive).toBe(false);
    expect(result.current.externalSubsModalOpen).toBe(false);
    expect(result.current.activeExternalSub).toBeNull();
    expect(result.current.showHelp).toBe(false);
  });

  it("handleEnded fires the up-next overlay when hasNextUp + callback present", () => {
    const onEndedCallback = vi.fn();
    const { result } = renderHook(() =>
      usePlayerOverlays({ itemId: "i1", hasNextUp: true, onEndedCallback }),
    );

    act(() => result.current.handleEnded());

    expect(result.current.upNextActive).toBe(true);
    expect(onEndedCallback).not.toHaveBeenCalled();
  });

  it("handleEnded calls onEndedCallback directly when hasNextUp is false", () => {
    const onEndedCallback = vi.fn();
    const { result } = renderHook(() =>
      usePlayerOverlays({ itemId: "i1", hasNextUp: false, onEndedCallback }),
    );

    act(() => result.current.handleEnded());

    expect(result.current.upNextActive).toBe(false);
    expect(onEndedCallback).toHaveBeenCalledOnce();
  });

  it("handleEnded calls onEndedCallback directly when no callback provided and hasNextUp true", () => {
    // El fork requiere AMBOS hasNextUp y onEndedCallback para mostrar
    // el overlay; sin callback no hay manera de auto-advance, así que
    // el fallback es no abrir el overlay (queda como no-op).
    const { result } = renderHook(() =>
      usePlayerOverlays({ itemId: "i1", hasNextUp: true }),
    );

    act(() => result.current.handleEnded());

    expect(result.current.upNextActive).toBe(false);
  });

  it("handleUpNextConfirm closes the overlay and fires the callback", () => {
    const onEndedCallback = vi.fn();
    const { result } = renderHook(() =>
      usePlayerOverlays({ itemId: "i1", hasNextUp: true, onEndedCallback }),
    );

    act(() => result.current.handleEnded());
    expect(result.current.upNextActive).toBe(true);

    act(() => result.current.handleUpNextConfirm());

    expect(result.current.upNextActive).toBe(false);
    expect(onEndedCallback).toHaveBeenCalledOnce();
  });

  it("handleUpNextCancel closes the overlay without firing the callback", () => {
    const onEndedCallback = vi.fn();
    const { result } = renderHook(() =>
      usePlayerOverlays({ itemId: "i1", hasNextUp: true, onEndedCallback }),
    );

    act(() => result.current.handleEnded());
    act(() => result.current.handleUpNextCancel());

    expect(result.current.upNextActive).toBe(false);
    expect(onEndedCallback).not.toHaveBeenCalled();
  });

  it("openExternalSubsModal and closeExternalSubsModal toggle the flag", () => {
    const { result } = renderHook(() =>
      usePlayerOverlays({ itemId: "i1", hasNextUp: false }),
    );

    act(() => result.current.openExternalSubsModal());
    expect(result.current.externalSubsModalOpen).toBe(true);

    act(() => result.current.closeExternalSubsModal());
    expect(result.current.externalSubsModalOpen).toBe(false);
  });

  it("pickExternalSub stores the pick and closes the modal in one shot", () => {
    const { result } = renderHook(() =>
      usePlayerOverlays({ itemId: "i1", hasNextUp: false }),
    );

    act(() => result.current.openExternalSubsModal());
    act(() => result.current.pickExternalSub(sampleSub));

    expect(result.current.activeExternalSub).toEqual(sampleSub);
    expect(result.current.externalSubsModalOpen).toBe(false);
  });

  it("clearExternalSub resets the active pick to null", () => {
    const { result } = renderHook(() =>
      usePlayerOverlays({ itemId: "i1", hasNextUp: false }),
    );

    act(() => result.current.pickExternalSub(sampleSub));
    expect(result.current.activeExternalSub).toEqual(sampleSub);

    act(() => result.current.clearExternalSub());
    expect(result.current.activeExternalSub).toBeNull();
  });

  it("toggleHelp flips the flag and closeHelp forces it false", () => {
    const { result } = renderHook(() =>
      usePlayerOverlays({ itemId: "i1", hasNextUp: false }),
    );

    act(() => result.current.toggleHelp());
    expect(result.current.showHelp).toBe(true);

    act(() => result.current.toggleHelp());
    expect(result.current.showHelp).toBe(false);

    act(() => result.current.toggleHelp());
    act(() => result.current.closeHelp());
    expect(result.current.showHelp).toBe(false);
  });

  it("SHOW_UP_NEXT dismisses an open help overlay", () => {
    // Cierre implícito: si el usuario tiene el panel de atajos abierto
    // y el vídeo termina, el up-next debe robar el foco.
    const onEndedCallback = vi.fn();
    const { result } = renderHook(() =>
      usePlayerOverlays({ itemId: "i1", hasNextUp: true, onEndedCallback }),
    );

    act(() => result.current.toggleHelp());
    expect(result.current.showHelp).toBe(true);

    act(() => result.current.handleEnded());

    expect(result.current.upNextActive).toBe(true);
    expect(result.current.showHelp).toBe(false);
  });

  it("OPEN_EXTERNAL_SUBS dismisses an open help overlay", () => {
    const { result } = renderHook(() =>
      usePlayerOverlays({ itemId: "i1", hasNextUp: false }),
    );

    act(() => result.current.toggleHelp());
    expect(result.current.showHelp).toBe(true);

    act(() => result.current.openExternalSubsModal());

    expect(result.current.externalSubsModalOpen).toBe(true);
    expect(result.current.showHelp).toBe(false);
  });

  it("changing itemId resets every overlay back to initial state", () => {
    const { result, rerender } = renderHook(
      ({ itemId }: { itemId: string }) =>
        usePlayerOverlays({ itemId, hasNextUp: true, onEndedCallback: vi.fn() }),
      { initialProps: { itemId: "i1" } },
    );

    act(() => result.current.handleEnded());
    act(() => result.current.pickExternalSub(sampleSub));
    act(() => result.current.toggleHelp());

    expect(result.current.upNextActive).toBe(true);
    expect(result.current.activeExternalSub).toEqual(sampleSub);
    expect(result.current.showHelp).toBe(true);

    rerender({ itemId: "i2" });

    expect(result.current.upNextActive).toBe(false);
    expect(result.current.externalSubsModalOpen).toBe(false);
    expect(result.current.activeExternalSub).toBeNull();
    expect(result.current.showHelp).toBe(false);
  });
});
