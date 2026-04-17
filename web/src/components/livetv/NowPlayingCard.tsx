import { useTranslation } from "react-i18next";
import type { Channel, EPGProgram } from "@/api/types";
import { ChannelLogo } from "./ChannelLogo";
import { categoryMeta, parseCategory } from "./categoryHelpers";
import { formatTime, getProgramProgress } from "./epgHelpers";

interface NowPlayingCardProps {
  channel: Channel;
  nowPlaying: EPGProgram | null;
  upNext: EPGProgram | null;
}

/**
 * Stand-alone info panel shown *below* the hero player. Keeps channel
 * metadata out of the video element so it never competes with the native
 * video controls (fullscreen, play/pause, volume). Scales from mobile
 * (stacked, compact) to desktop (horizontal, generous).
 */
export function NowPlayingCard({
  channel,
  nowPlaying,
  upNext,
}: NowPlayingCardProps) {
  const { t } = useTranslation();
  const parsed = parseCategory(channel.group);
  const meta = categoryMeta(parsed.primary);
  const progress = nowPlaying ? getProgramProgress(nowPlaying) : 0;

  return (
    <div
      className="relative mx-4 -mt-6 overflow-hidden rounded-2xl border border-white/10 bg-bg-card/90 px-4 py-4 shadow-[0_10px_40px_-10px_rgba(0,0,0,0.8)] backdrop-blur-xl md:mx-6 md:-mt-10 md:px-6 md:py-5"
      aria-live="polite"
      aria-atomic="true"
    >
      {/* subtle accent gradient behind */}
      <div
        className="pointer-events-none absolute inset-0 opacity-50"
        style={{
          background:
            "radial-gradient(ellipse at top left, rgba(13,148,136,0.10), transparent 55%)",
        }}
        aria-hidden="true"
      />

      <div className="relative flex flex-col gap-4 md:flex-row md:items-center md:gap-5">
        {/* Logo + identity */}
        <div className="flex items-start gap-3 md:shrink-0 md:gap-4">
          <div
            className={[
              "flex h-14 w-14 shrink-0 items-center justify-center rounded-xl md:h-20 md:w-20",
              meta.tint,
            ].join(" ")}
          >
            <ChannelLogo
              logoUrl={channel.logo_url}
              number={channel.number}
              name={channel.name}
              sizeClassName="w-10 h-10 md:w-14 md:h-14"
              fallbackTextClassName="text-xl md:text-2xl font-bold"
            />
          </div>

          <div className="min-w-0 flex-1 md:max-w-xs">
            <div className="flex items-center gap-2">
              <span className="flex items-center gap-1 rounded-md bg-live/90 px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wider text-white shadow-sm">
                <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-white" />
                {t("liveTV.live")}
              </span>
              <span className="text-[11px] font-semibold tabular-nums text-text-muted">
                CH.{channel.number}
              </span>
            </div>
            <h1 className="mt-1 truncate text-lg font-bold text-text-primary md:text-2xl">
              {channel.name}
            </h1>
            <div className="mt-1 flex items-center gap-1 text-xs">
              <span
                className={[
                  "inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 font-medium",
                  meta.tint,
                ].join(" ")}
              >
                <span aria-hidden="true">{meta.icon}</span>
                {parsed.primary}
              </span>
            </div>
          </div>
        </div>

        {/* Programme info */}
        <div className="min-w-0 flex-1 border-white/5 md:border-l md:pl-5">
          {nowPlaying ? (
            <>
              <div className="flex items-baseline gap-2">
                <span className="text-[10px] font-semibold uppercase tracking-wider text-accent-light">
                  {t("liveTV.nowPlaying")}
                </span>
                <span className="tabular-nums text-[11px] text-text-muted">
                  {formatTime(nowPlaying.start_time)} —{" "}
                  {formatTime(nowPlaying.end_time)}
                </span>
              </div>
              <p
                className="mt-0.5 truncate text-sm font-semibold text-text-primary md:text-base"
                title={nowPlaying.title}
              >
                {nowPlaying.title}
              </p>
              {nowPlaying.description && (
                <p
                  className="mt-1 line-clamp-2 text-xs text-text-secondary md:text-sm"
                  title={nowPlaying.description}
                >
                  {nowPlaying.description}
                </p>
              )}
              <div className="mt-2 flex items-center gap-2">
                <div
                  className="h-1.5 flex-1 overflow-hidden rounded-full bg-white/5"
                  role="progressbar"
                  aria-valuemin={0}
                  aria-valuemax={100}
                  aria-valuenow={Math.round(progress)}
                  aria-label={t("liveTV.programProgress")}
                >
                  <div
                    className="h-full rounded-full bg-gradient-to-r from-accent-light to-accent transition-all duration-1000"
                    style={{ width: `${progress}%` }}
                  />
                </div>
                <span className="shrink-0 text-[10px] tabular-nums text-text-muted md:text-xs">
                  {Math.round(progress)}%
                </span>
              </div>
              {upNext && (
                <p className="mt-2 truncate text-[11px] text-text-muted md:text-xs">
                  <span className="font-medium text-text-secondary">
                    {t("liveTV.upNext")}
                  </span>{" "}
                  · {upNext.title}{" "}
                  <span className="text-text-muted/60">
                    {t("liveTV.at")} {formatTime(upNext.start_time)}
                  </span>
                </p>
              )}
            </>
          ) : (
            <p className="text-sm italic text-text-muted">
              {t("liveTV.noProgram")}
            </p>
          )}
        </div>
      </div>
    </div>
  );
}
