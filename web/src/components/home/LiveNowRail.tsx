// LiveNowRail — "En directo ahora" mini-rail.
//
// Unlike the catalog rails, the card here represents a CHANNEL plus
// its currently airing program (joined server-side from EPG), not a
// MediaItem. Layout is a wider-than-tall tile dominated by the
// channel logo with a single-line "now" caption underneath.
//
// Clicking the card lands in the Live TV page focused on that
// channel — that's the usual continuation, since the header is just
// a teaser.

import { Link } from "react-router";
import { useTranslation } from "react-i18next";
import { useHomeLiveNow } from "@/api/hooks";
import type { HomeLiveNowChannel } from "@/api/types";
import { Skeleton } from "@/components/common";
import { HomeRail } from "./HomeRail";

export function LiveNowRail() {
  const { t } = useTranslation();
  const { data, isLoading, isError } = useHomeLiveNow();

  if (isError) return null;

  if (isLoading) {
    return (
      <HomeRail title={t("home.liveNow", { defaultValue: "En directo ahora" })}>
        {Array.from({ length: 5 }, (_, i) => (
          <div key={i} className="w-[260px] shrink-0">
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
  // Link to the Live TV surface with the channel pre-selected.
  // LiveTV.tsx accepts ?channel=<id> and focuses that channel on mount.
  const href = `/live-tv?channel=${encodeURIComponent(channel.channel_id)}`;

  return (
    <Link
      to={href}
      className="group flex-shrink-0 w-[260px] flex flex-col gap-2"
    >
      <div className="relative aspect-video overflow-hidden rounded-[--radius-md] bg-bg-elevated flex items-center justify-center">
        {channel.channel_logo ? (
          <img
            src={channel.channel_logo}
            alt={channel.channel_name}
            loading="lazy"
            className="max-h-[70%] max-w-[70%] object-contain transition-transform duration-300 group-hover:scale-105"
          />
        ) : (
          <span className="text-2xl font-bold text-text-muted">
            {channel.channel_name.charAt(0)}
          </span>
        )}

        {/* Live pill */}
        <div className="absolute top-2 left-2 flex items-center gap-1 rounded-full bg-live/90 px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider text-white">
          <span className="h-1.5 w-1.5 rounded-full bg-white animate-pulse" />
          {t("home.live", { defaultValue: "En directo" })}
        </div>

        {/* Hover tint */}
        <div className="absolute inset-0 bg-black/0 group-hover:bg-black/20 transition-colors duration-200" />
      </div>

      <div className="flex flex-col gap-0.5 px-0.5">
        <p className="text-sm font-medium text-text-primary truncate group-hover:text-white transition-colors">
          {channel.channel_name}
        </p>
        <p className="text-xs text-text-muted truncate">
          {channel.program_title ??
            t("home.noProgramInfo", { defaultValue: "Sin información de programa" })}
        </p>
      </div>
    </Link>
  );
}
