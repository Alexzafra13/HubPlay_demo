// LivetvAdminPanel — covers the routing-style logic of the admin
// surface (tab switching + sub-panel mounting) without exercising
// each sub-panel's internals (those have their own tests).
//
// Sub-panels are mocked to simple sentinel divs so the test
// observes which panel mounted by checking for the sentinel text.

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
  getChannelHealthSummary: vi.fn(),
  getEPGCatalog: vi.fn(),
  listEPGSources: vi.fn(),
  listScheduledJobs: vi.fn(),
}));
vi.mock("@/api/client", () => ({ api: apiMock }));

// useEventStream is fire-and-forget here; the real implementation
// hits EventSource (not implemented in jsdom). Stub to a no-op so
// the panel mounts cleanly.
vi.mock("@/hooks/useEventStream", () => ({
  useEventStream: vi.fn(),
}));

// Sub-panels render the same complex hooks as the main panel; mock
// them to sentinel markers so we observe routing only.
vi.mock("./EPGSourcesPanel", () => ({
  EPGSourcesPanel: () => <div data-testid="sub-sources">EPG-SOURCES</div>,
}));
vi.mock("./ScheduledJobsPanel", () => ({
  ScheduledJobsPanel: () => <div data-testid="sub-schedule">SCHEDULE</div>,
}));
vi.mock("./UnhealthyChannelsPanel", () => ({
  UnhealthyChannelsPanel: () => (
    <div data-testid="sub-unhealthy">UNHEALTHY</div>
  ),
}));
vi.mock("./ChannelsWithoutEPGPanel", () => ({
  ChannelsWithoutEPGPanel: () => (
    <div data-testid="sub-without-epg">WITHOUT-EPG</div>
  ),
}));

import { LivetvAdminPanel } from "./LivetvAdminPanel";

function wrap(node: React.ReactElement) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

beforeEach(() => {
  apiMock.getChannelHealthSummary.mockReset();
  apiMock.getEPGCatalog.mockReset();
  apiMock.listEPGSources.mockReset();
  apiMock.listScheduledJobs.mockReset();

  apiMock.getChannelHealthSummary.mockResolvedValue({
    library_id: "lib-1",
    total_count: 100,
    unhealthy_count: 0,
    without_epg_count: 0,
  });
  apiMock.getEPGCatalog.mockResolvedValue([]);
  apiMock.listEPGSources.mockResolvedValue([]);
  apiMock.listScheduledJobs.mockResolvedValue([]);
});

describe("LivetvAdminPanel routing", () => {
  it("defaults to the sources tab on mount", async () => {
    render(wrap(<LivetvAdminPanel libraryId="lib-1" totalChannels={100} />));

    await screen.findByTestId("sub-sources");
    expect(screen.queryByTestId("sub-schedule")).not.toBeInTheDocument();
    expect(screen.queryByTestId("sub-unhealthy")).not.toBeInTheDocument();
  });

  it("hides the unhealthy tab when unhealthy_count is zero", async () => {
    render(wrap(<LivetvAdminPanel libraryId="lib-1" totalChannels={100} />));

    await screen.findByTestId("sub-sources");
    // The Unhealthy tab is suppressed when count=0 — the operator
    // doesn't need a tab pointing at an empty list.
    expect(
      screen.queryByRole("tab", { name: /problemas|unhealthy/i }),
    ).not.toBeInTheDocument();
  });

  it("renders the unhealthy tab when there are unhealthy channels", async () => {
    apiMock.getChannelHealthSummary.mockResolvedValue({
      library_id: "lib-1",
      total_count: 100,
      unhealthy_count: 5,
      without_epg_count: 0,
    });

    render(wrap(<LivetvAdminPanel libraryId="lib-1" totalChannels={100} />));

    // Wait for the data-driven tab to appear after the summary
    // loads. Match by aria-controls suffix since the visible text
    // varies by locale ("Con problemas" / "Unhealthy") and gets
    // suffixed with a count badge that doesn't always integrate
    // cleanly into accessible-name matching.
    await waitFor(() => {
      expect(
        document.querySelector('[aria-controls$="-unhealthy"]'),
      ).not.toBeNull();
    });
  });

  it("switches the active panel when a tab is clicked", async () => {
    render(wrap(<LivetvAdminPanel libraryId="lib-1" totalChannels={100} />));

    await screen.findByTestId("sub-sources");
    // Click the schedule tab. Same DOM-attribute trick as above —
    // visible text is locale-dependent ("Programación" / "Schedule")
    // but the aria-controls suffix is stable.
    const scheduleTab = document.querySelector<HTMLElement>(
      '[aria-controls$="-schedule"]',
    );
    expect(scheduleTab).not.toBeNull();
    fireEvent.click(scheduleTab!);

    await screen.findByTestId("sub-schedule");
    expect(screen.queryByTestId("sub-sources")).not.toBeInTheDocument();
  });

  it("shows the EPG coverage stat in the header strip", async () => {
    apiMock.getChannelHealthSummary.mockResolvedValue({
      library_id: "lib-1",
      total_count: 100,
      unhealthy_count: 0,
      without_epg_count: 25,
    });

    render(wrap(<LivetvAdminPanel libraryId="lib-1" totalChannels={100} />));

    // 75 of 100 matched → "(75%)" inside the matched-count span.
    await screen.findByText("(75%)");
  });
});
