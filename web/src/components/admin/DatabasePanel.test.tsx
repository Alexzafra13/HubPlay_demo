import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { DatabasePanel } from "./DatabasePanel";
import { api } from "@/api/client";

// Mock the API surface this panel reaches into. Keeps the test
// hermetic — no React Query auto-fetching against a real backend —
// and lets each test wire its own return shapes.
vi.mock("@/api/client", () => ({
  api: {
    getAdminDatabase: vi.fn(),
    testAdminDatabase: vi.fn(),
    saveAdminDatabase: vi.fn(),
    restartServer: vi.fn(),
    migrateDatabase: vi.fn(),
  },
}));

function renderWithClient(ui: React.ReactElement) {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

describe("DatabasePanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders the live driver + path from the status endpoint", async () => {
    vi.mocked(api.getAdminDatabase).mockResolvedValue({
      driver: "sqlite",
      path: "/config/hubplay.db",
      pool: { max_open: 1, open: 1, in_use: 0, idle: 1, wait_count: 0, wait_duration_ms: 0 },
    });
    renderWithClient(<DatabasePanel />);
    // The path renders only in the status card (the form shows a placeholder
    // until the operator types), so it's a unique-enough anchor.
    await waitFor(() => {
      expect(screen.getByText(/config\/hubplay\.db/)).toBeInTheDocument();
    });
    // "SQLite" appears in both the badge and the radio form — at least one
    // is enough to confirm the renderer wired the live driver through.
    expect(screen.getAllByText("SQLite").length).toBeGreaterThan(0);
  });

  it("renders the live driver as PostgreSQL + the redacted DSN when pg is active", async () => {
    vi.mocked(api.getAdminDatabase).mockResolvedValue({
      driver: "postgres",
      dsn_redacted: "postgres://u:***@host:5432/db",
      pool: { max_open: 25, open: 5, in_use: 2, idle: 3, wait_count: 1, wait_duration_ms: 12 },
    });
    renderWithClient(<DatabasePanel />);
    await waitFor(() => {
      expect(screen.getByText(/postgres:\/\/u:\*\*\*@host/i)).toBeInTheDocument();
    });
    expect(screen.getAllByText("PostgreSQL").length).toBeGreaterThan(0);
    // Pool stats render in the same card.
    expect(screen.getByText(/2\/25/)).toBeInTheDocument();
  });

  it("only enables Save buttons after a successful Test", async () => {
    vi.mocked(api.getAdminDatabase).mockResolvedValue({
      driver: "sqlite",
      path: "/config/hubplay.db",
      pool: { max_open: 1, open: 1, in_use: 0, idle: 1, wait_count: 0, wait_duration_ms: 0 },
    });
    vi.mocked(api.testAdminDatabase).mockResolvedValue({
      ok: true,
      duration_ms: 5,
    });

    renderWithClient(<DatabasePanel />);
    await waitFor(() => expect(screen.getByText("SQLite")).toBeInTheDocument());

    // Initial state: Save buttons are disabled (no test result yet).
    const saveBtn = screen.getByRole("button", { name: /admin\.database\.save$/i });
    const saveAndRestart = screen.getByRole("button", { name: /admin\.database\.saveAndRestart/i });
    expect(saveBtn).toBeDisabled();
    expect(saveAndRestart).toBeDisabled();

    // Run Test → buttons enable.
    fireEvent.click(screen.getByRole("button", { name: /admin\.database\.test/i }));
    await waitFor(() => expect(saveBtn).toBeEnabled());
    expect(saveAndRestart).toBeEnabled();
  });

  it("hides the migration card when the live driver is already postgres", async () => {
    vi.mocked(api.getAdminDatabase).mockResolvedValue({
      driver: "postgres",
      dsn_redacted: "postgres://u:***@host:5432/db",
      pool: { max_open: 25, open: 0, in_use: 0, idle: 0, wait_count: 0, wait_duration_ms: 0 },
    });
    renderWithClient(<DatabasePanel />);
    await waitFor(() => {
      expect(screen.getByText(/postgres:\/\/u:\*\*\*@host/i)).toBeInTheDocument();
    });
    expect(screen.queryByText(/admin\.database\.migrateTitle/i)).toBeNull();
  });
});
