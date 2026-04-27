import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import "@/i18n";
import LibrariesStep from "./LibrariesStep";

// ─── Mocks ───────────────────────────────────────────────────────────────────

const mutateMock = vi.fn();

vi.mock("@/api/hooks", () => ({
  useSetupCreateLibraries: () => ({
    mutate: mutateMock,
    isPending: false,
    isError: false,
    isSuccess: false,
  }),
}));

// FolderBrowser depends on network hooks we don't want to exercise here.
// Replace it with a minimal harness that exposes its three props.
type FolderBrowserProps = {
  isOpen: boolean;
  onClose: () => void;
  onSelect: (path: string) => void;
};
let lastBrowserProps: FolderBrowserProps | null = null;
vi.mock("@/components/setup/FolderBrowser", () => ({
  FolderBrowser: (props: FolderBrowserProps) => {
    lastBrowserProps = props;
    return props.isOpen ? (
      <div data-testid="folder-browser-open">
        <button type="button" onClick={() => props.onSelect("/mnt/media/Movies")}>
          pick-path
        </button>
        <button type="button" onClick={props.onClose}>
          close-browser
        </button>
      </div>
    ) : null;
  },
}));

// ─── Helpers ────────────────────────────────────────────────────────────────

function submit() {
  fireEvent.click(screen.getByRole("button", { name: /^Save & Continue$/ }));
}

describe("LibrariesStep", () => {
  beforeEach(() => {
    mutateMock.mockReset();
    lastBrowserProps = null;
  });

  it("renders a single empty entry by default and disables remove when only one row exists", () => {
    render(<LibrariesStep onNext={vi.fn()} onBack={vi.fn()} />);
    expect(screen.getByLabelText(/Library Name/i)).toHaveValue("");
    // Remove button is only rendered when there are 2+ entries.
    expect(screen.queryByRole("button", { name: /Remove library/ })).toBeNull();
  });

  it("skipping with all-empty entries calls onNext with [] (no mutation)", () => {
    const onNext = vi.fn();
    render(<LibrariesStep onNext={onNext} onBack={vi.fn()} />);

    submit();
    expect(mutateMock).not.toHaveBeenCalled();
    expect(onNext).toHaveBeenCalledWith([]);
  });

  it("Skip button passes [] even with partially-filled rows (and never mutates)", () => {
    const onNext = vi.fn();
    render(<LibrariesStep onNext={onNext} onBack={vi.fn()} />);

    fireEvent.change(screen.getByLabelText(/Library Name/i), {
      target: { value: "Movies" },
    });
    fireEvent.click(screen.getByRole("button", { name: /^Skip$/ }));

    expect(onNext).toHaveBeenCalledWith([]);
    expect(mutateMock).not.toHaveBeenCalled();
  });

  it("shows per-field validation errors when a row has a name but no path", () => {
    const onNext = vi.fn();
    render(<LibrariesStep onNext={onNext} onBack={vi.fn()} />);

    fireEvent.change(screen.getByLabelText(/Library Name/i), {
      target: { value: "Movies" },
    });
    submit();

    expect(screen.getByText(/Path is required/)).toBeInTheDocument();
    expect(mutateMock).not.toHaveBeenCalled();
    expect(onNext).not.toHaveBeenCalled();
  });

  it("submits filled rows through the mutation with content_type mapped to the API shape", () => {
    render(<LibrariesStep onNext={vi.fn()} onBack={vi.fn()} />);

    fireEvent.change(screen.getByLabelText(/Library Name/i), {
      target: { value: "My Movies" },
    });
    fireEvent.change(screen.getByLabelText(/Content Type/i), {
      target: { value: "shows" },
    });
    fireEvent.change(screen.getByLabelText(/Media Path/i), {
      target: { value: "/mnt/shows" },
    });
    submit();

    expect(mutateMock).toHaveBeenCalledTimes(1);
    const [payload] = mutateMock.mock.calls[0];
    expect(payload).toEqual([
      { name: "My Movies", content_type: "shows", paths: ["/mnt/shows"] },
    ]);
  });

  it("on successful mutation: forwards the filled entries to onNext (not the API payload)", () => {
    const onNext = vi.fn();
    render(<LibrariesStep onNext={onNext} onBack={vi.fn()} />);

    fireEvent.change(screen.getByLabelText(/Library Name/i), {
      target: { value: "Movies" },
    });
    fireEvent.change(screen.getByLabelText(/Media Path/i), {
      target: { value: "/mnt/movies" },
    });
    submit();

    const [, handlers] = mutateMock.mock.calls[0];
    handlers.onSuccess();

    expect(onNext).toHaveBeenCalledWith([
      { name: "Movies", contentType: "movies", path: "/mnt/movies" },
    ]);
  });

  it("surfaces server errors when the mutation fails", async () => {
    render(<LibrariesStep onNext={vi.fn()} onBack={vi.fn()} />);

    fireEvent.change(screen.getByLabelText(/Library Name/i), {
      target: { value: "M" },
    });
    fireEvent.change(screen.getByLabelText(/Media Path/i), {
      target: { value: "/p" },
    });
    submit();

    const [, handlers] = mutateMock.mock.calls[0];
    handlers.onError(new Error("disk on fire"));

    expect(await screen.findByText("disk on fire")).toBeInTheDocument();
  });

  it("Add another + Remove library wire up in pairs (add N, remove one, left with N)", () => {
    const { container } = render(
      <LibrariesStep onNext={vi.fn()} onBack={vi.fn()} />,
    );

    const countRows = () =>
      container.querySelectorAll('select[id^="content-type-"]').length;

    expect(countRows()).toBe(1);
    fireEvent.click(screen.getByText(/Add another library/i));
    fireEvent.click(screen.getByText(/Add another library/i));
    expect(countRows()).toBe(3);

    // Remove the middle entry.
    fireEvent.click(screen.getByRole("button", { name: /Remove library 2/i }));
    expect(countRows()).toBe(2);
  });

  it("Back button triggers onBack without validating", () => {
    const onBack = vi.fn();
    render(<LibrariesStep onNext={vi.fn()} onBack={onBack} />);

    // Partially fill to ensure back doesn't block on validation.
    fireEvent.change(screen.getByLabelText(/Library Name/i), {
      target: { value: "Only name" },
    });
    fireEvent.click(screen.getByRole("button", { name: /^Back$/ }));

    expect(onBack).toHaveBeenCalledTimes(1);
    expect(mutateMock).not.toHaveBeenCalled();
  });

  it("FolderBrowser wiring: opens, returns a path, and auto-fills the name when empty", () => {
    render(<LibrariesStep onNext={vi.fn()} onBack={vi.fn()} />);

    // Open the browser for row 0.
    fireEvent.click(screen.getByRole("button", { name: /^Browse$/ }));
    expect(screen.getByTestId("folder-browser-open")).toBeInTheDocument();
    expect(lastBrowserProps?.isOpen).toBe(true);

    // Select a path. Name auto-fills from the last path segment.
    fireEvent.click(screen.getByText("pick-path"));
    expect(screen.getByLabelText(/Media Path/i)).toHaveValue(
      "/mnt/media/Movies",
    );
    expect(screen.getByLabelText(/Library Name/i)).toHaveValue("Movies");
  });

  it("FolderBrowser does NOT overwrite an existing library name", () => {
    render(<LibrariesStep onNext={vi.fn()} onBack={vi.fn()} />);

    fireEvent.change(screen.getByLabelText(/Library Name/i), {
      target: { value: "My Stuff" },
    });
    fireEvent.click(screen.getByRole("button", { name: /^Browse$/ }));
    fireEvent.click(screen.getByText("pick-path"));

    expect(screen.getByLabelText(/Library Name/i)).toHaveValue("My Stuff");
    expect(screen.getByLabelText(/Media Path/i)).toHaveValue(
      "/mnt/media/Movies",
    );
  });

  it("hydrates from initialData when present and non-empty", () => {
    const { container } = render(
      <LibrariesStep
        onNext={vi.fn()}
        onBack={vi.fn()}
        initialData={[
          { name: "A", contentType: "movies", path: "/a" },
          { name: "B", contentType: "livetv", path: "/b" },
        ]}
      />,
    );
    // Two rows with duplicate Input ids — identify them by unique select ids
    // and read the sibling name input from each row.
    const selects = container.querySelectorAll<HTMLSelectElement>(
      'select[id^="content-type-"]',
    );
    expect(selects).toHaveLength(2);
    expect(selects[0].value).toBe("movies");
    expect(selects[1].value).toBe("livetv");

    const nameInputs = container.querySelectorAll<HTMLInputElement>(
      'input[placeholder^="e.g. Movies"]',
    );
    expect(nameInputs).toHaveLength(2);
    expect(nameInputs[0].value).toBe("A");
    expect(nameInputs[1].value).toBe("B");
  });
});
