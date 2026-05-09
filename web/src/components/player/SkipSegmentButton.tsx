import { useTranslation } from "react-i18next";
import type { EpisodeSegment } from "@/api/types";
import { pickActiveSegment } from "./segmentLogic";

interface SkipSegmentButtonProps {
  segments: EpisodeSegment[] | undefined;
  /** Current playback position in seconds. Caller is responsible
   *  for keeping this in sync with `<video>.currentTime`. */
  currentTime: number;
  /** Called with the segment's `end_seconds` when the user clicks.
   *  The caller seeks the video to this position. */
  onSkip: (toSeconds: number) => void;
  /** Called when there's no next-up info to show after the outro
   *  (i.e. last episode). When `nextUpAvailable` is false, the
   *  outro button is suppressed entirely — there's nowhere to go,
   *  and "skip credits" before a movie ends is rarely the user
   *  intent. */
  nextUpAvailable?: boolean;
}

// SkipSegmentButton — floating "Saltar intro" / "Saltar créditos" /
// "Saltar resumen" button anchored bottom-right of the player. Only
// one segment is active at a time (segments don't overlap by
// design), so we render at most one button.
//
// The component is purely presentational — VideoPlayer owns the
// segment list, the currentTime source, and the seek action. This
// keeps the component testable in isolation without spinning up a
// real <video> element.
export function SkipSegmentButton({
  segments,
  currentTime,
  onSkip,
  nextUpAvailable = true,
}: SkipSegmentButtonProps) {
  const { t } = useTranslation();
  const active = pickActiveSegment(
    segments,
    currentTime,
    nextUpAvailable,
  );

  if (!active) return null;

  const label = labelFor(active.kind, t);

  return (
    <button
      type="button"
      onClick={() => onSkip(active.end_seconds)}
      className={[
        "absolute bottom-24 right-6 z-30",
        "rounded-md border border-white/30 bg-black/70 px-4 py-2",
        "text-sm font-medium text-white shadow-lg backdrop-blur-sm",
        "transition-colors duration-150",
        "hover:bg-black/85 hover:border-white/50",
        "focus-visible:outline-2 focus-visible:outline-accent",
      ].join(" ")}
      aria-label={label}
    >
      {label}
    </button>
  );
}

function labelFor(
  kind: EpisodeSegment["kind"],
  t: ReturnType<typeof useTranslation>["t"],
): string {
  switch (kind) {
    case "intro":
      return t("player.skipIntro", { defaultValue: "Saltar intro" });
    case "outro":
      return t("player.skipCredits", { defaultValue: "Saltar créditos" });
    case "recap":
      return t("player.skipRecap", { defaultValue: "Saltar resumen" });
  }
}
