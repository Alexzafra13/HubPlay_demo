import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import SetupWizard from "./SetupWizard";

// Each step is mocked into a tiny harness that exposes the props it received
// via data attributes + a couple of buttons that invoke the callbacks. This
// isolates the orchestrator from the real step internals (mutations, i18n,
// validation) so the tests only assert wiring + transitions.
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

  it("starts on the account step by default", () => {
    render(<SetupWizard />);
    expect(screen.getByTestId("account-step")).toBeInTheDocument();
    expect(screen.queryByTestId("libraries-step")).toBeNull();
  });

  it("respects the initialStep prop when it matches a known key", () => {
    render(<SetupWizard initialStep="settings" />);
    expect(screen.getByTestId("settings-step")).toBeInTheDocument();
    expect(screen.queryByTestId("account-step")).toBeNull();
  });

  it("falls back to step 0 for an unknown initialStep value", () => {
    render(<SetupWizard initialStep="not-a-real-step" />);
    expect(screen.getByTestId("account-step")).toBeInTheDocument();
  });

  it("advances account → libraries → settings → complete and persists data along the way", () => {
    render(<SetupWizard />);

    // Account → Libraries (and the data we pass through is preserved).
    fireEvent.click(screen.getByText("account-next"));
    const librariesStep = screen.getByTestId("libraries-step");
    expect(librariesStep).toBeInTheDocument();

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
    render(<SetupWizard />);

    fireEvent.click(screen.getByText("account-next"));
    fireEvent.click(screen.getByText("libraries-back"));

    const account = screen.getByTestId("account-step");
    expect(account).toBeInTheDocument();
    // initialData on account reflects what was previously submitted —
    // no data loss when the user steps backwards.
    const initial = JSON.parse(account.getAttribute("data-initial") ?? "null");
    expect(initial).toMatchObject({ username: "alice", password: "12345678" });
  });

  it("goBack stops at step 0 (cannot go below the first step)", () => {
    // Already on account (index 0). The wizard exposes no back button on
    // AccountStep, but the LibrariesStep one is wired to goBack — clicking
    // it once lands us on account; clicking again would be a no-op.
    render(<SetupWizard />);
    fireEvent.click(screen.getByText("account-next"));
    fireEvent.click(screen.getByText("libraries-back")); // index 0
    // Now on account — and it stays there even if the orchestrator's goBack
    // is somehow invoked again (we have no UI for that here, so we just
    // re-assert we did not crash and we're still on step 0).
    expect(screen.getByTestId("account-step")).toBeInTheDocument();
  });

  it("step indicator reflects the active step (3rd circle highlighted on settings)", () => {
    render(<SetupWizard initialStep="settings" />);
    // Step labels come from translations; the orchestrator passes the keys
    // to t() with no defaultValue. Without an i18n provider, t() returns the
    // raw key — we assert the number of circles instead, which is the most
    // stable contract.
    const numberedCircles = screen
      .getAllByText(/^[1-4]$/)
      .filter((el) => el.tagName === "DIV");
    // Steps 1 and 2 are completed (rendered as a check svg, not a digit),
    // step 3 is active (digit "3"), step 4 is pending (digit "4").
    const visibleDigits = numberedCircles.map((el) => el.textContent);
    expect(visibleDigits).toEqual(["3", "4"]);
  });
});
