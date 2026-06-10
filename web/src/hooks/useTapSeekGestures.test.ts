import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook } from "@testing-library/react";
import type { MouseEvent as ReactMouseEvent } from "react";
import { useTapSeekGestures } from "./useTapSeekGestures";

// Evento sintético con coordenada X relativa (0..1) sobre una
// superficie de 1000px de ancho.
function tapAt(xFraction: number): ReactMouseEvent<HTMLElement> {
  return {
    clientX: xFraction * 1000,
    currentTarget: {
      getBoundingClientRect: () => ({ left: 0, width: 1000 }),
    },
  } as unknown as ReactMouseEvent<HTMLElement>;
}

describe("useTapSeekGestures", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  function mount(isMobile = true) {
    const onSingleTap = vi.fn();
    const onZoneSkip = vi.fn();
    const hook = renderHook(() =>
      useTapSeekGestures({ isMobile, onSingleTap, onZoneSkip }),
    );
    return { hook, onSingleTap, onZoneSkip };
  }

  it("desktop: el click dispara el single tap inmediato (sin ventana)", () => {
    const { hook, onSingleTap, onZoneSkip } = mount(false);
    hook.result.current.handleSurfaceClick(tapAt(0.1));
    expect(onSingleTap).toHaveBeenCalledTimes(1);
    expect(onZoneSkip).not.toHaveBeenCalled();
  });

  it("móvil: doble-tap en el tercio izquierdo salta atrás y cancela el single", () => {
    const { hook, onSingleTap, onZoneSkip } = mount();
    hook.result.current.handleSurfaceClick(tapAt(0.1));
    vi.advanceTimersByTime(100);
    hook.result.current.handleSurfaceClick(tapAt(0.12));

    expect(onZoneSkip).toHaveBeenCalledWith("back");
    vi.advanceTimersByTime(1000);
    expect(onSingleTap).not.toHaveBeenCalled();
  });

  it("móvil: doble-tap en el tercio derecho salta adelante", () => {
    const { hook, onZoneSkip } = mount();
    hook.result.current.handleSurfaceClick(tapAt(0.9));
    vi.advanceTimersByTime(100);
    hook.result.current.handleSurfaceClick(tapAt(0.88));
    expect(onZoneSkip).toHaveBeenCalledWith("fwd");
  });

  it("móvil: el tap simple dispara el toggle tras la ventana de doble-tap", () => {
    const { hook, onSingleTap, onZoneSkip } = mount();
    hook.result.current.handleSurfaceClick(tapAt(0.5));
    expect(onSingleTap).not.toHaveBeenCalled(); // diferido
    vi.advanceTimersByTime(320);
    expect(onSingleTap).toHaveBeenCalledTimes(1);
    expect(onZoneSkip).not.toHaveBeenCalled();
  });

  it("móvil: doble-tap en el centro NO salta (toggle normal)", () => {
    const { hook, onSingleTap, onZoneSkip } = mount();
    hook.result.current.handleSurfaceClick(tapAt(0.5));
    vi.advanceTimersByTime(100);
    hook.result.current.handleSurfaceClick(tapAt(0.5));
    vi.advanceTimersByTime(320);
    expect(onZoneSkip).not.toHaveBeenCalled();
    expect(onSingleTap).toHaveBeenCalledTimes(1); // el segundo re-difirió
  });

  it("móvil: taps encadenados en la misma zona siguen saltando al ritmo del pulgar", () => {
    const { hook, onZoneSkip } = mount();
    hook.result.current.handleSurfaceClick(tapAt(0.9));
    vi.advanceTimersByTime(100);
    hook.result.current.handleSurfaceClick(tapAt(0.9)); // doble → salto 1
    vi.advanceTimersByTime(150);
    hook.result.current.handleSurfaceClick(tapAt(0.9)); // encadenado → salto 2
    vi.advanceTimersByTime(150);
    hook.result.current.handleSurfaceClick(tapAt(0.9)); // encadenado → salto 3
    expect(onZoneSkip).toHaveBeenCalledTimes(3);
  });
});
