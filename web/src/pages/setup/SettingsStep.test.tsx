import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import "@/i18n";
import SettingsStep from "./SettingsStep";

// ─── Mocks ───────────────────────────────────────────────────────────────────

const capabilitiesState = {
  data: { ffmpeg_path: "", ffmpeg_found: false, hw_accels: [] as string[] },
  isLoading: false,
  isError: false,
  isSuccess: true,
};

const mutateMock = vi.fn();

vi.mock("@/api/hooks", () => ({
  useSystemCapabilities: () => capabilitiesState,
  useSetupSettings: () => ({
    mutate: mutateMock,
    isPending: false,
    isError: false,
    isSuccess: false,
  }),
}));

// ─── Helpers ────────────────────────────────────────────────────────────────

function saveAndContinue() {
  fireEvent.click(screen.getByRole("button", { name: /^Save & Continue$/ }));
}

function resetCapabilities() {
  capabilitiesState.data = {
    ffmpeg_path: "",
    ffmpeg_found: false,
    hw_accels: [],
  };
  capabilitiesState.isLoading = false;
  capabilitiesState.isError = false;
  capabilitiesState.isSuccess = true;
}

describe("SettingsStep", () => {
  beforeEach(() => {
    mutateMock.mockReset();
    resetCapabilities();
  });

  it("with no fields filled: skips the mutation and advances immediately", () => {
    const onNext = vi.fn();
    render(<SettingsStep onNext={onNext} onBack={vi.fn()} />);

    saveAndContinue();

    expect(mutateMock).not.toHaveBeenCalled();
    expect(onNext).toHaveBeenCalledWith({
      tmdbApiKey: undefined,
      hwAccel: undefined,
    });
  });

  it("Skip button: always calls onNext with an empty object, never mutates", () => {
    const onNext = vi.fn();
    render(<SettingsStep onNext={onNext} onBack={vi.fn()} />);

    fireEvent.change(screen.getByLabelText(/TMDb API Key/i), {
      target: { value: "should-be-ignored" },
    });
    fireEvent.click(screen.getByRole("button", { name: /^Skip$/ }));

    expect(onNext).toHaveBeenCalledWith({});
    expect(mutateMock).not.toHaveBeenCalled();
  });

  it("sends tmdb_api_key to the mutation when the user fills it", () => {
    render(<SettingsStep onNext={vi.fn()} onBack={vi.fn()} />);

    fireEvent.change(screen.getByLabelText(/TMDb API Key/i), {
      target: { value: "  my-key  " },
    });
    saveAndContinue();

    expect(mutateMock).toHaveBeenCalledTimes(1);
    const [payload] = mutateMock.mock.calls[0];
    expect(payload).toEqual({ tmdb_api_key: "my-key" });
  });

  it("on successful mutation: calls onNext with the normalized settings", () => {
    const onNext = vi.fn();
    render(<SettingsStep onNext={onNext} onBack={vi.fn()} />);

    fireEvent.change(screen.getByLabelText(/TMDb API Key/i), {
      target: { value: "my-key" },
    });
    saveAndContinue();

    const [, handlers] = mutateMock.mock.calls[0];
    handlers.onSuccess();

    expect(onNext).toHaveBeenCalledWith({
      tmdbApiKey: "my-key",
      hwAccel: undefined,
    });
  });

  it("on mutation error: surfaces the message", async () => {
    render(<SettingsStep onNext={vi.fn()} onBack={vi.fn()} />);

    fireEvent.change(screen.getByLabelText(/TMDb API Key/i), {
      target: { value: "k" },
    });
    saveAndContinue();

    const [, handlers] = mutateMock.mock.calls[0];
    handlers.onError(new Error("cant write config"));

    expect(await screen.findByText("cant write config")).toBeInTheDocument();
  });

  it("Back button calls onBack", () => {
    const onBack = vi.fn();
    render(<SettingsStep onNext={vi.fn()} onBack={onBack} />);
    fireEvent.click(screen.getByRole("button", { name: /^Back$/ }));
    expect(onBack).toHaveBeenCalledTimes(1);
  });

  it("renders ffmpeg-missing warning when capabilities report ffmpeg_found=false", () => {
    render(<SettingsStep onNext={vi.fn()} onBack={vi.fn()} />);
    // "Not found" badge + missing copy appear.
    expect(screen.getByText(/Missing/i)).toBeInTheDocument();
  });

  it("shows hw_accel radio options when ffmpeg is found and the backend lists accels", () => {
    capabilitiesState.data = {
      ffmpeg_path: "/usr/bin/ffmpeg",
      ffmpeg_found: true,
      hw_accels: ["vaapi", "nvenc"],
    };

    render(<SettingsStep onNext={vi.fn()} onBack={vi.fn()} />);
    // Software (default) + the two backends = 3 radios.
    const radios = screen.getAllByRole("radio");
    expect(radios).toHaveLength(3);
    // Software radio is selected by default (hwAccel = "").
    expect(radios[0]).toBeChecked();
  });

  it("submits the selected hw_accel alongside the API call payload", () => {
    capabilitiesState.data = {
      ffmpeg_path: "/usr/bin/ffmpeg",
      ffmpeg_found: true,
      hw_accels: ["vaapi", "nvenc"],
    };

    render(<SettingsStep onNext={vi.fn()} onBack={vi.fn()} />);
    // Pick VAAPI (second radio).
    fireEvent.click(screen.getAllByRole("radio")[1]);
    saveAndContinue();

    expect(mutateMock).toHaveBeenCalledTimes(1);
    const [payload] = mutateMock.mock.calls[0];
    expect(payload).toEqual({ hw_accel: "vaapi" });
  });

  it("hydrates from initialData", () => {
    render(
      <SettingsStep
        onNext={vi.fn()}
        onBack={vi.fn()}
        initialData={{ tmdbApiKey: "restored", hwAccel: "" }}
      />,
    );
    expect(screen.getByLabelText(/TMDb API Key/i)).toHaveValue("restored");
  });
});
