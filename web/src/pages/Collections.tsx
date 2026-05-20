import { useState } from "react";
import { Link } from "react-router";
import { useTranslation } from "react-i18next";
import { useCollections } from "@/api/hooks";
import { Spinner, EmptyState } from "@/components/common";
import type { CollectionListEntry } from "@/api/types";

// /collections — index page that lists every TMDb saga the scanner
// has matched in the user's movie libraries. Reached either from the
// "View all collections" link in the topbar Movies dropdown or a
// direct URL share. Rendered as a poster grid (Plex-saga style); each
// tile drills into the existing /collections/:id detail page.

function CollectionTile({ entry }: { entry: CollectionListEntry }) {
  const [imageFailed, setImageFailed] = useState(false);
  const showPoster = !!entry.poster_url && !imageFailed;
  return (
    <Link
      to={`/collections/${encodeURIComponent(entry.id)}`}
      className="group flex flex-col gap-2 outline-none focus-visible:ring-2 focus-visible:ring-accent rounded-[--radius-lg]"
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
      <p className="line-clamp-2 px-0.5 text-sm font-medium text-text-primary group-hover:text-white transition-colors">
        {entry.name}
      </p>
    </Link>
  );
}

export default function Collections() {
  const { t } = useTranslation();
  const { data, isLoading, isError } = useCollections();

  if (isLoading) {
    return (
      <div className="flex min-h-[60vh] items-center justify-center">
        <Spinner size="lg" />
      </div>
    );
  }

  const entries = data?.collections ?? [];

  if (isError || entries.length === 0) {
    return (
      <div className="flex min-h-[60vh] items-center justify-center px-6">
        <EmptyState
          title={t("collections.emptyTitle", { defaultValue: "No hay colecciones" })}
          description={t("collections.emptyDescription", {
            defaultValue:
              "Cuando escanees pelis con metadatos de TMDb, las sagas (Star Wars, MCU, ...) apareceran aqui automaticamente.",
          })}
        />
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-6 px-6 py-8 sm:px-10">
      <header className="flex items-baseline gap-3">
        <h1 className="text-3xl font-semibold text-text-primary">
          {t("collections.title", { defaultValue: "Colecciones" })}
        </h1>
        <span className="text-sm text-text-muted">{entries.length}</span>
      </header>

      <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
        {entries.map((entry) => (
          <CollectionTile key={entry.id} entry={entry} />
        ))}
      </div>
    </div>
  );
}
