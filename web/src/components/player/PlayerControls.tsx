import { memo, useRef, useState } from "react";
import type { FC } from "react";
import { useTranslation } from "react-i18next";
import { TimeDisplay } from "./TimeDisplay";
import type { TrickplayManifest } from "@/hooks/useTrickplay";
import {
  AudioIcon,
  BackIcon,
  ExitFullscreenIcon,
  FullscreenIcon,
  LargePauseIcon,
  LargePlayIcon,
  PauseIcon,
  PlayIcon,
  QualityIcon,
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

// Icons live in `./icons.tsx`; audio enrichment helpers in
// `./audioTracks.ts`. Kept out of this file so it stays a pure
// presentation component (composition + props mapping).

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

// channelLabel + codecLabel + enrichAudioTracks live in
// `./audioTracks.ts` so the logic is unit-testable without rendering
// React. PlayerControls just imports `enrichAudioTracks` and lets the
// helper own the (codec × channel) → label mapping.

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
  audioStreams,
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

  // Enrich the audio picker labels with codec + channel info from the
  // DB-side stream list (bare hls.js names are just "English" /
  // "Spanish"; the user can't tell a stereo AAC from a 7.1 TrueHD
  // sibling without it). Match by language because hls.js doesn't
  // expose the original file's stream index — and within a language
  // we match by position so two Spanish tracks (DTS-MA, AAC) map
  // 1↔1 instead of both showing the same enriched label.
  const enrichedAudioTracks = audioStreams && audioStreams.length > 0
    ? enrichAudioTracks(audioTracks, audioStreams)
    : audioTracks;
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
            tracks={enrichedAudioTracks}
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
