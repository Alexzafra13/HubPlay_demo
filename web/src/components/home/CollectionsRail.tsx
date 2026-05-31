// CollectionsRail — "Colecciones" rail on Home.
//
// Surfaces the TMDb sagas the scanner matched in the user's movie
// libraries (Star Wars, MCU, El Señor de los Anillos…). Each tile
// drills into /collections/:id; the rail header links to the full
// /collections index ("Ver todo"). Mismo vocabulario de tarjeta que la
// página /collections (póster 2/3 + badge con el nº de títulos) para que
// se lea como la misma feature, sólo que como rail de descubrimiento.
//
// Se oculta cuando no hay colecciones (catálogo sin sagas matcheadas) o
// ante error — un rail ausente es mejor que uno roto, igual que el resto.

import { useState } from "react";
import { Link } from "react-router";
import { useTranslation } from "react-i18next";
import { useCollections } from "@/api/hooks";
import type { CollectionListEntry } from "@/api/types";
import { Skeleton } from "@/components/common";
import { HomeRail } from "./HomeRail";

const RAIL_ITEM = "w-[140px] md:w-[160px] lg:w-[180px] xl:w-[200px] shrink-0";

export function CollectionsRail() {
  const { t } = useTranslation();
  const { data, isLoading, isError } = useCollections();

  if (isError) return null;

  const title = t("collections.title", { defaultValue: "Colecciones" });

  if (isLoading) {
    return (
      <HomeRail title={title} linkTo="/collections">
        {Array.from({ length: 6 }, (_, i) => (
          <div key={`collections-skeleton-${i}`} className={RAIL_ITEM}>
            <Skeleton
              variant="rectangular"
              className="aspect-[2/3] w-full rounded-[--radius-lg]"
            />
            <Skeleton variant="text" width="80%" className="mt-2" />
          </div>
        ))}
      </HomeRail>
    );
  }

  const entries = data?.collections ?? [];
  if (entries.length === 0) return null;

  return (
    <HomeRail title={title} linkTo="/collections">
      {entries.map((entry) => (
        <div key={entry.id} className={RAIL_ITEM}>
          <CollectionTile entry={entry} />
        </div>
      ))}
    </HomeRail>
  );
}

function CollectionTile({ entry }: { entry: CollectionListEntry }) {
  const [imageFailed, setImageFailed] = useState(false);
  const showPoster = !!entry.poster_url && !imageFailed;
  return (
    <Link
      to={`/collections/${encodeURIComponent(entry.id)}`}
      className="group flex flex-col gap-2 rounded-[--radius-lg] outline-none focus-visible:ring-2 focus-visible:ring-accent"
    >
      <div className="relative aspect-[2/3] overflow-hidden rounded-[--radius-lg] bg-bg-elevated ring-1 ring-border/40 transition-transform duration-300 group-hover:scale-[1.03] group-hover:ring-accent/40">
        {showPoster ? (
          <img
            src={entry.poster_url}
            alt={entry.name}
            loading="lazy"
            decoding="async"
            className="size-full object-cover"
            onError={() => setImageFailed(true)}
          />
        ) : (
          <div className="flex size-full items-center justify-center bg-gradient-to-br from-bg-elevated to-bg-card">
            <span className="text-4xl font-bold text-text-muted">
              {entry.name.charAt(0).toUpperCase()}
            </span>
          </div>
        )}
        <span className="absolute bottom-2 right-2 rounded-full bg-black/70 px-2 py-0.5 text-[11px] font-medium text-white/90">
          {entry.item_count}
        </span>
      </div>
      <p className="line-clamp-1 px-0.5 text-[13px] font-medium text-text-secondary transition-colors group-hover:text-text-primary">
        {entry.name}
      </p>
    </Link>
  );
}
