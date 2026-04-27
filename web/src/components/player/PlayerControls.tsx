import { memo, useRef, useState } from "react";
import type { FC } from "react";
import { useTranslation } from "react-i18next";
import { TimeDisplay } from "./TimeDisplay";
import type { TrickplayManifest } from "@/hooks/useTrickplay";

interface AudioTrack {
  id: number;
  name: string;
  lang: string;
}

interface SubtitleTrack {
  id: number;
  name: string;
  lang: string;
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

interface PlayerControlsProps {
  isPlaying: boolean;
  currentTime: number;
  duration: number;
  buffered: number;
  volume: number;
  isMuted: boolean;
  isFullscreen: boolean;
  audioTracks: AudioTrack[];
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
  onPlayPause: () => void;
  onSeek: (time: number) => void;
  onVolumeChange: (volume: number) => void;
  onToggleMute: () => void;
  onToggleFullscreen: () => void;
  onAudioTrackChange: (id: number) => void;
  onSubtitleTrackChange: (id: number) => void;
  onQualityChange?: (id: number) => void;
  /** Optional: when provided, renders a "search online subs" button
   *  next to the subtitle selector. The parent owns the modal and
   *  the resulting `<track>` injection. */
  onSearchExternalSubs?: () => void;
  onClose: () => void;
  title?: string;
}

// ─── Icon helpers ────────────────────────────────────────────────────────────

function PlayIcon() {
  return (
    <svg className="h-5 w-5" viewBox="0 0 24 24" fill="currentColor">
      <path d="M8 5.14v14l11-7-11-7z" />
    </svg>
  );
}

function PauseIcon() {
  return (
    <svg className="h-5 w-5" viewBox="0 0 24 24" fill="currentColor">
      <path d="M6 4h4v16H6V4zm8 0h4v16h-4V4z" />
    </svg>
  );
}

function LargePlayIcon() {
  return (
    <svg className="h-16 w-16" viewBox="0 0 24 24" fill="currentColor">
      <path d="M8 5.14v14l11-7-11-7z" />
    </svg>
  );
}

function LargePauseIcon() {
  return (
    <svg className="h-16 w-16" viewBox="0 0 24 24" fill="currentColor">
      <path d="M6 4h4v16H6V4zm8 0h4v16h-4V4z" />
    </svg>
  );
}

function VolumeHighIcon() {
  return (
    <svg className="h-5 w-5" viewBox="0 0 24 24" fill="currentColor">
      <path d="M3 9v6h4l5 5V4L7 9H3zm13.5 3c0-1.77-1.02-3.29-2.5-4.03v8.05c1.48-.73 2.5-2.25 2.5-4.02zM14 3.23v2.06c2.89.86 5 3.54 5 6.71s-2.11 5.85-5 6.71v2.06c4.01-.91 7-4.49 7-8.77s-2.99-7.86-7-8.77z" />
    </svg>
  );
}

function VolumeMutedIcon() {
  return (
    <svg className="h-5 w-5" viewBox="0 0 24 24" fill="currentColor">
      <path d="M16.5 12c0-1.77-1.02-3.29-2.5-4.03v2.21l2.45 2.45c.03-.2.05-.41.05-.63zm2.5 0c0 .94-.2 1.82-.54 2.64l1.51 1.51C20.63 14.91 21 13.5 21 12c0-4.28-2.99-7.86-7-8.77v2.06c2.89.86 5 3.54 5 6.71zM4.27 3L3 4.27 7.73 9H3v6h4l5 5v-6.73l4.25 4.25c-.67.52-1.42.93-2.25 1.18v2.06c1.38-.31 2.63-.95 3.69-1.81L19.73 21 21 19.73l-9-9L4.27 3zM12 4L9.91 6.09 12 8.18V4z" />
    </svg>
  );
}

function VolumeLowIcon() {
  return (
    <svg className="h-5 w-5" viewBox="0 0 24 24" fill="currentColor">
      <path d="M7 9v6h4l5 5V4l-5 5H7z" />
    </svg>
  );
}

function FullscreenIcon() {
  return (
    <svg className="h-5 w-5" viewBox="0 0 24 24" fill="currentColor">
      <path d="M7 14H5v5h5v-2H7v-3zm-2-4h2V7h3V5H5v5zm12 7h-3v2h5v-5h-2v3zM14 5v2h3v3h2V5h-5z" />
    </svg>
  );
}

function ExitFullscreenIcon() {
  return (
    <svg className="h-5 w-5" viewBox="0 0 24 24" fill="currentColor">
      <path d="M5 16h3v3h2v-5H5v2zm3-8H5v2h5V5H8v3zm6 11h2v-3h3v-2h-5v5zm2-11V5h-2v5h5V8h-3z" />
    </svg>
  );
}

function BackIcon() {
  return (
    <svg className="h-5 w-5" viewBox="0 0 24 24" fill="currentColor">
      <path d="M20 11H7.83l5.59-5.59L12 4l-8 8 8 8 1.41-1.41L7.83 13H20v-2z" />
    </svg>
  );
}

function AudioIcon() {
  return (
    <svg className="h-4 w-4" viewBox="0 0 24 24" fill="currentColor">
      <path d="M12 3v9.28c-.47-.17-.97-.28-1.5-.28C8.01 12 6 14.01 6 16.5S8.01 21 10.5 21c2.31 0 4.2-1.75 4.45-4H15V6h4V3h-7z" />
    </svg>
  );
}

function SubtitleIcon() {
  return (
    <svg className="h-4 w-4" viewBox="0 0 24 24" fill="currentColor">
      <path d="M20 4H4c-1.1 0-2 .9-2 2v12c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V6c0-1.1-.9-2-2-2zm0 14H4V6h16v12zM6 10h2v2H6v-2zm0 4h8v2H6v-2zm10 0h2v2h-2v-2zm-6-4h8v2h-8v-2z" />
    </svg>
  );
}

function QualityIcon() {
  return (
    <svg className="h-4 w-4" viewBox="0 0 24 24" fill="currentColor">
      <path d="M19 3H5c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h14c1.1 0 2-.9 2-2V5c0-1.1-.9-2-2-2zm-9.46 14.5l-3.04-3.04 1.41-1.41 1.63 1.62 4.13-4.12 1.41 1.41-5.54 5.54z" />
    </svg>
  );
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
  const progress = duration > 0 ? (currentTime / duration) * 100 : 0;
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
  };

  return (
    <div
      ref={trackRef}
      className="group/seek relative flex-1 flex items-center h-6 cursor-pointer"
      onMouseMove={handleMouseMove}
      onMouseLeave={handleMouseLeave}
    >
      {/* Trickplay preview tooltip. Positioned above the bar, clamped
          inside the track width so the right edge of a 320 px thumb
          on a 30 px bar doesn't overflow the player. */}
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
        value={currentTime}
        onChange={(e) => onSeek(Number(e.target.value))}
        className="absolute inset-0 w-full h-full opacity-0 cursor-pointer z-10"
        aria-label={t("playerControls.seek")}
      />
      {/* Track background */}
      <div className="relative w-full h-1 group-hover/seek:h-1.5 bg-white/20 rounded-full transition-all duration-150">
        {/* Buffered */}
        <div
          className="absolute inset-y-0 left-0 bg-white/30 rounded-full"
          style={{ width: `${bufferedPercent}%` }}
        />
        {/* Progress */}
        <div
          className="absolute inset-y-0 left-0 bg-accent rounded-full"
          style={{ width: `${progress}%` }}
        />
        {/* Chapter markers — 2px white ticks at each chapter start.
            Skipping the 0-second marker (no UI value: the bar itself
            starts there) and any marker that lands past the end
            (defensive: a re-encode that shrunk the file shouldn't
            paint ticks off the visible bar). The `<title>` SVG-style
            attribute is honoured by the browser as a hover tooltip. */}
        {duration > 0 && chapters?.map((c, i) => {
          if (c.startSeconds <= 0 || c.startSeconds >= duration) return null;
          const left = (c.startSeconds / duration) * 100;
          return (
            <div
              key={i}
              className="absolute top-1/2 -translate-y-1/2 h-2 w-0.5 bg-white/80 pointer-events-auto"
              style={{ left: `${left}%` }}
              title={c.title || `Chapter ${i + 1}`}
              aria-hidden="true"
            />
          );
        })}
        {/* Thumb */}
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

/**
 * Renders a single thumbnail at hover time, plus a small time label.
 * The math is the inverse of `imaging.GenerateTrickplay`: given a
 * time in seconds, find which sub-image of the sprite covers it and
 * shift `background-position` to that cell.
 *
 * Position rules:
 *   - Centered on cursor X by default.
 *   - Clamped to stay inside the track width so the right/left edges
 *     don't bleed past the player chrome.
 *   - Sits above the track (bottom anchored), with a small gap so
 *     the seek thumb (visible on hover) doesn't overlap the
 *     thumbnail's bottom edge.
 */
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

  // Center on cursor, then clamp so the tooltip box stays inside the
  // track. The 8 px margin is just visual breathing room.
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

function formatHMS(s: number): string {
  if (!isFinite(s) || s < 0) return "0:00";
  const total = Math.floor(s);
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const sec = total % 60;
  const pad = (n: number) => n.toString().padStart(2, "0");
  return h > 0 ? `${h}:${pad(m)}:${pad(sec)}` : `${m}:${pad(sec)}`;
}

// ─── Track selector dropdown ─────────────────────────────────────────────────

const TrackSelector: FC<{
  icon: FC;
  label: string;
  tracks: Array<{ id: number; name: string; lang: string }>;
  currentTrack: number;
  offLabel?: string;
  onSelect: (id: number) => void;
}> = ({ icon: Icon, label, tracks, currentTrack, offLabel, onSelect }) => {
  const { t } = useTranslation();
  if (tracks.length === 0 && !offLabel) return null;
  // The fallback label used when a track has neither name nor lang.
  // The locale provides the "Track N" / "Pista N" shape; the index
  // is interpolated as `n` so each language can word-order it
  // freely.
  const trackLabel = (id: number) => t("playerControls.trackFallback", { n: id + 1 });

  return (
    <div className="relative group/track">
      <button
        className="p-1.5 rounded-[--radius-sm] text-white/80 hover:text-white hover:bg-white/10 transition-colors cursor-pointer"
        aria-label={label}
      >
        <Icon />
      </button>
      <div className="absolute bottom-full right-0 mb-2 hidden group-hover/track:block z-20">
        <div className="bg-bg-card/95 backdrop-blur-md border border-border rounded-[--radius-md] shadow-xl py-1 min-w-[160px]">
          <div className="px-3 py-1.5 text-xs font-medium text-text-muted uppercase tracking-wide">
            {label}
          </div>
          {offLabel && (
            <button
              onClick={() => onSelect(-1)}
              className={[
                "w-full text-left px-3 py-1.5 text-sm transition-colors cursor-pointer",
                currentTrack === -1
                  ? "text-accent bg-accent/10"
                  : "text-text-primary hover:bg-bg-elevated",
              ].join(" ")}
            >
              {offLabel}
            </button>
          )}
          {tracks.map((track) => (
            <button
              key={track.id}
              onClick={() => onSelect(track.id)}
              className={[
                "w-full text-left px-3 py-1.5 text-sm transition-colors cursor-pointer",
                currentTrack === track.id
                  ? "text-accent bg-accent/10"
                  : "text-text-primary hover:bg-bg-elevated",
              ].join(" ")}
            >
              {track.name || track.lang || trackLabel(track.id)}
            </button>
          ))}
        </div>
      </div>
    </div>
  );
};

// ─── Volume slider ───────────────────────────────────────────────────────────

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
        onClick={onToggleMute}
        className="p-1.5 rounded-[--radius-sm] text-white/80 hover:text-white hover:bg-white/10 transition-colors cursor-pointer"
        aria-label={isMuted ? t("playerControls.unmute") : t("playerControls.mute")}
      >
        <VIcon />
      </button>
      <div className="w-0 group-hover/vol:w-20 overflow-hidden transition-all duration-200">
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
  subtitleTracks,
  qualityLevels = [],
  chapters,
  trickplay,
  currentAudioTrack,
  currentSubtitleTrack,
  currentQuality = -1,
  onPlayPause,
  onSeek,
  onVolumeChange,
  onToggleMute,
  onToggleFullscreen,
  onAudioTrackChange,
  onSubtitleTrackChange,
  onQualityChange,
  onSearchExternalSubs,
  onClose,
  title,
}) => {
  const { t } = useTranslation();
  // Quality picker only earns its place when the player has more
  // than one rung to choose from. With a single level the dropdown
  // would be a UI lie ("Auto / 1080p" → both pick the same stream).
  const qualityTracks = qualityLevels.map((l) => ({
    id: l.id,
    name: l.label,
    lang: "",
  }));
  return (
    <div className="absolute inset-0 flex flex-col justify-between z-10">
      {/* Gradient overlays for readability */}
      <div className="absolute inset-x-0 top-0 h-32 bg-gradient-to-b from-black/70 to-transparent pointer-events-none" />
      <div className="absolute inset-x-0 bottom-0 h-40 bg-gradient-to-t from-black/80 to-transparent pointer-events-none" />

      {/* Top bar */}
      <div className="relative flex items-center gap-3 px-4 pt-4">
        <button
          onClick={onClose}
          className="p-2 rounded-full text-white/80 hover:text-white hover:bg-white/10 transition-colors cursor-pointer"
          aria-label={t("playerControls.back")}
        >
          <BackIcon />
        </button>
        {title && (
          <h2 className="text-sm font-medium text-white/90 truncate">
            {title}
          </h2>
        )}
      </div>

      {/* Center play/pause */}
      <div className="relative flex items-center justify-center">
        <button
          onClick={onPlayPause}
          className="p-4 rounded-full text-white/90 hover:text-white bg-black/30 hover:bg-black/50 backdrop-blur-sm transition-all duration-200 cursor-pointer"
          aria-label={isPlaying ? t("playerControls.pause") : t("playerControls.play")}
        >
          {isPlaying ? <LargePauseIcon /> : <LargePlayIcon />}
        </button>
      </div>

      {/* Bottom bar */}
      <div className="relative flex flex-col gap-2 px-4 pb-4">
        {/* Seek bar */}
        <SeekBar
          currentTime={currentTime}
          duration={duration}
          buffered={buffered}
          chapters={chapters}
          trickplay={trickplay}
          onSeek={onSeek}
        />

        {/* Controls row */}
        <div className="flex items-center gap-2">
          {/* Play/Pause small */}
          <button
            onClick={onPlayPause}
            className="p-1.5 rounded-[--radius-sm] text-white/80 hover:text-white hover:bg-white/10 transition-colors cursor-pointer"
            aria-label={isPlaying ? t("playerControls.pause") : t("playerControls.play")}
          >
            {isPlaying ? <PauseIcon /> : <PlayIcon />}
          </button>

          {/* Volume */}
          <VolumeControl
            volume={volume}
            isMuted={isMuted}
            onVolumeChange={onVolumeChange}
            onToggleMute={onToggleMute}
          />

          {/* Time */}
          <TimeDisplay currentTime={currentTime} duration={duration} />

          {/* Spacer */}
          <div className="flex-1" />

          {/* Audio tracks */}
          <TrackSelector
            icon={AudioIcon}
            label={t("playerControls.audio")}
            tracks={audioTracks}
            currentTrack={currentAudioTrack}
            onSelect={onAudioTrackChange}
          />

          {/* Subtitle tracks */}
          <TrackSelector
            icon={SubtitleIcon}
            label={t("playerControls.subtitles")}
            tracks={subtitleTracks}
            currentTrack={currentSubtitleTrack}
            offLabel={t("playerControls.subtitlesOff")}
            onSelect={onSubtitleTrackChange}
          />

          {/* Search online subtitles. Sibling to the subs selector
              rather than nested inside it: opening a modal from a
              hover-revealed dropdown is fragile (the dropdown closes
              the moment focus moves), so the affordance is a
              dedicated button. */}
          {onSearchExternalSubs && (
            <button
              type="button"
              onClick={onSearchExternalSubs}
              aria-label={t("playerControls.subtitlesExternal")}
              title={t("playerControls.subtitlesExternal")}
              className="p-1.5 rounded-[--radius-sm] text-white/80 hover:text-white hover:bg-white/10 transition-colors cursor-pointer"
            >
              <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}>
                <circle cx="11" cy="11" r="7" />
                <path d="M21 21l-4.35-4.35" strokeLinecap="round" />
              </svg>
            </button>
          )}

          {/* Quality (HLS levels only — direct play has no ladder) */}
          {qualityLevels.length > 1 && onQualityChange && (
            <TrackSelector
              icon={QualityIcon}
              label={t("playerControls.quality")}
              tracks={qualityTracks}
              currentTrack={currentQuality}
              offLabel={t("playerControls.qualityAuto")}
              onSelect={onQualityChange}
            />
          )}

          {/* Fullscreen */}
          <button
            onClick={onToggleFullscreen}
            className="p-1.5 rounded-[--radius-sm] text-white/80 hover:text-white hover:bg-white/10 transition-colors cursor-pointer"
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
