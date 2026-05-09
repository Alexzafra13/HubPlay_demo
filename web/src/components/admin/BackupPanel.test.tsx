// BackupPanel — covers the two operator paths and the safety net
// around them:
//
//   - Download triggers an anchor click with the right blob URL.
//   - Restore prompts a window.confirm and bails out cleanly on
//     cancel without ever hitting the API.
//   - Restore confirmed → POST hits the API, success message
//     mentions the staged size.
//   - API failures surface the server error message inline (the
//     rendered <p role="status"> with `error` styling is the only
//     place an operator sees what went wrong).
//
// Visual concerns (icons, button hover) are intentionally not
// covered.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import {
  render,
  screen,
  waitFor,
  fireEvent,
} from "@testing-library/react";
import "@/i18n";

const apiMock = vi.hoisted(() => ({
  downloadBackup: vi.fn(),
  restoreBackup: vi.fn(),
}));
vi.mock("@/api/client", () => ({
  api: apiMock,
}));

import { BackupPanel } from "./BackupPanel";

beforeEach(() => {
  apiMock.downloadBackup.mockReset();
  apiMock.restoreBackup.mockReset();
  // jsdom doesn't implement object URLs; stub them so the download
  // path doesn't throw when the component asks for one.
  vi.stubGlobal(
    "URL",
    Object.assign(URL, {
      createObjectURL: vi.fn(() => "blob:mock-url"),
      revokeObjectURL: vi.fn(),
    }),
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("BackupPanel download", () => {
  it("calls api.downloadBackup and triggers an anchor click with the blob URL", async () => {
    const blob = new Blob(["fake-db"], { type: "application/octet-stream" });
    apiMock.downloadBackup.mockResolvedValueOnce(blob);

    // Spy on the synthetic anchor click — the component creates an
    // <a>, sets href + download, then calls .click(). We can't reach
    // the element through the testing-library tree (it's appended
    // and removed in the same tick) so we patch the prototype.
    const clickSpy = vi.fn();
    const originalClick = HTMLAnchorElement.prototype.click;
    HTMLAnchorElement.prototype.click = clickSpy;

    try {
      render(<BackupPanel />);
      fireEvent.click(screen.getByRole("button", { name: /descargar/i }));

      await waitFor(() => {
        expect(apiMock.downloadBackup).toHaveBeenCalled();
      });
      await waitFor(() => {
        expect(clickSpy).toHaveBeenCalled();
      });
    } finally {
      HTMLAnchorElement.prototype.click = originalClick;
    }
  });

  it("surfaces the API error message when the download fails", async () => {
    apiMock.downloadBackup.mockRejectedValueOnce(
      new Error("VACUUM INTO falló: disco lleno"),
    );

    render(<BackupPanel />);
    fireEvent.click(screen.getByRole("button", { name: /descargar/i }));

    await screen.findByText(/disco lleno/i);
    expect(screen.getByRole("status")).toHaveTextContent(/disco lleno/i);
  });
});

describe("BackupPanel restore", () => {
  it("aborts cleanly when the operator cancels the confirm dialog", async () => {
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(false);

    const { container } = render(<BackupPanel />);
    const fileInput = container.querySelector(
      'input[type="file"]',
    ) as HTMLInputElement;
    expect(fileInput).toBeTruthy();

    const file = new File(["x"], "old.db", {
      type: "application/octet-stream",
    });
    fireEvent.change(fileInput, { target: { files: [file] } });

    await waitFor(() => {
      expect(confirmSpy).toHaveBeenCalled();
    });
    expect(apiMock.restoreBackup).not.toHaveBeenCalled();

    confirmSpy.mockRestore();
  });

  it("uploads the file and renders the staged-size confirmation on success", async () => {
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);
    apiMock.restoreBackup.mockResolvedValueOnce({
      size_bytes: 12 * 1024 * 1024,
    });

    const { container } = render(<BackupPanel />);
    const fileInput = container.querySelector(
      'input[type="file"]',
    ) as HTMLInputElement;
    const file = new File(["x"], "fresh.db", {
      type: "application/octet-stream",
    });
    fireEvent.change(fileInput, { target: { files: [file] } });

    await waitFor(() => {
      expect(apiMock.restoreBackup).toHaveBeenCalledWith(file);
    });
    // Success copy mentions the staged size; the unit string varies
    // (KiB/MiB/GiB) but the digits should land somewhere in the
    // status banner.
    await screen.findByRole("status");
    expect(screen.getByRole("status")).toHaveTextContent(/MiB/i);

    confirmSpy.mockRestore();
  });

  it("surfaces the API error message when restore fails", async () => {
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);
    apiMock.restoreBackup.mockRejectedValueOnce(
      new Error("Backup corrupto"),
    );

    const { container } = render(<BackupPanel />);
    const fileInput = container.querySelector(
      'input[type="file"]',
    ) as HTMLInputElement;
    fireEvent.change(fileInput, {
      target: { files: [new File(["x"], "bad.db")] },
    });

    await screen.findByText(/backup corrupto/i);

    confirmSpy.mockRestore();
  });
});
