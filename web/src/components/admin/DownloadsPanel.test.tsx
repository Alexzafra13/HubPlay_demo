// DownloadsPanel — el "×" de cada job según su estado (T-9):
//   - en cola / descargando → "Cancelar descarga" (DELETE cancela el job)
//   - copiando a la biblioteca → sin botón (el backend respondería 409)
//   - completado / error → "Descartar"
// El progreso en vivo llega por SSE; aquí useEventStream es un no-op.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import "@/i18n";
import type { DownloadJob } from "@/api/types";

const apiMock = vi.hoisted(() => ({
  listDownloads: vi.fn(),
  dismissDownload: vi.fn(),
}));
vi.mock("@/api/client", () => ({ api: apiMock }));
vi.mock("@/hooks/useEventStream", () => ({ useEventStream: vi.fn() }));

import { DownloadsPanel } from "./DownloadsPanel";

function job(id: string, status: DownloadJob["status"]): DownloadJob {
  return { id, name: `Job ${id}`, status, bytes_done: 50, bytes_total: 100 };
}

function renderPanel() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <DownloadsPanel />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  apiMock.listDownloads.mockReset();
  apiMock.dismissDownload.mockReset();
});

describe("DownloadsPanel", () => {
  it("renders nothing when there are no jobs", async () => {
    apiMock.listDownloads.mockResolvedValueOnce([]);
    const { container } = renderPanel();
    await waitFor(() => expect(apiMock.listDownloads).toHaveBeenCalled());
    expect(container).toBeEmptyDOMElement();
  });

  it("offers cancel for active jobs, dismiss for finished ones and nothing while copying", async () => {
    apiMock.listDownloads.mockResolvedValue([
      job("q", "queued"),
      job("d", "downloading"),
      job("c", "copying"),
      job("ok", "completed"),
      job("ko", "failed"),
    ]);
    renderPanel();
    await screen.findByText("Job q");

    expect(screen.getAllByRole("button", { name: /cancel/i })).toHaveLength(2);
    expect(screen.getAllByRole("button", { name: /descartar|dismiss/i })).toHaveLength(2);
    expect(screen.getAllByRole("button")).toHaveLength(4); // el de "copying" no tiene

    apiMock.dismissDownload.mockResolvedValueOnce(undefined);
    fireEvent.click(screen.getAllByRole("button", { name: /cancel/i })[1]);
    await waitFor(() => expect(apiMock.dismissDownload).toHaveBeenCalledWith("d"));
  });
});
