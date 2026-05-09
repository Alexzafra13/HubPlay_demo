// ScheduledJobsPanel — covers the operator-side contract of the
// automated M3U / EPG refresh UI:
//
//   - Empty list (no jobs from the API) renders the "no jobs" copy
//     instead of an empty table.
//   - Loaded jobs render with kind label + status badge.
//   - Toggling enabled fires upsertScheduledJob with the new state.
//   - Changing the interval dropdown also fires upsertScheduledJob.
//   - "Run now" fires runScheduledJobNow.
//   - StatusBadge variants render the matching i18n copy.
//
// The relative-time helper inside formatRelative is left untested
// here — it's a pure helper and the UI assertions don't depend on
// the exact phrasing.

import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  render,
  screen,
  fireEvent,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import "@/i18n";

const apiMock = vi.hoisted(() => ({
  listScheduledJobs: vi.fn(),
  upsertScheduledJob: vi.fn(),
  runScheduledJobNow: vi.fn(),
  deleteScheduledJob: vi.fn(),
}));
vi.mock("@/api/client", () => ({ api: apiMock }));

import { ScheduledJobsPanel } from "./ScheduledJobsPanel";
import type { IPTVScheduledJob } from "@/api/types";

function wrap(node: React.ReactElement) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

beforeEach(() => {
  apiMock.listScheduledJobs.mockReset();
  apiMock.upsertScheduledJob.mockReset();
  apiMock.runScheduledJobNow.mockReset();
});

const sampleJobs: IPTVScheduledJob[] = [
  {
    library_id: "lib-1",
    kind: "m3u_refresh",
    interval_hours: 6,
    enabled: true,
    last_run_at: new Date(Date.now() - 3 * 60 * 60_000).toISOString(),
    last_status: "ok",
    last_duration_ms: 1234,
  },
  {
    library_id: "lib-1",
    kind: "epg_refresh",
    interval_hours: 24,
    enabled: false,
    last_status: "",
    last_duration_ms: 0,
  },
];

describe("ScheduledJobsPanel empty + loaded states", () => {
  it("renders the no-jobs copy when the API returns an empty list", async () => {
    apiMock.listScheduledJobs.mockResolvedValueOnce([]);

    render(wrap(<ScheduledJobsPanel libraryId="lib-1" />));

    await screen.findByText(
      /sin tareas programadas|no scheduled tasks/i,
    );
  });

  it("renders one row per scheduled job with its kind label", async () => {
    apiMock.listScheduledJobs.mockResolvedValueOnce(sampleJobs);

    render(wrap(<ScheduledJobsPanel libraryId="lib-1" />));

    await screen.findByText(/refrescar m3u|refresh m3u/i);
    expect(
      screen.getByText(/refrescar epg|refresh epg/i),
    ).toBeInTheDocument();
  });
});

describe("ScheduledJobsPanel mutations", () => {
  it("fires upsertScheduledJob when the enabled toggle flips", async () => {
    apiMock.listScheduledJobs.mockResolvedValueOnce(sampleJobs);
    apiMock.upsertScheduledJob.mockResolvedValueOnce({
      ...sampleJobs[1],
      enabled: true,
    });

    render(wrap(<ScheduledJobsPanel libraryId="lib-1" />));

    // EPG row starts disabled in the fixture; clicking the
    // checkbox should fire an upsert with `enabled: true`.
    await screen.findByText(/refrescar epg|refresh epg/i);
    const epgToggles = screen.getAllByRole("checkbox");
    // Fixture order: m3u_refresh first, epg_refresh second.
    fireEvent.click(epgToggles[1]);

    await waitFor(() => {
      expect(apiMock.upsertScheduledJob).toHaveBeenCalledWith(
        "lib-1",
        "epg_refresh",
        expect.objectContaining({ enabled: true }),
      );
    });
  });

  it("fires upsertScheduledJob when the interval dropdown changes", async () => {
    apiMock.listScheduledJobs.mockResolvedValueOnce(sampleJobs);
    apiMock.upsertScheduledJob.mockResolvedValueOnce({
      ...sampleJobs[0],
      interval_hours: 12,
    });

    render(wrap(<ScheduledJobsPanel libraryId="lib-1" />));

    await screen.findByText(/refrescar m3u|refresh m3u/i);
    // m3u row is enabled with interval=6; bump to 12.
    const selects = screen.getAllByRole("combobox");
    fireEvent.change(selects[0], { target: { value: "12" } });

    await waitFor(() => {
      expect(apiMock.upsertScheduledJob).toHaveBeenCalledWith(
        "lib-1",
        "m3u_refresh",
        expect.objectContaining({ interval_hours: 12 }),
      );
    });
  });

  it("fires runScheduledJobNow when the Run now button is clicked", async () => {
    apiMock.listScheduledJobs.mockResolvedValueOnce(sampleJobs);
    apiMock.runScheduledJobNow.mockResolvedValueOnce(sampleJobs[0]);

    render(wrap(<ScheduledJobsPanel libraryId="lib-1" />));

    await screen.findByText(/refrescar m3u|refresh m3u/i);
    const buttons = screen.getAllByRole("button", {
      name: /ejecutar ahora|run now/i,
    });
    fireEvent.click(buttons[0]);

    await waitFor(() => {
      expect(apiMock.runScheduledJobNow).toHaveBeenCalledWith(
        "lib-1",
        "m3u_refresh",
      );
    });
  });

  it("renders the OK status badge for jobs whose last run succeeded", async () => {
    apiMock.listScheduledJobs.mockResolvedValueOnce(sampleJobs);

    render(wrap(<ScheduledJobsPanel libraryId="lib-1" />));

    // The m3u_refresh fixture has last_status="ok" → "OK" badge.
    await screen.findByText(/^OK$/);
  });
});
