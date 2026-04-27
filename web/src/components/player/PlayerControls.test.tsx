import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
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

describe("PlayerControls — quality selector", () => {
  it("hides the quality button when there is only one level", () => {
    render(
      <PlayerControls
        {...baseProps}
        qualityLevels={[{ id: 0, height: 1080, bitrate: 5_000_000, label: "1080p" }]}
        currentQuality={-1}
        onQualityChange={vi.fn()}
      />,
    );
    expect(screen.queryByLabelText(/quality|calidad/i)).toBeNull();
  });

  it("hides the quality button when no onQualityChange handler is provided (legacy callers)", () => {
    render(
      <PlayerControls
        {...baseProps}
        qualityLevels={[
          { id: 0, height: 720, bitrate: 2_000_000, label: "720p" },
          { id: 1, height: 1080, bitrate: 5_000_000, label: "1080p" },
        ]}
      />,
    );
    expect(screen.queryByLabelText(/quality|calidad/i)).toBeNull();
  });

  it("shows the quality selector with Auto + ladder rungs when multi-level", () => {
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
    expect(screen.getByLabelText(/quality|calidad/i)).toBeInTheDocument();
    // Dropdown items are pre-rendered (CSS-only hide); check labels exist.
    expect(screen.getByText(/auto/i)).toBeInTheDocument();
    expect(screen.getByText("480p")).toBeInTheDocument();
    expect(screen.getByText("720p")).toBeInTheDocument();
    expect(screen.getByText("1080p")).toBeInTheDocument();
  });
});
