import { describe, it, expect, vi } from "vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";
import "@/i18n";
import { PlayerControls } from "./PlayerControls";

const baseProps = {
  isPlaying: false,
  currentTime: 0,
  duration: 100,
  buffered: 0,
  volume: 1,
  isMuted: false,
  isFullscreen: false,
  audioTracks: [],
  subtitleTracks: [],
  currentAudioTrack: 0,
  currentSubtitleTrack: -1,
  onPlayPause: vi.fn(),
  onSeek: vi.fn(),
  onVolumeChange: vi.fn(),
  onToggleMute: vi.fn(),
  onToggleFullscreen: vi.fn(),
  onAudioTrackChange: vi.fn(),
  onSubtitleTrackChange: vi.fn(),
  onClose: vi.fn(),
};

// Helper: open a picker by clicking its bar button. Returns the
// rendered dialog (sheet or popover) so the test can scope its
// queries to that surface and not collide with the bar's own
// labels (the bar advertises the same `Audio` / `Subtitles` aria
// labels as the bottom sheet titles).
function openPicker(name: RegExp) {
  fireEvent.click(screen.getByRole("button", { name }));
}

describe("PlayerControls — Ajustes (Settings)", () => {
  it("renders the Ajustes button regardless of quality-ladder availability", () => {
    // The gear is always visible — its sheet contains both Velocidad
    // and Calidad, and Velocidad doesn't depend on the ladder being
    // multi-rung.
    render(<PlayerControls {...baseProps} />);
    expect(screen.getByRole("button", { name: /settings|ajustes/i })).toBeInTheDocument();
  });

  it("paints success tint on the Ajustes button when method is direct_play", () => {
    render(<PlayerControls {...baseProps} playbackMethod="direct_play" />);
    const btn = screen.getByRole("button", { name: /settings|ajustes/i });
    expect(btn.className).toMatch(/text-success/);
  });

  it("paints warning tint on the Ajustes button when method is transcode", () => {
    render(<PlayerControls {...baseProps} playbackMethod="transcode" />);
    const btn = screen.getByRole("button", { name: /settings|ajustes/i });
    expect(btn.className).toMatch(/text-warning/);
  });

  it("shows the quality ladder inside the Ajustes popover when multi-level", () => {
    render(
      <PlayerControls
        {...baseProps}
        qualityLevels={[
          { id: 0, height: 480, bitrate: 1_000_000, label: "480p" },
          { id: 1, height: 720, bitrate: 2_500_000, label: "720p" },
          { id: 2, height: 1080, bitrate: 5_000_000, label: "1080p" },
        ]}
        currentQuality={-1}
        onQualityChange={vi.fn()}
      />,
    );
    openPicker(/settings|ajustes/i);
    // All three rungs land inside the open Ajustes popover.
    expect(screen.getByText("480p")).toBeInTheDocument();
    expect(screen.getByText("720p")).toBeInTheDocument();
    expect(screen.getByText("1080p")).toBeInTheDocument();
    // Auto row exists too.
    expect(screen.getByText(/^auto$/i)).toBeInTheDocument();
  });

  it("renders the playback-rate ladder inside Ajustes when handler is wired", () => {
    render(
      <PlayerControls
        {...baseProps}
        playbackRate={1}
        onPlaybackRateChange={vi.fn()}
      />,
    );
    openPicker(/settings|ajustes/i);
    // The 6-rung ladder is hard-coded; pin the endpoints + the
    // default rung so a regression that drops one is loud.
    expect(screen.getByText("0.5×")).toBeInTheDocument();
    expect(screen.getByText("1×")).toBeInTheDocument();
    expect(screen.getByText("1.5×")).toBeInTheDocument();
    expect(screen.getByText("2×")).toBeInTheDocument();
  });

  it("fires onPlaybackRateChange when a speed row is tapped", () => {
    const onPlaybackRateChange = vi.fn();
    render(
      <PlayerControls
        {...baseProps}
        playbackRate={1}
        onPlaybackRateChange={onPlaybackRateChange}
      />,
    );
    openPicker(/settings|ajustes/i);
    fireEvent.click(screen.getByText("1.5×"));
    expect(onPlaybackRateChange).toHaveBeenCalledWith(1.5);
  });
});

describe("PlayerControls — audio picker", () => {
  it("does NOT show the Audio button when there are no audio tracks", () => {
    // The picker is only worth its space when at least one track
    // exists — direct play of an audio-less source (rare but
    // possible) skips it entirely.
    render(<PlayerControls {...baseProps} audioTracks={[]} />);
    expect(screen.queryByRole("button", { name: /^audio$/i })).toBeNull();
  });

  it("opens the audio sheet/popover and shows enriched labels", () => {
    render(
      <PlayerControls
        {...baseProps}
        audioTracks={[
          { id: 0, name: "English", lang: "eng" },
          { id: 1, name: "Spanish", lang: "spa" },
        ]}
        audioStreams={[
          { index: 1, codec: "truehd", language: "eng", title: null, channels: 8 },
          { index: 2, codec: "aac", language: "spa", title: null, channels: 6 },
        ]}
      />,
    );
    openPicker(/^audio$/i);
    expect(screen.getByText("English · TrueHD 7.1")).toBeInTheDocument();
    expect(screen.getByText("Spanish · AAC 5.1")).toBeInTheDocument();
  });

  it("falls back to the bare label when no DB stream matches the language", () => {
    render(
      <PlayerControls
        {...baseProps}
        audioTracks={[{ id: 0, name: "Director's commentary", lang: "" }]}
        audioStreams={[
          { index: 1, codec: "truehd", language: "eng", title: null, channels: 8 },
        ]}
      />,
    );
    openPicker(/^audio$/i);
    expect(screen.getByText("Director's commentary")).toBeInTheDocument();
    expect(screen.queryByText(/TrueHD/i)).toBeNull();
  });

  it("pairs multiple same-language tracks in file order", () => {
    render(
      <PlayerControls
        {...baseProps}
        audioTracks={[
          { id: 0, name: "Spanish", lang: "spa" },
          { id: 1, name: "Spanish", lang: "spa" },
        ]}
        audioStreams={[
          { index: 1, codec: "dca", language: "spa", title: null, channels: 8 },
          { index: 2, codec: "aac", language: "spa", title: null, channels: 2 },
        ]}
      />,
    );
    openPicker(/^audio$/i);
    expect(screen.getByText("Spanish · DTS-HD 7.1")).toBeInTheDocument();
    expect(screen.getByText("Spanish · AAC Stereo")).toBeInTheDocument();
  });

  it("fires onAudioTrackChange and closes when a row is tapped", () => {
    const onAudioTrackChange = vi.fn();
    render(
      <PlayerControls
        {...baseProps}
        audioTracks={[
          { id: 0, name: "English", lang: "eng" },
          { id: 1, name: "Spanish", lang: "spa" },
        ]}
        onAudioTrackChange={onAudioTrackChange}
      />,
    );
    openPicker(/^audio$/i);
    fireEvent.click(screen.getByText("Spanish"));
    expect(onAudioTrackChange).toHaveBeenCalledWith(1);
  });
});

describe("PlayerControls — subtitles picker", () => {
  it("includes the Off row and surfaces external-subs search inside the picker", () => {
    const onSearch = vi.fn();
    render(
      <PlayerControls
        {...baseProps}
        subtitleTracks={[{ id: 0, name: "English", lang: "eng" }]}
        onSearchExternalSubs={onSearch}
      />,
    );
    openPicker(/subtitles|subtítulos/i);
    expect(screen.getByText(/off|ninguno/i)).toBeInTheDocument();
    // English row is present.
    expect(screen.getByText("English")).toBeInTheDocument();
    // Search-online row lives inside the picker (no longer a sibling
    // button on the bar).
    fireEvent.click(screen.getByText(/search online|buscar.*online/i));
    expect(onSearch).toHaveBeenCalledTimes(1);
  });
});

describe("PlayerControls — menu open reporting", () => {
  it("reports menu open + close to the parent so the controls overlay can be pinned", () => {
    const onMenuOpenChange = vi.fn();
    render(
      <PlayerControls
        {...baseProps}
        audioTracks={[{ id: 0, name: "English", lang: "eng" }]}
        onMenuOpenChange={onMenuOpenChange}
      />,
    );
    // Open audio.
    openPicker(/^audio$/i);
    // Last reported value should be true (any menu open).
    expect(onMenuOpenChange).toHaveBeenLastCalledWith(true);
    // Close by selecting a row.
    fireEvent.click(screen.getByText("English"));
    expect(onMenuOpenChange).toHaveBeenLastCalledWith(false);
  });
});

describe("PlayerControls — top bar", () => {
  it("does not render the legacy STREAM-DIRECTO pill in the top bar", () => {
    // The pill was dropped in 2026-05-12 — the method indicator now
    // lives as a colour tint on the Ajustes button below. Pin so a
    // regression that re-adds the pill is loud.
    const { container } = render(
      <PlayerControls
        {...baseProps}
        title="Bumblebee"
        playbackMethod="direct_play"
      />,
    );
    const topBar = container.querySelector(".pt-4");
    if (topBar) {
      expect(within(topBar as HTMLElement).queryByText(/stream directo|direct stream|direct play/i)).toBeNull();
    }
  });
});
