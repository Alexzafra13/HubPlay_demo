import { useTranslation } from "react-i18next";
import type { Channel, EPGProgram } from "@/api/types";
import { ChannelLogo } from "./ChannelLogo";
import { formatTime, getProgramProgress } from "./epgHelpers";

interface NowNextRowProps {
  channel: Channel;
  nowPlaying?: EPGProgram | null;
  upNext?: EPGProgram | null;
  isFavorite: boolean;
  onClick: () => void;
  onToggleFavorite: () => void;
  /** Dead/unhealthy channel — muted, no live treatment. */
  dimmed?: boolean;
}

/**
 * NowNextRow — a single channel rendered as a "what's on now / next"
 * guide row (Live TV "En directo" surface). Replaces the repetitive
 * 16:9 logo card: the program a channel is airing *right now* is the
 * hero of the row, not a footnote under a logo.
 *
 * Layout: [logo + name/number] · [AHORA block w/ progress] · [DESPUÉS
 * block]. Stacks on mobile, three columns from md up. The whole row is
 * one button (→ onClick); the favorite heart is a nested role="button"
 * span so we don't nest <button> elements.
 */
export function NowNextRow({
  channel,
  nowPlaying,
  upNext,
  isFavorite,
  onClick,
  onToggleFavorite,
  dimmed = false,
}: NowNextRowProps) {
  const { t } = useTranslation();
  const live = !dimmed && !!nowPlaying;
  const progress = nowPlaying ? getProgramProgress(nowPlaying) : 0;

  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={`${channel.name}${nowPlaying ? ` — ${nowPlaying.title}` : ""}`}
      className={[
        "group relative grid w-full grid-cols-1 items-stretch gap-px text-left",
        "transition-colors focus-visible:outline-none",
        "md:grid-cols-[15rem_1.6fr_1fr]",
        live
          ? "hover:bg-[linear-gradient(90deg,var(--tv-accent-soft),transparent_72%)]"
          : "hover:bg-tv-bg-2/50",
        dimmed ? "opacity-55" : "",
      ].join(" ")}
    >
      {/* live accent spine on the left edge */}
      {live ? (
        <span
          aria-hidden="true"
          className="pointer-events-none absolute inset-y-0 left-0 w-[3px] bg-tv-accent opacity-0 transition-opacity group-hover:opacity-100"
        />
      ) : null}

      {/* Channel identity */}
      <div className="flex items-center gap-3 px-4 py-3 md:border-r md:border-tv-line">
        <ChannelLogo
          logoUrl={channel.logo_url}
          initials={channel.logo_initials}
          bg={channel.logo_bg}
          fg={channel.logo_fg}
          name={channel.name}
          className="size-11 flex-none rounded-tv-sm shadow-sm ring-1 ring-black/30"
          textClassName="text-sm font-bold"
        />
        <div className="min-w-0 flex-1">
          <div className="truncate text-[13.5px] font-semibold text-tv-fg-0">
            {channel.name}
          </div>
          <div className="mt-0.5 flex items-center gap-2">
            <span className="font-mono text-[10px] uppercase tracking-widest text-tv-fg-3">
              CH {channel.number}
            </span>
            {channel.quality ? (
              <span className="rounded-tv-xs border border-tv-accent/40 px-1 font-mono text-[9px] font-bold text-tv-accent">
                {channel.quality}
              </span>
            ) : null}
          </div>
        </div>
        <span
          role="button"
          tabIndex={0}
          aria-label={
            isFavorite
              ? t("liveTV.removeFromFavorites", {
                  defaultValue: "Quitar de favoritos",
                })
              : t("liveTV.addToFavorites", {
                  defaultValue: "Añadir a favoritos",
                })
          }
          aria-pressed={isFavorite}
          onClick={(e) => {
            e.stopPropagation();
            onToggleFavorite();
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter" || e.key === " ") {
              e.preventDefault();
              e.stopPropagation();
              onToggleFavorite();
            }
          }}
          className="flex size-7 flex-none cursor-pointer items-center justify-center rounded-full text-tv-fg-3 transition hover:bg-tv-bg-3 hover:text-tv-fg-1"
          style={isFavorite ? { color: "var(--tv-live)" } : undefined}
        >
          <svg viewBox="0 0 24 24" className="size-4" aria-hidden="true">
            <path
              d="M12 21s-7.5-4.7-10-9.3C.6 8.8 2 5.5 5.2 5.5c1.9 0 3.2 1.1 3.8 2.1.6-1 1.9-2.1 3.8-2.1 3.2 0 4.6 3.3 3.2 6.2C19.5 16.3 12 21 12 21z"
              fill={isFavorite ? "currentColor" : "none"}
              stroke="currentColor"
              strokeWidth="1.6"
            />
          </svg>
        </span>
      </div>

      {/* AHORA */}
      <div
        className={[
          "relative flex flex-col justify-center px-4 py-3",
          live ? "bg-[var(--tv-accent-soft)]" : "",
        ].join(" ")}
      >
        <div className="flex items-center gap-2">
          <span
            className={[
              "text-[9.5px] font-semibold uppercase tracking-[0.16em]",
              live ? "text-tv-accent" : "text-tv-fg-3",
            ].join(" ")}
          >
            {t("liveTV.now", { defaultValue: "Ahora" })}
          </span>
          {live ? (
            <span
              aria-hidden="true"
              className="size-1.5 rounded-full"
              style={{
                background: "var(--tv-live)",
                boxShadow: "0 0 6px var(--tv-live)",
              }}
            />
          ) : null}
        </div>
        <div
          className={[
            "mt-0.5 truncate text-sm",
            live ? "font-semibold text-tv-fg-0" : "text-tv-fg-2",
          ].join(" ")}
        >
          {nowPlaying
            ? nowPlaying.title
            : dimmed
              ? t("liveTV.channelOff", {
                  defaultValue: "Canal apagado · reintenta más tarde",
                })
              : t("liveTV.noEPG", { defaultValue: "Sin guía disponible" })}
        </div>
        {nowPlaying ? (
          <div className="mt-1 font-mono text-[10px] tabular-nums text-tv-fg-3">
            {t("liveTV.endsAt", {
              defaultValue: "termina {{time}}",
              time: formatTime(nowPlaying.end_time),
            })}
          </div>
        ) : null}
        {live ? (
          <div className="absolute inset-x-0 bottom-0 h-[3px] bg-tv-accent/20">
            <div
              className="h-full bg-tv-accent"
              style={{ width: `${progress}%` }}
            />
          </div>
        ) : null}
      </div>

      {/* DESPUÉS */}
      <div className="flex flex-col justify-center px-4 py-3 md:border-l md:border-tv-line">
        {upNext ? (
          <>
            <div className="text-[9.5px] font-semibold uppercase tracking-[0.16em] text-tv-fg-3">
              {t("liveTV.upNextShort", { defaultValue: "Después" })}
              <span className="ml-2 font-mono tracking-normal text-tv-fg-2">
                {formatTime(upNext.start_time)}
              </span>
            </div>
            <div className="mt-0.5 truncate text-[13px] text-tv-fg-2">
              {upNext.title}
            </div>
          </>
        ) : (
          <div className="text-xs text-tv-fg-3">
            {t("liveTV.noUpcoming", {
              defaultValue: "Sin más programación.",
            })}
          </div>
        )}
      </div>
    </button>
  );
}
