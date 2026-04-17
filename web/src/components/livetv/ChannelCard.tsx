import type { Channel, EPGProgram } from "@/api/types";
import { ChannelLogo } from "./ChannelLogo";
import { getProgramProgress } from "./epgHelpers";

interface ChannelCardProps {
  channel: Channel;
  isActive: boolean;
  nowPlaying: EPGProgram | null;
  onClick: () => void;
}

export function ChannelCard({
  channel,
  isActive,
  nowPlaying,
  onClick,
}: ChannelCardProps) {
  const progress = nowPlaying ? getProgramProgress(nowPlaying) : 0;

  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={isActive}
      aria-label={
        nowPlaying
          ? `${channel.name} — now playing ${nowPlaying.title}`
          : channel.name
      }
      className={[
        "group relative flex items-center gap-2.5 rounded-xl p-2.5 transition-all duration-200 text-left w-full overflow-hidden",
        isActive
          ? "bg-accent/10 ring-1 ring-accent/30"
          : "bg-white/[0.03] hover:bg-white/[0.07] focus-visible:bg-white/[0.07] focus-visible:ring-1 focus-visible:ring-accent/40",
      ].join(" ")}
    >
      {/* Logo */}
      <div
        className={[
          "w-10 h-10 md:w-11 md:h-11 rounded-lg flex items-center justify-center shrink-0 relative",
          isActive ? "bg-accent/10" : "bg-white/5",
        ].join(" ")}
      >
        <ChannelLogo
          logoUrl={channel.logo_url}
          number={channel.number}
          name={channel.name}
          sizeClassName="w-7 h-7 md:w-8 md:h-8"
          fallbackTextClassName="text-sm font-bold"
        />
        {isActive && (
          <div className="absolute -top-0.5 -right-0.5 w-2 h-2 rounded-full bg-live animate-pulse" />
        )}
      </div>

      {/* Info */}
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1.5">
          <p className="text-xs md:text-sm font-medium text-text-primary truncate">
            {channel.name}
          </p>
          {isActive && (
            <span className="shrink-0 w-1.5 h-1.5 rounded-full bg-live animate-pulse" />
          )}
        </div>

        {/* Now playing or channel number */}
        {nowPlaying ? (
          <p className="text-[10px] md:text-xs text-text-muted truncate mt-0.5">
            {nowPlaying.title}
          </p>
        ) : (
          <p className="text-[10px] md:text-xs text-text-muted truncate mt-0.5">
            Ch. {channel.number}
            {channel.group ? ` · ${channel.group}` : ""}
          </p>
        )}
      </div>

      {/* EPG progress bar at bottom of card */}
      {nowPlaying && (
        <div
          className="absolute bottom-0 left-0 right-0 h-0.5 bg-white/5"
          role="progressbar"
          aria-valuenow={Math.round(progress)}
          aria-valuemin={0}
          aria-valuemax={100}
          aria-label="Program progress"
        >
          <div
            className={[
              "h-full rounded-r-full transition-all duration-1000",
              isActive ? "bg-accent" : "bg-accent/50",
            ].join(" ")}
            style={{ width: `${progress}%` }}
          />
        </div>
      )}
    </button>
  );
}
