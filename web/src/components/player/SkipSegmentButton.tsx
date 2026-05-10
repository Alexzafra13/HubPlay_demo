import { useEffect } from "react";
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
// Visual treatment: pill-shaped, accent ring, gentle pulse on first
// appearance to draw the eye without distracting on every frame.
// Plex / Netflix do the same thing — the first second of visibility
// uses a slightly bolder shadow + scale so the user notices, then
// settles to a static look.
//
// Keyboard: pressing 'S' while the button is active triggers it,
// matching the convention every major streamer uses for skip.
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

  // Keyboard shortcut. Listener is mounted only while a segment is
  // active so it doesn't intercept 'S' (or 'Shift') presses for the
  // rest of the player session. Ignored when focus is on a text
  // input — the player has none today, but covers the future case
  // of a comments/notes overlay.
  useEffect(() => {
    if (!active) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "s" && e.key !== "S") return;
      const tag = (e.target as HTMLElement | null)?.tagName ?? "";
      if (tag === "INPUT" || tag === "TEXTAREA") return;
      e.preventDefault();
      onSkip(active.end_seconds);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [active, onSkip]);

  if (!active) return null;

  const label = labelFor(active.kind, t);

  return (
    <button
      type="button"
      onClick={() => onSkip(active.end_seconds)}
      // CSS animation: 600ms slide-up + fade-in on mount, then a
      // single subtle pulse via Tailwind's animate-pulse-once
      // (defined inline below — Tailwind's built-in animate-pulse
      // loops forever, which we don't want).
      className={[
        "skip-segment-btn",
        "absolute bottom-24 right-6 z-30",
        "rounded-full border-2 border-white/40 bg-black/75 px-5 py-2.5",
        "text-sm font-semibold text-white shadow-lg backdrop-blur-sm",
        "transition-all duration-150",
        "hover:bg-white hover:text-black hover:border-white",
        "hover:scale-105",
        "focus-visible:outline-2 focus-visible:outline-accent",
      ].join(" ")}
      aria-label={`${label} (S)`}
      title={`${label} — S`}
    >
      <span className="inline-flex items-center gap-2">
        {label}
        <kbd className="hidden sm:inline-flex h-5 min-w-5 items-center justify-center rounded border border-white/30 bg-white/10 px-1 text-[10px] font-mono">
          S
        </kbd>
      </span>
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
