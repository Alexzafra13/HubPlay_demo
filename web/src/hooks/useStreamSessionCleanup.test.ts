import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook } from "@testing-library/react";
import { useStreamSessionCleanup } from "./useStreamSessionCleanup";
import { api } from "@/api/client";

vi.mock("@/api/client", () => ({
  api: {
    stopStreamSession: vi.fn().mockResolvedValue(undefined),
  },
}));

function firePageHide() {
  window.dispatchEvent(new Event("pagehide"));
}

describe("useStreamSessionCleanup", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("no invoca la API en el montaje (sólo registra listener)", () => {
    renderHook(() => useStreamSessionCleanup("item-1"));

    expect(api.stopStreamSession).not.toHaveBeenCalled();
  });

  it("llama a api.stopStreamSession con itemId al disparar pagehide", () => {
    renderHook(() => useStreamSessionCleanup("item-42"));

    firePageHide();

    expect(api.stopStreamSession).toHaveBeenCalledWith("item-42");
    expect(api.stopStreamSession).toHaveBeenCalledOnce();
  });

  it("swallowea rechazos de la API (best-effort)", async () => {
    vi.mocked(api.stopStreamSession).mockRejectedValueOnce(
      new Error("network down"),
    );

    renderHook(() => useStreamSessionCleanup("item-1"));

    firePageHide();

    // El handler usa `void ... .catch(() => {})`, así que el rejection
    // no se propaga ni rompe la página. La promesa rechazada se
    // resuelve internamente — el test pasa si NO hay unhandled.
    await Promise.resolve();
    expect(api.stopStreamSession).toHaveBeenCalledOnce();
  });

  it("retira el listener al desmontar", () => {
    const { unmount } = renderHook(() => useStreamSessionCleanup("item-1"));

    unmount();
    firePageHide();

    expect(api.stopStreamSession).not.toHaveBeenCalled();
  });

  it("rerender con nuevo itemId usa el nuevo id en futuras llamadas", () => {
    const { rerender } = renderHook(
      ({ id }: { id: string }) => useStreamSessionCleanup(id),
      { initialProps: { id: "item-old" } },
    );

    rerender({ id: "item-new" });

    firePageHide();

    expect(api.stopStreamSession).toHaveBeenCalledWith("item-new");
    expect(api.stopStreamSession).not.toHaveBeenCalledWith("item-old");
  });

  // PB-17: en reproducción federada itemId es el id REMOTO — el DELETE
  // local 404eaba. Sin endpoint de stop remoto, el reaper del peer es
  // el mecanismo correcto: aquí no se dispara nada.
  it("no llama a la API para items federados (peerId presente)", () => {
    renderHook(() => useStreamSessionCleanup("remote-item", "peer-1"));

    firePageHide();

    expect(api.stopStreamSession).not.toHaveBeenCalled();
  });
});
