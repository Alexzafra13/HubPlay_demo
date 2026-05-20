// LiveNowRail — "En directo ahora" mini-rail.
//
// Unlike the catalog rails, the card here represents a CHANNEL plus
// its currently airing program (joined server-side from EPG), not a
// MediaItem. Layout is a wider-than-tall tile dominated by the
// channel logo with a single-line "now" caption underneath.
//
// When the EPG payload includes program_start / program_end the
// card overlays a thin progress bar at the bottom of the thumbnail
// and switches the subtitle to a "Queda 42min" countdown — same
// language Plex uses on its Live TV rail. The countdown re-renders
// every 30 s via useNowTick so it stays accurate without a polling
// query.
//
// Clicking the card lands in the Live TV page focused on that
// channel — that's the usual continuation, since the header is just
// a teaser.

import { Link } from "react-router";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { useHomeLiveNow } from "@/api/hooks";
import { useNowTick } from "@/hooks/useNowTick";
import type { HomeLiveNowChannel } from "@/api/types";
import { Skeleton } from "@/components/common";
import { ChannelLogo } from "@/components/livetv/ChannelLogo";
import { HomeRail } from "./HomeRail";

export function LiveNowRail() {
  const { t } = useTranslation();
  const { data, isLoading, isError } = useHomeLiveNow();

  if (isError) return null;

  if (isLoading) {
    return (
      <HomeRail title={t("home.liveNow", { defaultValue: "En directo ahora" })}>
        {Array.from({ length: 5 }, (_, i) => (
          <div key={`live-skeleton-${i}`} className="w-[320px] md:w-[360px] lg:w-[400px] xl:w-[440px] shrink-0">
            <Skeleton variant="rectangular" className="aspect-video w-full rounded-md" />
            <Skeleton variant="text" width="70%" className="mt-2" />
          </div>
        ))}
      </HomeRail>
    );
  }

  const channels = data ?? [];
  if (channels.length === 0) return null;

  return (
    <HomeRail
      title={t("home.liveNow", { defaultValue: "En directo ahora" })}
      linkTo="/live-tv"
    >
      {channels.map((ch) => (
        <LiveNowCard key={ch.channel_id} channel={ch} />
      ))}
    </HomeRail>
  );
}

interface LiveNowCardProps {
  channel: HomeLiveNowChannel;
}

function LiveNowCard({ channel }: LiveNowCardProps) {
  const { t } = useTranslation();
  // 30 s tick is enough granularity for "Queda 42min" — paying for a
  // per-second re-render across N rail cards isn't worth the polish.
  const now = useNowTick();
  // Link to the Live TV surface with the channel pre-selected.
  // LiveTV.tsx accepts ?channel=<id> and focuses that channel on mount.
  const href = `/live-tv?channel=${encodeURIComponent(channel.channel_id)}`;

  const epg = computeEpgState(channel, now);

  return (
    <Link
      to={href}
      className="group flex-shrink-0 w-[320px] md:w-[360px] lg:w-[400px] xl:w-[440px] flex flex-col gap-2"
    >
      <div className="relative aspect-video overflow-hidden rounded-[--radius-md] bg-bg-elevated flex items-center justify-center">
        {/* ChannelLogo paints the deterministic colored tile + initials
            underneath and overlays the upstream logo image on top when
            it loads cleanly, swapping back to the tile on <img> error.
            Same widget the LiveTV browser uses, so a card on this home
            rail matches its sibling in /live-tv pixel-for-pixel for the
            same channel. The square wrapper at ~70% height matches the
            old design's logo footprint, so existing real logos keep
            their breathing-room around the edge of the rail card. */}
        <ChannelLogo
          logoUrl={channel.channel_logo ?? null}
          initials={channel.logo_initials}
          bg={channel.logo_bg}
          fg={channel.logo_fg}
          name={channel.channel_name}
          className="aspect-square h-[70%] rounded-md transition-transform duration-300 group-hover:scale-105"
          textClassName="text-3xl font-extrabold tracking-wide"
        />

        {/* Live pill */}
        <div className="absolute top-2 left-2 flex items-center gap-1 rounded-full bg-live/90 px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider text-white">
          <span className="size-1.5 rounded-full bg-white animate-pulse" />
          {t("home.live", { defaultValue: "En directo" })}
        </div>

        {/* Hover tint */}
        <div className="absolute inset-0 bg-black/0 group-hover:bg-black/20 transition-colors duration-200" />

        {/* EPG progress bar — only renders when we have a program
            window. Sits flush against the bottom edge so it reads as
            a "what's left of this show" sliver, matching Plex. */}
        {epg && (
          <div className="absolute bottom-0 left-0 right-0 h-1 bg-black/50">
            <div
              className="h-full bg-warning"
              style={{ width: `${epg.progress}%` }}
            />
          </div>
        )}
      </div>

      <div className="flex flex-col gap-0.5 px-0.5">
        <p className="text-sm font-medium text-text-primary truncate group-hover:text-white transition-colors">
          {channel.channel_name}
        </p>
        <div className="flex items-center gap-2 text-xs text-text-muted truncate">
          {channel.program_title ? (
            <span className="truncate">{channel.program_title}</span>
          ) : (
            <span className="truncate">
              {t("home.noProgramInfo", { defaultValue: "Sin información de programa" })}
            </span>
          )}
          {epg && (
            <>
              <span className="text-text-muted/40" aria-hidden>·</span>
              <span className="shrink-0 text-text-muted/80">
                {formatRemaining(epg.remainingMs, t)}
              </span>
            </>
          )}
        </div>
      </div>
    </Link>
  );
}

interface EpgState {
  progress: number;
  remainingMs: number;
}

// computeEpgState collapses program_start/program_end + the current
// epoch ms into the two values the card needs. Returns null when:
//   - either timestamp is missing (channel has no EPG join, or the
//     program straddled an EPG-prune window);
//   - the dates fail to parse;
//   - the program window is degenerate (end <= start) or already in
//     the past.
// Returning null lets the card silently fall back to the no-EPG
// layout instead of showing a 0%/100% bar that lies about state.
function computeEpgState(channel: HomeLiveNowChannel, now: number): EpgState | null {
  if (!channel.program_start || !channel.program_end) return null;
  const start = Date.parse(channel.program_start);
  const end = Date.parse(channel.program_end);
  if (Number.isNaN(start) || Number.isNaN(end)) return null;
  const duration = end - start;
  if (duration <= 0) return null;
  const remainingMs = end - now;
  if (remainingMs <= 0) return null;
  const elapsed = now - start;
  const progress = Math.min(100, Math.max(0, (elapsed / duration) * 100));
  return { progress, remainingMs };
}

// formatRemaining picks the granularity the user actually cares
// about: seconds when the program is about to end, plain minutes
// for the common case, hours+minutes when there's more than an
// hour left. Mirrors Plex's "1h 12min left" / "Queda 8min" format.
function formatRemaining(remainingMs: number, t: TFunction): string {
  const totalSeconds = Math.max(0, Math.round(remainingMs / 1000));
  if (totalSeconds < 60) {
    return t("home.remainingSeconds", {
      seconds: totalSeconds,
      defaultValue: `Queda ${totalSeconds}s`,
    });
  }
  const totalMinutes = Math.floor(totalSeconds / 60);
  if (totalMinutes < 60) {
    return t("home.remainingMinutes", {
      minutes: totalMinutes,
      defaultValue: `Queda ${totalMinutes}min`,
    });
  }
  const hours = Math.floor(totalMinutes / 60);
  const minutes = totalMinutes % 60;
  return t("home.remainingHours", {
    hours,
    minutes,
    defaultValue: `Queda ${hours}h ${minutes}min`,
  });
}
