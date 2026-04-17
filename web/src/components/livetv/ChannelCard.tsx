import type { Channel, EPGProgram } from "@/api/types";
import { useTranslation } from "react-i18next";
import { ChannelLogo } from "./ChannelLogo";
import { categoryMeta, parseCategory } from "./categoryHelpers";
import { formatTime, getProgramProgress } from "./epgHelpers";

interface ChannelCardProps {
  channel: Channel;
  isActive: boolean;
  nowPlaying: EPGProgram | null;
  upNext?: EPGProgram | null;
  onClick: () => void;
  /** Layout variant. `tile` = portrait-ish card (default for grids).
   *  `row` = horizontal list item (for search results, compact lists). */
  variant?: "tile" | "row";
}

export function ChannelCard({
  channel,
  isActive,
  nowPlaying,
  upNext = null,
  onClick,
  variant = "tile",
}: ChannelCardProps) {
  const { t } = useTranslation();
  const parsed = parseCategory(channel.group);
  const meta = categoryMeta(parsed.primary);
  const progress = nowPlaying ? getProgramProgress(nowPlaying) : 0;
  const ariaLabel = nowPlaying
    ? `${channel.name} — ${t("liveTV.nowPlaying")}: ${nowPlaying.title}`
    : channel.name;

  if (variant === "row") {
    return (
      <button
        type="button"
        onClick={onClick}
        aria-pressed={isActive}
        aria-label={ariaLabel}
        className={[
          "group relative flex items-center gap-3 w-full overflow-hidden rounded-xl p-3 text-left transition-all duration-200",
          isActive
            ? "bg-accent/10 ring-1 ring-accent/40"
            : "bg-white/[0.03] hover:bg-white/[0.07] focus-visible:bg-white/[0.07] focus-visible:ring-1 focus-visible:ring-accent/40",
        ].join(" ")}
      >
        <div
          className={[
            "relative flex h-12 w-12 shrink-0 items-center justify-center rounded-lg",
            meta.tint,
          ].join(" ")}
        >
          <ChannelLogo
            logoUrl={channel.logo_url}
            number={channel.number}
            name={channel.name}
            sizeClassName="w-9 h-9"
            fallbackTextClassName="text-sm font-bold"
          />
          {isActive && (
            <span className="absolute -right-0.5 -top-0.5 h-2 w-2 animate-pulse rounded-full bg-live" />
          )}
        </div>

        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <span className="text-[10px] font-semibold uppercase tracking-wider text-text-muted tabular-nums">
              {channel.number.toString().padStart(2, "0")}
            </span>
            <p className="truncate text-sm font-semibold text-text-primary">
              {channel.name}
            </p>
          </div>
          {nowPlaying ? (
            <p className="mt-0.5 truncate text-xs text-text-muted">
              <span className="text-accent-light">●</span> {nowPlaying.title}
            </p>
          ) : (
            <p className="mt-0.5 truncate text-xs text-text-muted">
              {parsed.primary}
            </p>
          )}
        </div>

        {nowPlaying && (
          <div
            className="absolute inset-x-0 bottom-0 h-0.5 bg-white/5"
            role="progressbar"
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={Math.round(progress)}
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

  // "tile" variant: richer card, used in carousel/grid views.
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={isActive}
      aria-label={ariaLabel}
      className={[
        "group relative flex flex-col overflow-hidden rounded-2xl text-left transition-all duration-300",
        "bg-gradient-to-b from-white/[0.04] to-white/[0.01]",
        "border border-white/[0.06] hover:border-white/[0.14]",
        isActive
          ? "ring-2 ring-accent shadow-[0_0_0_3px_rgba(13,148,136,0.18)]"
          : "hover:-translate-y-0.5 hover:shadow-lg hover:shadow-black/40 focus-visible:ring-2 focus-visible:ring-accent/50",
      ].join(" ")}
    >
      {/* ── Logo hero ─────────────────────────────────────────── */}
      <div
        className={[
          "relative flex h-24 items-center justify-center overflow-hidden sm:h-28",
          meta.tint,
        ].join(" ")}
      >
        {/* soft radial glow */}
        <div
          className="pointer-events-none absolute inset-0 opacity-60"
          style={{
            background:
              "radial-gradient(ellipse at center, rgba(255,255,255,0.10) 0%, transparent 60%)",
          }}
          aria-hidden="true"
        />
        <ChannelLogo
          logoUrl={channel.logo_url}
          number={channel.number}
          name={channel.name}
          sizeClassName="relative z-10 w-14 h-14 sm:w-16 sm:h-16 drop-shadow-[0_2px_8px_rgba(0,0,0,0.5)]"
          fallbackTextClassName="text-2xl font-bold relative z-10"
        />

        {/* channel number badge */}
        <span className="absolute left-2 top-2 rounded-md bg-black/40 px-1.5 py-0.5 text-[10px] font-semibold tabular-nums text-white/80 backdrop-blur-sm">
          CH.{channel.number}
        </span>

        {/* LIVE badge if a programme is currently airing */}
        {nowPlaying && (
          <span className="absolute right-2 top-2 flex items-center gap-1 rounded-md bg-live/90 px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wider text-white shadow-md shadow-live/20">
            <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-white" />
            {t("liveTV.live")}
          </span>
        )}

        {/* now-active indicator */}
        {isActive && (
          <span
            className="absolute bottom-2 right-2 flex items-center gap-1 rounded-full bg-accent/90 px-2 py-0.5 text-[10px] font-bold text-white"
            aria-hidden="true"
          >
            <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-white" />
            {t("liveTV.watching")}
          </span>
        )}
      </div>

      {/* ── Info block ────────────────────────────────────────── */}
      <div className="flex flex-1 flex-col gap-1.5 p-3">
        <h3
          className={[
            "truncate text-sm font-semibold leading-tight sm:text-[15px]",
            isActive ? "text-accent" : "text-text-primary",
          ].join(" ")}
          title={channel.name}
        >
          {channel.name}
        </h3>

        <div className="flex items-center gap-1.5 text-[11px]">
          <span
            className={[
              "inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 font-medium",
              meta.tint,
            ].join(" ")}
          >
            <span aria-hidden="true">{meta.icon}</span>
            <span className="truncate max-w-[6.5rem]">{parsed.primary}</span>
          </span>
        </div>

        {nowPlaying ? (
          <div className="mt-1 space-y-1">
            <p className="truncate text-xs text-text-secondary">
              {nowPlaying.title}
            </p>
            <div className="flex items-center gap-2">
              <div
                className="h-1 flex-1 overflow-hidden rounded-full bg-white/5"
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
              <span className="shrink-0 text-[10px] tabular-nums text-text-muted">
                {formatTime(nowPlaying.end_time)}
              </span>
            </div>
            {upNext && (
              <p className="truncate text-[10px] text-text-muted">
                <span className="text-text-muted/70">
                  {t("liveTV.upNext")}:
                </span>{" "}
                {upNext.title}
              </p>
            )}
          </div>
        ) : (
          <p className="mt-1 text-xs italic text-text-muted/70">
            {t("liveTV.noProgram")}
          </p>
        )}
      </div>
    </button>
  );
}
