import { memo, useEffect, useRef, useState } from "react";
import type { FC, ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { TimeDisplay } from "./TimeDisplay";
import type { TrickplayManifest } from "@/hooks/useTrickplay";
import { useIsMobile } from "@/hooks/useIsMobile";
import { BottomSheet, SheetRow, SheetSection } from "./BottomSheet";
import {
  AudioIcon,
  BackIcon,
  ExitFullscreenIcon,
  FullscreenIcon,
  LargePauseIcon,
  LargePlayIcon,
  PauseIcon,
  PlayIcon,
  SettingsIcon,
  SubtitleIcon,
  VolumeHighIcon,
  VolumeLowIcon,
  VolumeMutedIcon,
} from "./icons";
import {
  enrichAudioTracks,
  type AudioStreamInfo,
  type AudioTrack,
} from "./audioTracks";

interface SubtitleTrack {
  id: number;
  name: string;
  lang: string;
  // burnIn marks a subtitle that has to be rendered into the video
  // frames at transcode time (PGS / DVDSUB / ASS) instead of riding
  // as an HLS sub track. The picker decorates these with a hint —
  // "se reinicia el stream" — so the user knows their selection
  // causes a brief reload rather than an instant switch.
  burnIn?: boolean;
}

interface QualityLevel {
  id: number;
  height: number;
  bitrate: number;
  label: string;
}

// One chapter marker on the seek bar. `startSeconds` is duration-in-
// seconds (already converted from ticks at the call site) so SeekBar
// stays unit-agnostic and doesn't need to know about the 10-million-
// ticks-per-second backend convention.
interface ChapterMarker {
  startSeconds: number;
  title: string;
}

interface TrickplayProps {
  manifest: TrickplayManifest;
  spriteURL: string;
}

// Playback-rate ladder. Hard-coded ladder (matches Plex / YouTube)
// because three-quarter / one-and-a-third increments confuse more
// than they help, and exposing arbitrary values via a slider on
// touch is fiddly. Stored as numbers so the parent can multiply
// directly onto `video.playbackRate` without parsing.
const PLAYBACK_RATES: ReadonlyArray<{ value: number; label: string }> = [
  { value: 0.5, label: "0.5×" },
  { value: 0.75, label: "0.75×" },
  { value: 1.0, label: "1×" },
  { value: 1.25, label: "1.25×" },
  { value: 1.5, label: "1.5×" },
  { value: 2.0, label: "2×" },
];

interface PlayerControlsProps {
  isPlaying: boolean;
  currentTime: number;
  duration: number;
  buffered: number;
  volume: number;
  isMuted: boolean;
  isFullscreen: boolean;
  audioTracks: AudioTrack[];
  /**
   * DB-side audio MediaStreams (same item, different source). Cross-
   * referenced with `audioTracks` to enrich each picker entry with
   * codec + channel count ("English · TrueHD 7.1") — the bare
   * hls.js name ("English") hides the difference between a stereo
   * AAC track and the lossless 7.1 sibling on the same release.
   */
  audioStreams?: AudioStreamInfo[];
  subtitleTracks: SubtitleTrack[];
  qualityLevels?: QualityLevel[];
  // Seek bar chapter markers. Optional — when absent or empty the
  // bar renders unchanged. When present, each entry becomes a 2-px
  // tick on the bar; hovering reveals the title.
  chapters?: ChapterMarker[];
  // Trickplay (preview thumbnails). When provided, the SeekBar
  // shows a sub-image of the sprite at the cursor position on
  // hover. Absent = legacy bar (no preview tooltip).
  trickplay?: TrickplayProps;
  currentAudioTrack: number;
  currentSubtitleTrack: number;
  /** -1 = auto / ABR. */
  currentQuality?: number;
  /** Current playback rate (1.0 = normal). */
  playbackRate?: number;
  onPlayPause: () => void;
  onSeek: (time: number) => void;
  onVolumeChange: (volume: number) => void;
  onToggleMute: () => void;
  onToggleFullscreen: () => void;
  onAudioTrackChange: (id: number) => void;
  onSubtitleTrackChange: (id: number) => void;
  onQualityChange?: (id: number) => void;
  /** Called with the new playback rate (e.g. 1.5). Renders the
   *  Velocidad section in the Ajustes sheet when provided. */
  onPlaybackRateChange?: (rate: number) => void;
  /** Called whenever a picker (Audio / Subs / Ajustes) opens or
   *  closes. The parent uses this to pin the controls-visible state
   *  while a sheet is up, so the 3-second auto-hide timer can't
   *  evict the sheet's containing overlay mid-interaction. */
  onMenuOpenChange?: (anyOpen: boolean) => void;
  /** Optional: when provided, renders a "search online subs" action
   *  inside the subtitle picker. The parent owns the modal and the
   *  resulting `<track>` injection. */
  onSearchExternalSubs?: () => void;
  onClose: () => void;
  title?: string;
  /**
   * Optional title-treatment logo URL. Rendered as an image in the
   * top bar in place of the plain text title when present — keeps
   * visual continuity with the hero / detail surfaces the user just
   * came from. The text title is the fallback.
   */
  logoUrl?: string;
  /**
   * Active playback method — "direct_play" / "direct_stream" /
   * "transcode". Drives the colour tint on the Ajustes button (the
   * gear icon picks up green for "free" paths or amber for transcode
   * so the operator sees the session state without opening anything).
   * Inside the Ajustes sheet the method is shown as a banner above
   * the Calidad section.
   */
  playbackMethod?: "direct_play" | "direct_stream" | "transcode";
  /**
   * Optional resolution / quality label appended next to the method
   * inside the Ajustes sheet banner (e.g. "1080p"). Hidden for
   * direct_play / direct_stream because those don't pick a profile.
   */
  transcodeProfileLabel?: string;
}

// Icons live in `./icons.tsx`; audio enrichment helpers in
// `./audioTracks.ts`. Kept out of this file so it stays a pure
// presentation component (composition + props mapping).

// ─── Click-away helper ──────────────────────────────────────────────────────

// useClickAway: closes a popover when the user mousedowns outside the
// referenced element. Bound at document level so a click on the
// video / backdrop / a sibling control all dismiss the popover. The
// caller passes a ref + a setter and (optionally) an `enabled` gate
// to avoid binding listeners when the popover is closed.
function useClickAway(
  ref: React.RefObject<HTMLElement | null>,
  onAway: () => void,
  enabled: boolean,
) {
  useEffect(() => {
    if (!enabled) return;
    const handler = (e: MouseEvent) => {
      const el = ref.current;
      if (!el) return;
      if (!el.contains(e.target as Node)) onAway();
    };
    // mousedown (not click) so a fresh click on another menu button
    // closes us BEFORE that button's click toggles its own open state.
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [ref, onAway, enabled]);
}

// ─── Seek bar ────────────────────────────────────────────────────────────────

const SeekBar: FC<{
  currentTime: number;
  duration: number;
  buffered: number;
  chapters?: ChapterMarker[];
  trickplay?: TrickplayProps;
  onSeek: (time: number) => void;
}> = memo(({ currentTime, duration, buffered, chapters, trickplay, onSeek }) => {
  const { t } = useTranslation();

  // While the user is actively dragging or click-positioning the seek
  // input, we ignore `currentTime` from the video element so the thumb
  // doesn't snap back when an in-flight `timeupdate` lands during the
  // 1-2 s window ffmpeg needs to produce the first segment after a
  // restart. The pending value is committed exactly once on pointerup
  // (or, for keyboard arrows, immediately — see onChange below).
  //
  // This is the Plex/YouTube pattern: mid-drag is a local-only echo,
  // commit happens at end of interaction. Without it React's onChange
  // fires multiple times per drag (one per intermediate value) and
  // each one was forwarding to `video.currentTime = X` → ffmpeg
  // restart → cascade. The doc captured this on 2026-05-07 as the
  // "+366-segment" cadence in server logs for one user click.
  const [dragValue, setDragValue] = useState<number | null>(null);
  const isDraggingRef = useRef(false);

  const displayedTime = dragValue ?? currentTime;
  const progress = duration > 0 ? (displayedTime / duration) * 100 : 0;
  const bufferedPercent = duration > 0 ? (buffered / duration) * 100 : 0;

  // Hover state for the trickplay preview tooltip. The two pieces:
  // the time at the cursor (formatted) and the X position of the
  // tooltip clamped to stay inside the bar. We track them on the
  // container `<div>`'s mouse events and render an absolutely-
  // positioned preview above the bar when both are populated.
  const trackRef = useRef<HTMLDivElement | null>(null);
  const [hoverTime, setHoverTime] = useState<number | null>(null);
  const [hoverX, setHoverX] = useState(0);
  const [trackWidth, setTrackWidth] = useState(0);

  // Chapter-tick hover state, separate from the trickplay-cursor
  // tracking above. Only the index is needed because the marker
  // owns its own X position via the absolute `left` percentage.
  const [hoveredChapter, setHoveredChapter] = useState<number | null>(null);

  const handleMouseMove = (e: React.MouseEvent<HTMLDivElement>) => {
    if (!trickplay || duration <= 0) return;
    const rect = trackRef.current?.getBoundingClientRect();
    if (!rect || rect.width <= 0) return;
    const ratio = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
    setHoverTime(ratio * duration);
    setHoverX(e.clientX - rect.left);
    setTrackWidth(rect.width);
  };

  const handleMouseLeave = () => {
    setHoverTime(null);
    setHoveredChapter(null);
  };

  // Commit the pending drag value. Capturing logic here so pointerup
  // and pointercancel both end the interaction cleanly.
  const commitPending = () => {
    isDraggingRef.current = false;
    setDragValue((current) => {
      if (current != null) {
        onSeek(current);
      }
      return null;
    });
  };

  return (
    <div
      ref={trackRef}
      className="group/seek relative flex-1 flex items-center h-6 cursor-pointer"
      onMouseMove={handleMouseMove}
      onMouseLeave={handleMouseLeave}
    >
      {trickplay && hoverTime != null && (
        <TrickplayTooltip
          manifest={trickplay.manifest}
          spriteURL={trickplay.spriteURL}
          time={hoverTime}
          cursorX={hoverX}
          trackWidth={trackWidth}
        />
      )}

      <input
        type="range"
        min={0}
        max={duration || 1}
        step={0.1}
        value={displayedTime}
        onPointerDown={() => {
          isDraggingRef.current = true;
        }}
        onPointerUp={commitPending}
        onPointerCancel={commitPending}
        onChange={(e) => {
          const v = Number(e.target.value);
          if (isDraggingRef.current) {
            setDragValue(v);
          } else {
            onSeek(v);
          }
        }}
        className="absolute inset-0 w-full h-full opacity-0 cursor-pointer z-10"
        aria-label={t("playerControls.seek")}
      />
      <div className="relative w-full h-1 group-hover/seek:h-1.5 bg-white/20 rounded-full transition-all duration-150">
        <div
          className="absolute inset-y-0 left-0 bg-white/30 rounded-full"
          style={{ width: `${bufferedPercent}%` }}
        />
        <div
          className="absolute inset-y-0 left-0 bg-accent rounded-full"
          style={{ width: `${progress}%` }}
        />
        {duration > 0 && chapters?.map((c, i) => {
          if (c.startSeconds <= 0 || c.startSeconds >= duration) return null;
          const left = (c.startSeconds / duration) * 100;
          return (
            <div
              key={i}
              className="absolute top-1/2 -translate-y-1/2 -translate-x-1/2 h-3 w-1 cursor-pointer pointer-events-auto"
              style={{ left: `${left}%` }}
              onMouseEnter={() => setHoveredChapter(i)}
              onMouseLeave={() => setHoveredChapter((cur) => (cur === i ? null : cur))}
              aria-label={c.title || `Chapter ${i + 1}`}
            >
              <div
                className={[
                  "absolute left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2 h-2 w-0.5 transition-all duration-150",
                  hoveredChapter === i
                    ? "bg-white h-3 w-[3px]"
                    : "bg-white/80",
                ].join(" ")}
              />
            </div>
          );
        })}
        {duration > 0 && hoveredChapter != null && chapters?.[hoveredChapter] && (
          <ChapterTooltip
            title={chapters[hoveredChapter].title || `Chapter ${hoveredChapter + 1}`}
            time={chapters[hoveredChapter].startSeconds}
            leftPercent={(chapters[hoveredChapter].startSeconds / duration) * 100}
          />
        )}
        <div
          className="absolute top-1/2 -translate-y-1/2 -translate-x-1/2 h-3 w-3 bg-accent rounded-full opacity-0 group-hover/seek:opacity-100 transition-opacity duration-150"
          style={{ left: `${progress}%` }}
        />
      </div>
    </div>
  );
});

SeekBar.displayName = "SeekBar";

// ─── Trickplay tooltip ─────────────────────────────────────────────────────

const TrickplayTooltip: FC<{
  manifest: TrickplayManifest;
  spriteURL: string;
  time: number;
  cursorX: number;
  trackWidth: number;
}> = ({ manifest, spriteURL, time, cursorX, trackWidth }) => {
  const idx = Math.min(
    manifest.total - 1,
    Math.max(0, Math.floor(time / Math.max(1, manifest.interval_sec))),
  );
  const col = idx % manifest.columns;
  const row = Math.floor(idx / manifest.columns);
  const tw = manifest.thumb_width;
  const th = manifest.thumb_height;

  const half = tw / 2;
  let left = cursorX - half;
  if (left < 8) left = 8;
  if (left + tw > trackWidth - 8) left = trackWidth - tw - 8;

  return (
    <div
      className="absolute bottom-full mb-3 pointer-events-none flex flex-col items-center"
      style={{ left, width: tw }}
      aria-hidden="true"
    >
      <div
        className="rounded-[--radius-md] border border-border shadow-lg shadow-black/50 overflow-hidden bg-black"
        style={{
          width: tw,
          height: th,
          backgroundImage: `url(${spriteURL})`,
          backgroundPosition: `-${col * tw}px -${row * th}px`,
          backgroundSize: `${manifest.columns * tw}px ${manifest.rows * th}px`,
          backgroundRepeat: "no-repeat",
        }}
      />
      <span className="mt-1 px-1.5 py-0.5 rounded bg-black/80 text-[11px] font-medium text-white tabular-nums">
        {formatHMS(time)}
      </span>
    </div>
  );
};

const ChapterTooltip: FC<{
  title: string;
  time: number;
  leftPercent: number;
}> = ({ title, time, leftPercent }) => (
  <div
    className="absolute bottom-full mb-3 pointer-events-none -translate-x-1/2 z-20"
    style={{
      left: `clamp(70px, ${leftPercent}%, calc(100% - 70px))`,
    }}
    aria-hidden="true"
  >
    <div className="flex flex-col items-center gap-0.5 rounded-md border border-border/70 bg-black/85 px-2.5 py-1.5 shadow-lg shadow-black/50 backdrop-blur-sm">
      <span className="text-xs font-semibold text-white whitespace-nowrap max-w-[220px] truncate">
        {title}
      </span>
      <span className="text-[10px] font-medium text-white/70 tabular-nums">
        {formatHMS(time)}
      </span>
    </div>
  </div>
);

function formatHMS(s: number): string {
  if (!isFinite(s) || s < 0) return "0:00";
  const total = Math.floor(s);
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const sec = total % 60;
  const pad = (n: number) => n.toString().padStart(2, "0");
  return h > 0 ? `${h}:${pad(m)}:${pad(sec)}` : `${m}:${pad(sec)}`;
}

// ─── Track button (click-to-open, mobile sheet / desktop popover) ────────────

interface TrackOption {
  id: number;
  label: string;
  sublabel?: string;
}

interface TrackButtonProps {
  /** Aria label + sheet/popover title. */
  label: string;
  /** Active method tint. Optional — when set, the button tints
   *  background/border to surface direct-play vs transcode at a glance
   *  (used by the Ajustes button). */
  tint?: "success" | "warning";
  icon: FC;
  options: TrackOption[];
  currentId: number;
  /** When set, the picker renders a leading "off" / "auto" row mapped
   *  to id = -1. */
  offLabel?: string;
  onSelect: (id: number) => void;
  /** Optional extra row appended at the end of the list (used by the
   *  Subtitles picker to surface "Search online"). */
  extra?: ReactNode;
  /** When true, the button is disabled (e.g. quality picker with one
   *  rung). The wrapping caller is responsible for the conditional. */
  disabled?: boolean;
  /** Optional text label rendered next to the icon — used on the
   *  Settings button (desktop) to show "1080p" / "Auto" etc. at a
   *  glance. Hidden on mobile via responsive class to save space. */
  textLabel?: string;
  /** Reports open/close to the parent so it can pin the controls
   *  overlay visible while the picker is up. */
  onOpenChange?: (open: boolean) => void;
}

const TrackButton: FC<TrackButtonProps> = ({
  label,
  tint,
  icon: Icon,
  options,
  currentId,
  offLabel,
  onSelect,
  extra,
  disabled,
  textLabel,
  onOpenChange,
}) => {
  const [open, setOpenState] = useState(false);
  const setOpen = (next: boolean | ((v: boolean) => boolean)) => {
    setOpenState((v) => {
      const resolved = typeof next === "function" ? next(v) : next;
      onOpenChange?.(resolved);
      return resolved;
    });
  };
  const isMobile = useIsMobile();
  const wrapperRef = useRef<HTMLDivElement | null>(null);
  useClickAway(wrapperRef, () => setOpen(false), open && !isMobile);

  const tintClass = (() => {
    if (!tint) return "text-white/80 hover:text-white hover:bg-white/10";
    if (tint === "success") {
      return "text-success hover:text-success hover:bg-success/15 ring-1 ring-success/40";
    }
    return "text-warning hover:text-warning hover:bg-warning/15 ring-1 ring-warning/40";
  })();

  const handleSelect = (id: number) => {
    onSelect(id);
    setOpen(false);
  };

  const Btn = (
    <button
      type="button"
      onClick={(e) => {
        e.stopPropagation();
        if (!disabled) setOpen((v) => !v);
      }}
      disabled={disabled}
      className={[
        // Bigger hit target on mobile (~44 px effective) so the bar
        // doesn't feel like a desktop-port. Desktop keeps the compact
        // 28-px footprint to leave room for the volume slider next to
        // it on hover.
        "inline-flex items-center gap-1.5 rounded-[--radius-sm] p-2 sm:p-1.5 transition-colors cursor-pointer",
        disabled ? "opacity-40 cursor-not-allowed" : tintClass,
      ].join(" ")}
      aria-label={label}
      aria-haspopup="menu"
      aria-expanded={open}
    >
      <Icon />
      {textLabel && (
        <span className="hidden sm:inline text-xs font-semibold tabular-nums">
          {textLabel}
        </span>
      )}
    </button>
  );

  // Mobile: native bottom sheet. Animated, fills width, scrolls.
  if (isMobile) {
    return (
      <div ref={wrapperRef} className="relative">
        {Btn}
        <BottomSheet open={open} title={label} onClose={() => setOpen(false)}>
          <SheetSection>
            {offLabel && (
              <SheetRow
                selected={currentId === -1}
                label={offLabel}
                onClick={() => handleSelect(-1)}
              />
            )}
            {options.map((opt) => (
              <SheetRow
                key={opt.id}
                selected={currentId === opt.id}
                label={opt.label}
                sublabel={opt.sublabel}
                onClick={() => handleSelect(opt.id)}
              />
            ))}
            {extra}
          </SheetSection>
        </BottomSheet>
      </div>
    );
  }

  // Desktop: click-to-open popover (replaces the old hover dropdown
  // so the menu doesn't disappear when the cursor leaves it).
  return (
    <div ref={wrapperRef} className="relative">
      {Btn}
      {open && (
        <div className="absolute bottom-full right-0 mb-2 z-30 min-w-[200px]">
          <div className="bg-bg-card/95 backdrop-blur-md border border-border rounded-[--radius-md] shadow-xl py-1">
            <div className="px-3 py-1.5 text-xs font-medium text-text-muted uppercase tracking-wide">
              {label}
            </div>
            {offLabel && (
              <button
                type="button"
                onClick={() => handleSelect(-1)}
                className={[
                  "w-full text-left px-3 py-1.5 text-sm transition-colors cursor-pointer",
                  currentId === -1
                    ? "text-accent bg-accent/10"
                    : "text-text-primary hover:bg-bg-elevated",
                ].join(" ")}
              >
                {offLabel}
              </button>
            )}
            {options.map((opt) => (
              <button
                key={opt.id}
                type="button"
                onClick={() => handleSelect(opt.id)}
                className={[
                  "w-full text-left px-3 py-1.5 text-sm transition-colors cursor-pointer",
                  currentId === opt.id
                    ? "text-accent bg-accent/10"
                    : "text-text-primary hover:bg-bg-elevated",
                ].join(" ")}
              >
                <div className="truncate">{opt.label}</div>
                {opt.sublabel && (
                  <div className="text-xs text-text-muted truncate">{opt.sublabel}</div>
                )}
              </button>
            ))}
            {extra && <div className="border-t border-border/60 mt-1">{extra}</div>}
          </div>
        </div>
      )}
    </div>
  );
};

// ─── Settings button (Ajustes: Velocidad + Calidad) ──────────────────────────

interface SettingsButtonProps {
  qualityLevels: QualityLevel[];
  currentQuality: number;
  onQualityChange?: (id: number) => void;
  playbackRate: number;
  onPlaybackRateChange?: (rate: number) => void;
  playbackMethod?: "direct_play" | "direct_stream" | "transcode";
  transcodeProfileLabel?: string;
  onOpenChange?: (open: boolean) => void;
}

const SettingsButton: FC<SettingsButtonProps> = ({
  qualityLevels,
  currentQuality,
  onQualityChange,
  playbackRate,
  onPlaybackRateChange,
  playbackMethod,
  transcodeProfileLabel,
  onOpenChange,
}) => {
  const { t } = useTranslation();
  const [open, setOpenState] = useState(false);
  const setOpen = (next: boolean | ((v: boolean) => boolean)) => {
    setOpenState((v) => {
      const resolved = typeof next === "function" ? next(v) : next;
      onOpenChange?.(resolved);
      return resolved;
    });
  };
  const isMobile = useIsMobile();
  const wrapperRef = useRef<HTMLDivElement | null>(null);
  useClickAway(wrapperRef, () => setOpen(false), open && !isMobile);

  // Tint reflects the active playback method. Green = direct path
  // (no CPU cost on the server), amber = transcoding session. The
  // tint replaces the old STREAM-DIRECTO pill in the top bar — same
  // information, fewer UI elements, lives on the button that already
  // owns the quality picker.
  const tint: "success" | "warning" | undefined = (() => {
    if (!playbackMethod) return undefined;
    return playbackMethod === "transcode" ? "warning" : "success";
  })();

  const tintClass = (() => {
    if (!tint) return "text-white/80 hover:text-white hover:bg-white/10";
    if (tint === "success") {
      return "text-success hover:text-success hover:bg-success/15 ring-1 ring-success/40";
    }
    return "text-warning hover:text-warning hover:bg-warning/15 ring-1 ring-warning/40";
  })();

  // Desktop label next to the gear — gives the user a "Plex Convert·1080p"
  // feel without opening anything. Hidden on mobile (no room).
  const methodLabel = (() => {
    if (!playbackMethod) return undefined;
    switch (playbackMethod) {
      case "direct_play":
        return t("playerControls.method.directPlay");
      case "direct_stream":
        return t("playerControls.method.directStream");
      case "transcode":
        return transcodeProfileLabel
          ? t("playerControls.method.transcodeWithProfile", { profile: transcodeProfileLabel })
          : t("playerControls.method.transcode");
    }
  })();

  const hasQualityLadder = qualityLevels.length > 1 && !!onQualityChange;

  const Btn = (
    <button
      type="button"
      onClick={(e) => {
        e.stopPropagation();
        setOpen((v) => !v);
      }}
      className={[
        "inline-flex items-center gap-1.5 rounded-[--radius-sm] p-2 sm:p-1.5 transition-colors cursor-pointer",
        tintClass,
      ].join(" ")}
      aria-label={t("playerControls.settings")}
      aria-haspopup="menu"
      aria-expanded={open}
    >
      <SettingsIcon />
      {methodLabel && (
        <span className="hidden sm:inline text-xs font-semibold uppercase tracking-wider">
          {methodLabel}
        </span>
      )}
    </button>
  );

  const QualitySection = (
    <SheetSection title={t("playerControls.quality")}>
      {hasQualityLadder ? (
        <>
          <SheetRow
            selected={currentQuality === -1}
            label={t("playerControls.qualityAuto")}
            onClick={() => {
              onQualityChange?.(-1);
              setOpen(false);
            }}
          />
          {qualityLevels.map((l) => (
            <SheetRow
              key={l.id}
              selected={currentQuality === l.id}
              label={l.label}
              onClick={() => {
                onQualityChange?.(l.id);
                setOpen(false);
              }}
            />
          ))}
        </>
      ) : (
        <div className="px-3 py-2 text-xs text-text-muted">
          {playbackMethod === "transcode"
            ? t("playerControls.qualitySingleRung")
            : t("playerControls.qualityNotApplicable")}
        </div>
      )}
    </SheetSection>
  );

  const SpeedSection = onPlaybackRateChange ? (
    <SheetSection title={t("playerControls.playbackRate")}>
      {PLAYBACK_RATES.map((r) => (
        <SheetRow
          key={r.value}
          selected={Math.abs(playbackRate - r.value) < 0.01}
          label={r.label}
          onClick={() => {
            onPlaybackRateChange(r.value);
            setOpen(false);
          }}
        />
      ))}
    </SheetSection>
  ) : null;

  if (isMobile) {
    return (
      <div ref={wrapperRef} className="relative">
        {Btn}
        <BottomSheet
          open={open}
          title={t("playerControls.settings")}
          onClose={() => setOpen(false)}
        >
          {methodLabel && (
            <div
              className={[
                "mx-3 my-2 rounded-[--radius-md] border px-3 py-2 text-xs font-medium",
                tint === "warning"
                  ? "border-warning/40 bg-warning/10 text-warning"
                  : "border-success/40 bg-success/10 text-success",
              ].join(" ")}
            >
              {methodLabel}
            </div>
          )}
          {SpeedSection}
          {QualitySection}
        </BottomSheet>
      </div>
    );
  }

  return (
    <div ref={wrapperRef} className="relative">
      {Btn}
      {open && (
        <div className="absolute bottom-full right-0 mb-2 z-30 min-w-[240px]">
          <div className="bg-bg-card/95 backdrop-blur-md border border-border rounded-[--radius-md] shadow-xl py-2 px-1">
            {methodLabel && (
              <div
                className={[
                  "mx-2 mb-2 rounded-[--radius-sm] border px-2.5 py-1.5 text-[11px] font-semibold uppercase tracking-wider",
                  tint === "warning"
                    ? "border-warning/40 bg-warning/10 text-warning"
                    : "border-success/40 bg-success/10 text-success",
                ].join(" ")}
              >
                {methodLabel}
              </div>
            )}
            {SpeedSection}
            {QualitySection}
          </div>
        </div>
      )}
    </div>
  );
};

// ─── Volume control ─────────────────────────────────────────────────────────

// Desktop: hover-reveal slider next to a mute button. Mobile: only
// the mute button — the slider is hidden because mobile users have
// hardware volume keys and crowding the bar costs touch targets.
const VolumeControl: FC<{
  volume: number;
  isMuted: boolean;
  onVolumeChange: (v: number) => void;
  onToggleMute: () => void;
}> = ({ volume, isMuted, onVolumeChange, onToggleMute }) => {
  const { t } = useTranslation();
  const VIcon = isMuted || volume === 0
    ? VolumeMutedIcon
    : volume < 0.5
      ? VolumeLowIcon
      : VolumeHighIcon;

  return (
    <div className="group/vol flex items-center gap-1">
      <button
        type="button"
        onClick={onToggleMute}
        className="p-2 sm:p-1.5 rounded-[--radius-sm] text-white/80 hover:text-white hover:bg-white/10 transition-colors cursor-pointer"
        aria-label={isMuted ? t("playerControls.unmute") : t("playerControls.mute")}
      >
        <VIcon />
      </button>
      {/* Slider hidden on mobile (hardware keys + space constraints). */}
      <div className="hidden sm:block w-0 group-hover/vol:w-20 overflow-hidden transition-all duration-200">
        <input
          type="range"
          min={0}
          max={1}
          step={0.01}
          value={isMuted ? 0 : volume}
          onChange={(e) => onVolumeChange(Number(e.target.value))}
          className="w-20 h-1 accent-accent cursor-pointer"
          aria-label={t("playerControls.volume")}
        />
      </div>
    </div>
  );
};

// ─── Main PlayerControls ─────────────────────────────────────────────────────

const PlayerControls: FC<PlayerControlsProps> = ({
  isPlaying,
  currentTime,
  duration,
  buffered,
  volume,
  isMuted,
  isFullscreen,
  audioTracks,
  audioStreams,
  subtitleTracks,
  qualityLevels = [],
  chapters,
  trickplay,
  currentAudioTrack,
  currentSubtitleTrack,
  currentQuality = -1,
  playbackRate = 1,
  onPlayPause,
  onSeek,
  onVolumeChange,
  onToggleMute,
  onToggleFullscreen,
  onAudioTrackChange,
  onSubtitleTrackChange,
  onQualityChange,
  onPlaybackRateChange,
  onMenuOpenChange,
  onSearchExternalSubs,
  onClose,
  title,
  logoUrl,
  playbackMethod,
  transcodeProfileLabel,
}) => {
  const { t } = useTranslation();

  // Aggregate the open state of the three pickers (Audio, Subs,
  // Settings) into one boolean and bubble it up. The parent uses
  // it to pin the controls overlay so the auto-hide timer can't
  // evict the sheet mid-interaction.
  const openMenusRef = useRef(new Set<string>());
  const reportMenu = (key: string) => (open: boolean) => {
    if (open) openMenusRef.current.add(key);
    else openMenusRef.current.delete(key);
    onMenuOpenChange?.(openMenusRef.current.size > 0);
  };

  // Audio picker labels enriched with codec + channels when the
  // DB-side stream list is available. Match by language + index
  // within language (see audioTracks.ts).
  const enrichedAudioTracks = audioStreams && audioStreams.length > 0
    ? enrichAudioTracks(audioTracks, audioStreams)
    : audioTracks;

  const audioOptions: TrackOption[] = enrichedAudioTracks.map((tr) => ({
    id: tr.id,
    label: tr.name || tr.lang || t("playerControls.trackFallback", { n: tr.id + 1 }),
  }));

  const subtitleOptions: TrackOption[] = subtitleTracks.map((tr) => ({
    id: tr.id,
    label: tr.name || tr.lang || t("playerControls.trackFallback", { n: tr.id + 1 }),
    // Burn-in entries get a small inline tag ("integrado") next to
    // the name so the user can distinguish them from native HLS
    // sub tracks. Defined as a free-text sublabel rather than a
    // structured badge so existing TrackOption consumers don't have
    // to grow a new field — the picker renders sublabel beneath the
    // label exactly the way it already does for audio.
    sublabel: tr.burnIn
      ? t("playerControls.subtitlesBurnInHint", {
          defaultValue: "Integrado · reinicia el stream",
        })
      : undefined,
  }));

  // External-subs row, appended to the subtitle picker. Lives inside
  // the picker (not as its own button on the bar) — Plex pattern. The
  // styling is intentionally distinct from the regular rows so users
  // see it's an action, not a track selection.
  const externalSubsRow = onSearchExternalSubs ? (
    <button
      type="button"
      onClick={() => {
        onSearchExternalSubs();
      }}
      className="w-full flex items-center gap-3 px-3 py-3 mt-1 rounded-[--radius-md] text-left text-sm text-accent hover:bg-accent/10 transition-colors cursor-pointer"
    >
      <svg className="h-4 w-4 shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}>
        <circle cx="11" cy="11" r="7" />
        <path d="M21 21l-4.35-4.35" strokeLinecap="round" />
      </svg>
      <span className="flex-1 font-medium">{t("playerControls.subtitlesExternal")}</span>
    </button>
  ) : null;

  return (
    <div className="absolute inset-0 flex flex-col justify-between z-10">
      {/* Gradient overlays for readability */}
      <div className="absolute inset-x-0 top-0 h-32 bg-gradient-to-b from-black/70 to-transparent pointer-events-none" />
      <div className="absolute inset-x-0 bottom-0 h-40 bg-gradient-to-t from-black/80 to-transparent pointer-events-none" />

      {/* Top bar — back button + brand mark / title.
          Method pill removed in 2026-05-12 redesign: the same info now
          lives on the Ajustes button (gear) below as a colour tint +
          label, removing the redundant chrome up here. */}
      <div className="relative flex items-center gap-3 px-4 pt-4">
        <button
          type="button"
          onClick={onClose}
          className="p-2 rounded-full text-white/80 hover:text-white hover:bg-white/10 transition-colors cursor-pointer"
          aria-label={t("playerControls.back")}
        >
          <BackIcon />
        </button>
        {logoUrl ? (
          <img
            src={logoUrl}
            alt={title ?? ""}
            loading="eager"
            decoding="async"
            className="h-8 sm:h-10 max-w-[60vw] sm:max-w-[40vw] w-auto object-contain object-left drop-shadow-[0_2px_12px_rgba(0,0,0,0.7)]"
          />
        ) : title ? (
          <h2 className="text-sm font-medium text-white/90 truncate">
            {title}
          </h2>
        ) : null}
      </div>

      {/* Center play/pause — bigger hit target on mobile. */}
      <div className="relative flex items-center justify-center">
        <button
          type="button"
          onClick={onPlayPause}
          className="p-4 rounded-full text-white/90 hover:text-white bg-black/30 hover:bg-black/50 backdrop-blur-sm transition-all duration-200 cursor-pointer"
          aria-label={isPlaying ? t("playerControls.pause") : t("playerControls.play")}
        >
          {isPlaying ? <LargePauseIcon /> : <LargePlayIcon />}
        </button>
      </div>

      {/* Bottom bar */}
      <div className="relative flex flex-col gap-2 px-4 pb-4">
        <SeekBar
          currentTime={currentTime}
          duration={duration}
          buffered={buffered}
          chapters={chapters}
          trickplay={trickplay}
          onSeek={onSeek}
        />

        {/* Controls row. Layout (left → right):
            Play · Time · ··· · Audio · Subs · Ajustes (with method
            tint) · Volume · Fullscreen.
            Search-online-subs moved INSIDE the subs picker (no longer
            a top-level button) — same pattern as Plex. */}
        <div className="flex items-center gap-1 sm:gap-2">
          <button
            type="button"
            onClick={onPlayPause}
            className="p-2 sm:p-1.5 rounded-[--radius-sm] text-white/80 hover:text-white hover:bg-white/10 transition-colors cursor-pointer"
            aria-label={isPlaying ? t("playerControls.pause") : t("playerControls.play")}
          >
            {isPlaying ? <PauseIcon /> : <PlayIcon />}
          </button>

          <TimeDisplay currentTime={currentTime} duration={duration} />

          <div className="flex-1" />

          {audioOptions.length > 0 && (
            <TrackButton
              label={t("playerControls.audio")}
              icon={AudioIcon}
              options={audioOptions}
              currentId={currentAudioTrack}
              onSelect={onAudioTrackChange}
              onOpenChange={reportMenu("audio")}
            />
          )}

          <TrackButton
            label={t("playerControls.subtitles")}
            icon={SubtitleIcon}
            options={subtitleOptions}
            currentId={currentSubtitleTrack}
            offLabel={t("playerControls.subtitlesOff")}
            onSelect={onSubtitleTrackChange}
            extra={externalSubsRow}
            onOpenChange={reportMenu("subs")}
          />

          <SettingsButton
            qualityLevels={qualityLevels}
            currentQuality={currentQuality}
            onQualityChange={onQualityChange}
            playbackRate={playbackRate}
            onPlaybackRateChange={onPlaybackRateChange}
            playbackMethod={playbackMethod}
            transcodeProfileLabel={transcodeProfileLabel}
            onOpenChange={reportMenu("settings")}
          />

          <VolumeControl
            volume={volume}
            isMuted={isMuted}
            onVolumeChange={onVolumeChange}
            onToggleMute={onToggleMute}
          />

          <button
            type="button"
            onClick={onToggleFullscreen}
            className="p-2 sm:p-1.5 rounded-[--radius-sm] text-white/80 hover:text-white hover:bg-white/10 transition-colors cursor-pointer"
            aria-label={isFullscreen ? t("playerControls.exitFullscreen") : t("playerControls.fullscreen")}
          >
            {isFullscreen ? <ExitFullscreenIcon /> : <FullscreenIcon />}
          </button>
        </div>
      </div>
    </div>
  );
};

export { PlayerControls };
export type { PlayerControlsProps, AudioTrack, SubtitleTrack };
