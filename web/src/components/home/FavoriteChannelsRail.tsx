// FavoriteChannelsRail — "Canales favoritos" rail on Home.
//
// Los canales que el usuario marcó con la estrella, como rail propio en
// Home (antes sólo asomaban mezclados dentro de "En directo ahora", que
// además va capado a 5 y es determinista — de ahí la sensación de "salen
// siempre los mismos"). Aquí son su contenido más personal: logo + nombre,
// clic → /live-tv con el canal preseleccionado. El header enlaza al filtro
// de favoritos (?fav=1) como "Ver todo".
//
// Se auto-oculta si el usuario no tiene favoritos (o no hay IPTV) — un
// rail ausente es mejor que uno vacío, igual que el resto de Home.

import { Link } from "react-router";
import { useTranslation } from "react-i18next";
import { useChannelFavorites } from "@/api/hooks/channels";
import type { Channel } from "@/api/types";
import { Skeleton } from "@/components/common";
import { ChannelLogo } from "@/components/livetv/ChannelLogo";
import { HomeRail } from "./HomeRail";

const RAIL_ITEM = "w-[160px] md:w-[180px] lg:w-[200px] shrink-0";

export function FavoriteChannelsRail() {
  const { t } = useTranslation();
  const { data, isLoading, isError } = useChannelFavorites();

  if (isError) return null;

  const title = t("home.favoriteChannels", {
    defaultValue: "Canales favoritos",
  });

  if (isLoading) {
    return (
      <HomeRail title={title} linkTo="/live-tv?fav=1">
        {Array.from({ length: 6 }, (_, i) => (
          <div key={`fav-channel-skeleton-${i}`} className={RAIL_ITEM}>
            <Skeleton
              variant="rectangular"
              className="aspect-video w-full rounded-[--radius-md]"
            />
            <Skeleton variant="text" width="70%" className="mt-2" />
          </div>
        ))}
      </HomeRail>
    );
  }

  const channels = data ?? [];
  if (channels.length === 0) return null;

  return (
    <HomeRail title={title} linkTo="/live-tv?fav=1">
      {channels.map((ch) => (
        <div key={ch.id} className={RAIL_ITEM}>
          <FavoriteChannelCard channel={ch} />
        </div>
      ))}
    </HomeRail>
  );
}

function FavoriteChannelCard({ channel }: { channel: Channel }) {
  // LiveTV.tsx acepta ?channel=<id> y enfoca ese canal al montar.
  const href = `/live-tv?channel=${encodeURIComponent(channel.id)}`;
  return (
    <Link to={href} className="group flex flex-col gap-2">
      <div className="relative flex aspect-video items-center justify-center overflow-hidden rounded-[--radius-md] bg-bg-elevated ring-1 ring-border-subtle/50 transition-colors group-hover:ring-accent/40">
        <ChannelLogo
          logoUrl={channel.logo_url}
          initials={channel.logo_initials}
          bg={channel.logo_bg}
          fg={channel.logo_fg}
          name={channel.name}
          className="aspect-square h-[70%] rounded-md transition-transform duration-300 group-hover:scale-105"
          textClassName="text-2xl font-extrabold tracking-wide"
        />
        <div className="absolute inset-0 bg-black/0 transition-colors duration-200 group-hover:bg-black/20" />
      </div>
      <p className="truncate px-0.5 text-sm font-medium text-text-primary transition-colors group-hover:text-white">
        {channel.name}
      </p>
    </Link>
  );
}
