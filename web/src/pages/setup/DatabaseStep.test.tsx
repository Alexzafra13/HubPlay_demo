import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import DatabaseStep from "./DatabaseStep";

// The step talks to two endpoints (/setup/db/test and /setup/db) through
// the api client. Mock that surface so tests stay deterministic and never
// hit the network — the wiring is the only thing we want to assert here.
vi.mock("@/api/client", () => ({
  api: {
    getSetupDatabaseProfiles: vi.fn(),
    testSetupDatabase: vi.fn(),
    saveSetupDatabase: vi.fn(),
  },
}));

import { api } from "@/api/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

function renderWithClient(ui: React.ReactElement) {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

describe("DatabaseStep", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // Default: no bundled — the legacy DSN form is rendered. Tests
    // that need the bundled flow override per-test.
    vi.mocked(api.getSetupDatabaseProfiles).mockResolvedValue({
      bundled_postgres: false,
    });
  });

  it('shows the SQLite path field by default and lets the operator "skip" to the next step', async () => {
    const onNext = vi.fn();
    renderWithClient(<DatabaseStep onNext={onNext} />);
    // Wait for the profiles query to settle so the form renders.
    await waitFor(() => {
      expect(screen.getByPlaceholderText(/hubplay\.db/i)).toBeInTheDocument();
    });

    expect(screen.getByPlaceholderText(/hubplay\.db/i)).toBeInTheDocument();
    fireEvent.click(screen.getByText(/setup\.database\.skip/i));
    expect(onNext).toHaveBeenCalledOnce();
  });

  it("renders the postgres DSN field when the operator picks the postgres radio", async () => {
    renderWithClient(<DatabaseStep onNext={vi.fn()} />);
    await waitFor(() => {
      expect(screen.getByRole("radio", { name: /PostgreSQL/ })).toBeInTheDocument();
    });
    fireEvent.click(screen.getByRole("radio", { name: /PostgreSQL/ }));
    expect(
      screen.getByPlaceholderText(/postgres:\/\/user:pass/i),
    ).toBeInTheDocument();
  });

  it("calls /setup/db/test on Test and renders the result", async () => {
    vi.mocked(api.testSetupDatabase).mockResolvedValue({
      ok: true,
      driver_detected: "postgres",
      server_version: "PostgreSQL 16.2 …",
      duration_ms: 42,
    });
    renderWithClient(<DatabaseStep onNext={vi.fn()} />);
    await waitFor(() => {
      expect(screen.getByRole("radio", { name: /PostgreSQL/ })).toBeInTheDocument();
    });
    fireEvent.click(screen.getByRole("radio", { name: /PostgreSQL/ }));
    fireEvent.change(screen.getByPlaceholderText(/postgres:\/\/user:pass/i), {
      target: { value: "postgres://u:p@h/d" },
    });
    fireEvent.click(screen.getByRole("button", { name: /setup\.database\.test/i }));

    await waitFor(() => {
      expect(api.testSetupDatabase).toHaveBeenCalledWith({
        driver: "postgres",
        path: undefined,
        dsn: "postgres://u:p@h/d",
      });
    });
    await waitFor(() => {
      expect(screen.getByText(/PostgreSQL 16\.2/i)).toBeInTheDocument();
    });
  });

  it('enables "Save & Restart" only after a successful test', async () => {
    vi.mocked(api.testSetupDatabase).mockResolvedValue({
      ok: true,
      duration_ms: 10,
    });
    vi.mocked(api.saveSetupDatabase).mockResolvedValue({
      status: "saved",
      restart_scheduled: true,
    });
    renderWithClient(<DatabaseStep onNext={vi.fn()} />);
    await waitFor(() => {
      expect(screen.getByRole("radio", { name: /PostgreSQL/ })).toBeInTheDocument();
    });
    fireEvent.click(screen.getByRole("radio", { name: /PostgreSQL/ }));
    fireEvent.change(screen.getByPlaceholderText(/postgres:\/\/user:pass/i), {
      target: { value: "postgres://u:p@h/d" },
    });

    const saveButton = screen.getByRole("button", { name: /setup\.database\.saveAndRestart/i });
    expect(saveButton).toBeDisabled();

    fireEvent.click(screen.getByRole("button", { name: /setup\.database\.test/i }));
    await waitFor(() => expect(saveButton).toBeEnabled());

    fireEvent.click(saveButton);
    await waitFor(() => {
      expect(api.saveSetupDatabase).toHaveBeenCalledWith({
        driver: "postgres",
        path: undefined,
        dsn: "postgres://u:p@h/d",
        restart: true,
      });
    });
  });

  it("shows two one-click cards (no DSN field) when bundled is available", async () => {
    vi.mocked(api.getSetupDatabaseProfiles).mockResolvedValue({
      bundled_postgres: true,
      bundled_label: "PostgreSQL (bundled)",
    });
    renderWithClient(<DatabaseStep onNext={vi.fn()} />);

    // Wait for the cards. They render as <button> with the labels.
    await waitFor(() => {
      expect(screen.getByRole("button", { name: /SQLite/ })).toBeInTheDocument();
    });
    expect(screen.getByRole("button", { name: /PostgreSQL/ })).toBeInTheDocument();
    // The custom DSN form must NOT be present in this flow.
    expect(screen.queryByPlaceholderText(/postgres:\/\/user:pass/i)).toBeNull();
  });

  it("renders the test failure inline without crashing the form", async () => {
    vi.mocked(api.testSetupDatabase).mockResolvedValue({
      ok: false,
      duration_ms: 8,
      error: "connection refused",
    });
    renderWithClient(<DatabaseStep onNext={vi.fn()} />);
    await waitFor(() => {
      expect(screen.getByRole("radio", { name: /PostgreSQL/ })).toBeInTheDocument();
    });
    fireEvent.click(screen.getByRole("radio", { name: /PostgreSQL/ }));
    fireEvent.change(screen.getByPlaceholderText(/postgres:\/\/user:pass/i), {
      target: { value: "postgres://u:p@h/d" },
    });
    fireEvent.click(screen.getByRole("button", { name: /setup\.database\.test/i }));

    await waitFor(() => {
      expect(screen.getByText(/connection refused/i)).toBeInTheDocument();
    });
  });
});
