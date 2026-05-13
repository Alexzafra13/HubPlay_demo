import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import SetupWizard from "./SetupWizard";

// Each step is mocked into a tiny harness that exposes the props it received
// via data attributes + a couple of buttons that invoke the callbacks. This
// isolates the orchestrator from the real step internals (mutations, i18n,
// validation) so the tests only assert wiring + transitions.

vi.mock("./DatabaseStep", () => ({
  default: (props: { onNext: () => void }) => (
    <div data-testid="database-step">
      <button type="button" onClick={props.onNext}>
        database-next
      </button>
    </div>
  ),
}));

vi.mock("./AccountStep", () => ({
  default: (props: {
    onNext: (data: { username: string; password: string }) => void;
    initialData?: { username: string; password: string };
  }) => (
    <div data-testid="account-step" data-initial={JSON.stringify(props.initialData ?? null)}>
      <button
        type="button"
        onClick={() => props.onNext({ username: "alice", password: "12345678" })}
      >
        account-next
      </button>
    </div>
  ),
}));

vi.mock("./LibrariesStep", () => ({
  default: (props: {
    onNext: (data: Array<{ name: string; contentType: string; path: string }>) => void;
    onBack: () => void;
    initialData?: Array<{ name: string; contentType: string; path: string }>;
  }) => (
    <div data-testid="libraries-step" data-initial={JSON.stringify(props.initialData ?? null)}>
      <button
        type="button"
        onClick={() =>
          props.onNext([{ name: "Movies", contentType: "movies", path: "/m" }])
        }
      >
        libraries-next
      </button>
      <button type="button" onClick={props.onBack}>
        libraries-back
      </button>
    </div>
  ),
}));

vi.mock("./SettingsStep", () => ({
  default: (props: {
    onNext: (data: { tmdbApiKey?: string; hwAccel?: string }) => void;
    onBack: () => void;
    initialData?: { tmdbApiKey?: string; hwAccel?: string };
  }) => (
    <div data-testid="settings-step" data-initial={JSON.stringify(props.initialData ?? null)}>
      <button
        type="button"
        onClick={() => props.onNext({ tmdbApiKey: "tmdb-key", hwAccel: "vaapi" })}
      >
        settings-next
      </button>
      <button type="button" onClick={props.onBack}>
        settings-back
      </button>
    </div>
  ),
}));

vi.mock("./CompleteStep", () => ({
  default: (props: { setupData: unknown }) => (
    <div data-testid="complete-step" data-setup={JSON.stringify(props.setupData)} />
  ),
}));

describe("SetupWizard", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("starts on the database step by default", () => {
    render(<SetupWizard />);
    expect(screen.getByTestId("database-step")).toBeInTheDocument();
    expect(screen.queryByTestId("account-step")).toBeNull();
  });

  it("respects the initialStep prop when it matches a known key", () => {
    render(<SetupWizard initialStep="settings" />);
    expect(screen.getByTestId("settings-step")).toBeInTheDocument();
    expect(screen.queryByTestId("account-step")).toBeNull();
  });

  it("falls back to step 0 (database) for an unknown initialStep value", () => {
    render(<SetupWizard initialStep="not-a-real-step" />);
    expect(screen.getByTestId("database-step")).toBeInTheDocument();
  });

  it("advances database → account → libraries → settings → complete and persists data along the way", () => {
    render(<SetupWizard />);

    // Database step — clicking next moves on without persisting any
    // wizard-level state (the database step lives entirely on the
    // server through /setup/db).
    fireEvent.click(screen.getByText("database-next"));
    expect(screen.getByTestId("account-step")).toBeInTheDocument();

    // Account → Libraries.
    fireEvent.click(screen.getByText("account-next"));
    expect(screen.getByTestId("libraries-step")).toBeInTheDocument();

    // Libraries → Settings.
    fireEvent.click(screen.getByText("libraries-next"));
    expect(screen.getByTestId("settings-step")).toBeInTheDocument();

    // Settings → Complete. The complete step receives the full setupData
    // accumulated across the previous steps.
    fireEvent.click(screen.getByText("settings-next"));
    const complete = screen.getByTestId("complete-step");
    expect(complete).toBeInTheDocument();

    const setupData = JSON.parse(complete.getAttribute("data-setup") ?? "{}");
    expect(setupData.user).toEqual({ username: "alice", password: "12345678" });
    expect(setupData.libraries).toEqual([
      { name: "Movies", contentType: "movies", path: "/m" },
    ]);
    expect(setupData.settings).toEqual({ tmdbApiKey: "tmdb-key", hwAccel: "vaapi" });
  });

  it("goBack from libraries returns to account and re-hydrates the saved user data", () => {
    render(<SetupWizard initialStep="account" />);

    fireEvent.click(screen.getByText("account-next"));
    fireEvent.click(screen.getByText("libraries-back"));

    const account = screen.getByTestId("account-step");
    expect(account).toBeInTheDocument();
    const initial = JSON.parse(account.getAttribute("data-initial") ?? "null");
    expect(initial).toMatchObject({ username: "alice", password: "12345678" });
  });

  it("goBack stops at step 0 (cannot go below the first step)", () => {
    render(<SetupWizard initialStep="account" />);
    fireEvent.click(screen.getByText("account-next"));
    fireEvent.click(screen.getByText("libraries-back")); // back to account
    expect(screen.getByTestId("account-step")).toBeInTheDocument();
  });

  it("step indicator reflects the active step (4th circle highlighted on settings)", () => {
    render(<SetupWizard initialStep="settings" />);
    // The wizard now has 5 steps (database, account, libraries, settings,
    // complete). Settings is slot 3 (zero-indexed) so steps 1-3 render
    // as completed (check svg, no digit), step 4 is the active digit,
    // step 5 is pending.
    const numberedCircles = screen
      .getAllByText(/^[1-5]$/)
      .filter((el) => el.tagName === "DIV");
    const visibleDigits = numberedCircles.map((el) => el.textContent);
    expect(visibleDigits).toEqual(["4", "5"]);
  });
});
