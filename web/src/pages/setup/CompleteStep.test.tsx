import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import "@/i18n";
import CompleteStep from "./CompleteStep";

// ─── Mocks ───────────────────────────────────────────────────────────────────

const mutateMock = vi.fn();
const navigateMock = vi.fn();

vi.mock("react-router", () => ({
  useNavigate: () => navigateMock,
}));

vi.mock("@/api/hooks", () => ({
  useSetupComplete: () => ({
    mutate: mutateMock,
    isPending: false,
    isError: false,
    isSuccess: false,
  }),
}));

// ─── Helpers ────────────────────────────────────────────────────────────────

function finish() {
  fireEvent.click(screen.getByRole("button", { name: /Finish Setup/i }));
}

describe("CompleteStep", () => {
  beforeEach(() => {
    mutateMock.mockReset();
    navigateMock.mockReset();
  });

  it("summarises the admin username when user data is present", () => {
    render(
      <CompleteStep
        setupData={{
          user: { username: "alice", password: "secret" },
        }}
      />,
    );
    expect(screen.getByText("alice")).toBeInTheDocument();
  });

  it("lists every library configured, with its name and path", () => {
    render(
      <CompleteStep
        setupData={{
          libraries: [
            { name: "Movies", contentType: "movies", path: "/mnt/movies" },
            { name: "Shows", contentType: "shows", path: "/mnt/shows" },
          ],
        }}
      />,
    );
    expect(screen.getByText("Movies")).toBeInTheDocument();
    expect(screen.getByText("/mnt/movies")).toBeInTheDocument();
    expect(screen.getByText("Shows")).toBeInTheDocument();
    expect(screen.getByText("/mnt/shows")).toBeInTheDocument();
  });

  it("shows 'none added' when no libraries were configured (and hides the scan checkbox)", () => {
    render(<CompleteStep setupData={{}} />);
    expect(screen.getByText(/None added/i)).toBeInTheDocument();
    expect(screen.queryByRole("checkbox")).toBeNull();
  });

  it("shows the scan checkbox, checked by default, when there are libraries", () => {
    render(
      <CompleteStep
        setupData={{
          libraries: [{ name: "M", contentType: "movies", path: "/m" }],
        }}
      />,
    );
    const checkbox = screen.getByRole("checkbox");
    expect(checkbox).toBeChecked();
  });

  it("clicking Finish fires the complete mutation with the scan flag", () => {
    render(
      <CompleteStep
        setupData={{
          libraries: [{ name: "M", contentType: "movies", path: "/m" }],
        }}
      />,
    );
    finish();
    expect(mutateMock).toHaveBeenCalledTimes(1);
    // First arg is the scan-on-finish flag (true by default when libraries exist).
    expect(mutateMock.mock.calls[0][0]).toBe(true);
  });

  it("unchecking the scan box forwards false to the mutation", () => {
    render(
      <CompleteStep
        setupData={{
          libraries: [{ name: "M", contentType: "movies", path: "/m" }],
        }}
      />,
    );
    fireEvent.click(screen.getByRole("checkbox"));
    finish();
    expect(mutateMock.mock.calls[0][0]).toBe(false);
  });

  it("navigates to / on successful finish", () => {
    render(<CompleteStep setupData={{}} />);
    finish();
    const [, handlers] = mutateMock.mock.calls[0];
    handlers.onSuccess();
    expect(navigateMock).toHaveBeenCalledWith("/");
  });

  it("surfaces the server error when the mutation fails", async () => {
    render(<CompleteStep setupData={{}} />);
    finish();
    const [, handlers] = mutateMock.mock.calls[0];
    handlers.onError(new Error("DB is locked"));
    expect(await screen.findByText("DB is locked")).toBeInTheDocument();
    // Critically, we do NOT navigate on failure.
    expect(navigateMock).not.toHaveBeenCalled();
  });

  it("displays the hw_accel mode in uppercase when the user picked one", () => {
    render(
      <CompleteStep
        setupData={{ settings: { hwAccel: "vaapi" } }}
      />,
    );
    // The copy includes the accel name in uppercase.
    expect(screen.getByText(/VAAPI/)).toBeInTheDocument();
  });

  it("without hw_accel settings: shows the software default message", () => {
    render(<CompleteStep setupData={{}} />);
    expect(
      screen.getByText(/Software encoding \(default\)/i),
    ).toBeInTheDocument();
  });
});
