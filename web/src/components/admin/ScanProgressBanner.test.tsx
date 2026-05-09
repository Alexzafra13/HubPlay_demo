// ScanProgressBanner — covers the "alive but no jitter" contract:
//
//   - When the live scan map is empty the component renders nothing
//     (so the page header doesn't bounce on mount).
//   - When one or more scans are active, each gets a row with the
//     library name and the running file count.
//   - Optional `currentPath` is rendered when present and skipped
//     when absent — the i18n string for the file count must always
//     be there.

import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import "@/i18n";

const useScanProgressMock = vi.hoisted(() => vi.fn());
vi.mock("@/api/hooks", () => ({
  useScanProgress: useScanProgressMock,
}));

import { ScanProgressBanner } from "./ScanProgressBanner";

function makeMap(
  entries: Array<{
    libraryId: string;
    libraryName: string;
    scanned: number;
    currentPath?: string;
  }>,
) {
  const m = new Map();
  for (const e of entries) {
    m.set(e.libraryId, {
      libraryId: e.libraryId,
      libraryName: e.libraryName,
      scanned: e.scanned,
      currentPath: e.currentPath,
      startedAt: Date.now(),
    });
  }
  return m;
}

describe("ScanProgressBanner", () => {
  it("renders nothing when there are no active scans", () => {
    useScanProgressMock.mockReturnValue(new Map());

    const { container } = render(<ScanProgressBanner />);

    expect(container).toBeEmptyDOMElement();
  });

  it("renders one row per active scan with name and file count", () => {
    useScanProgressMock.mockReturnValue(
      makeMap([
        { libraryId: "lib-1", libraryName: "Películas", scanned: 142 },
        { libraryId: "lib-2", libraryName: "Series", scanned: 7 },
      ]),
    );

    render(<ScanProgressBanner />);

    // Bilingual matchers — jsdom defaults to en-US so the test
    // environment renders English copy unless we force es; matching
    // either keeps the assertion robust to future locale flips.
    expect(
      screen.getByText(/(escaneando|scanning).*películas/i),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/(escaneando|scanning).*series/i),
    ).toBeInTheDocument();
    expect(screen.getByText(/142 (archivos|files)/i)).toBeInTheDocument();
    expect(screen.getByText(/7 (archivos|files)/i)).toBeInTheDocument();
  });

  it("shows the current path when provided", () => {
    useScanProgressMock.mockReturnValue(
      makeMap([
        {
          libraryId: "lib-1",
          libraryName: "Películas",
          scanned: 9,
          currentPath: "Acción/2024/Heat.mkv",
        },
      ]),
    );

    render(<ScanProgressBanner />);

    expect(screen.getByText("Acción/2024/Heat.mkv")).toBeInTheDocument();
  });

  it("omits the path slot when currentPath is missing", () => {
    useScanProgressMock.mockReturnValue(
      makeMap([
        { libraryId: "lib-1", libraryName: "Películas", scanned: 1 },
      ]),
    );

    render(<ScanProgressBanner />);

    // The path span uses a font-mono class as its only distinguishing
    // marker — if the component ever rendered the slot unconditionally
    // even with `currentPath` undefined, this assertion would catch it.
    const pathNode = document.querySelector(".font-mono");
    expect(pathNode).toBeNull();
  });
});
