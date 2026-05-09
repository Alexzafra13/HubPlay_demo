// SkipSegmentButton — covers the contract that drives whether the
// floating "Saltar X" button appears at any given currentTime:
//
//   - pickActiveSegment is exhaustively pinned (the rendering layer
//     is just a thin wrapper around it; if pickActiveSegment is
//     right the rest follows).
//   - Confidence floor (0.7) suppresses low-quality detector output.
//   - The tail-trim window prevents a "button appears for the last
//     half-second" jitter.
//   - Outros are gated on nextUpAvailable so movies don't surface a
//     useless "Saltar créditos" button.
//   - Render path: the button click calls onSkip with the segment's
//     end_seconds (so the caller knows where to seek).

import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import "@/i18n";

import { SkipSegmentButton } from "./SkipSegmentButton";
import { pickActiveSegment } from "./segmentLogic";
import type { EpisodeSegment } from "@/api/types";

const intro: EpisodeSegment = {
  kind: "intro",
  source: "chapter",
  start_seconds: 45,
  end_seconds: 135,
  confidence: 0.95,
};

const outro: EpisodeSegment = {
  kind: "outro",
  source: "chapter",
  start_seconds: 1700,
  end_seconds: 1800,
  confidence: 0.95,
};

const recap: EpisodeSegment = {
  kind: "recap",
  source: "chapter",
  start_seconds: 0,
  end_seconds: 45,
  confidence: 0.95,
};

describe("pickActiveSegment", () => {
  it("returns null when there are no segments", () => {
    expect(pickActiveSegment(undefined, 100, true)).toBeNull();
    expect(pickActiveSegment([], 100, true)).toBeNull();
  });

  it("returns the segment containing currentTime", () => {
    expect(pickActiveSegment([intro, outro, recap], 60, true)).toBe(intro);
    expect(pickActiveSegment([intro, outro, recap], 10, true)).toBe(recap);
    expect(pickActiveSegment([intro, outro, recap], 1750, true)).toBe(outro);
  });

  it("returns null outside any segment", () => {
    expect(pickActiveSegment([intro, outro, recap], 200, true)).toBeNull();
    expect(pickActiveSegment([intro, outro, recap], 1900, true)).toBeNull();
  });

  it("trims the tail to avoid flicker in the last half-second", () => {
    // intro ends at 135. With TAIL_TRIM=0.5 the active window
    // closes at 134.5 — we should NOT match at 134.7.
    expect(pickActiveSegment([intro], 134.7, true)).toBeNull();
    expect(pickActiveSegment([intro], 134.4, true)).toBe(intro);
  });

  it("filters out segments below the confidence floor", () => {
    const lowConf: EpisodeSegment = {
      ...intro,
      confidence: 0.5,
    };
    expect(pickActiveSegment([lowConf], 60, true)).toBeNull();
  });

  it("suppresses outros when nextUpAvailable is false", () => {
    expect(pickActiveSegment([outro], 1750, false)).toBeNull();
    expect(pickActiveSegment([outro], 1750, true)).toBe(outro);
  });
});

describe("SkipSegmentButton render", () => {
  it("renders nothing when no segment is active", () => {
    const { container } = render(
      <SkipSegmentButton
        segments={[intro]}
        currentTime={500}
        onSkip={vi.fn()}
      />,
    );
    expect(container.firstChild).toBeNull();
  });

  it("renders the localized label for the active segment kind", () => {
    render(
      <SkipSegmentButton
        segments={[intro]}
        currentTime={60}
        onSkip={vi.fn()}
      />,
    );
    // Bilingual matcher — jsdom defaults to en-US so the test
    // environment renders English copy by default.
    expect(
      screen.getByRole("button", {
        name: /saltar intro|skip intro/i,
      }),
    ).toBeInTheDocument();
  });

  it("invokes onSkip with the segment end when clicked", () => {
    const onSkip = vi.fn();
    render(
      <SkipSegmentButton
        segments={[intro]}
        currentTime={60}
        onSkip={onSkip}
      />,
    );
    fireEvent.click(
      screen.getByRole("button", {
        name: /saltar intro|skip intro/i,
      }),
    );
    expect(onSkip).toHaveBeenCalledWith(intro.end_seconds);
  });

  it("does not render the outro button when no nextUp is available", () => {
    const { container } = render(
      <SkipSegmentButton
        segments={[outro]}
        currentTime={1750}
        onSkip={vi.fn()}
        nextUpAvailable={false}
      />,
    );
    expect(container.firstChild).toBeNull();
  });
});
