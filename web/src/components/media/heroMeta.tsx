// Shared meta-row primitives used by both HeroSection (movies / episodes /
// seasons) and SeriesHero (series). Both surfaces want the same Plex-
// inspired bits — overview clamp toggle, external-ID row, studio brand
// mark — so the rendering logic lives here, behind small components,
// instead of getting copy-pasted with subtle drift between the two.

import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import type { MediaItem } from "@/api/types";

// ─── External IDs ───────────────────────────────────────────────────────

// External-ID providers we surface in the hero. Order is fixed (IMDb
// first because it's the most-recognised in the movie/TV world). The
// scanner only persists ids the metadata provider returned, so this
// row stays empty when no match was made — no broken links.
//
// Module-private so this file only exports React components and stays
// happy with the `react-refresh/only-export-components` rule. If a
// future caller needs the catalogue, move it to /utils/heroMeta.ts
// alongside `formatPremiereDate`.
const EXTERNAL_PROVIDERS: ReadonlyArray<{
  key: string;
  label: "openOnImdb" | "openOnTmdb" | "openOnTvdb";
  href: (id: string, type: string) => string;
  text: string;
}> = [
  {
    key: "imdb",
    label: "openOnImdb",
    href: (id) => `https://www.imdb.com/title/${encodeURIComponent(id)}/`,
    text: "IMDb",
  },
  {
    key: "tmdb",
    label: "openOnTmdb",
    // TMDb's URL splits movies vs TV at the path level; passing the
    // wrong slug 404s rather than redirects, so honour the item's type.
    href: (id, type) => {
      const slug = type === "series" || type === "season" || type === "episode"
        ? "tv"
        : "movie";
      return `https://www.themoviedb.org/${slug}/${encodeURIComponent(id)}`;
    },
    text: "TMDb",
  },
  {
    key: "tvdb",
    label: "openOnTvdb",
    href: (id) => `https://thetvdb.com/?tab=series&id=${encodeURIComponent(id)}`,
    text: "TVDb",
  },
];

interface ExternalIdRowProps {
  item: MediaItem;
}

/**
 * Renders the IMDb / TMDb / TVDb mini-chip row. Returns null when no
 * external ids were persisted so callers don't have to gate the
 * surrounding spacing manually.
 */
export function ExternalIdRow({ item }: ExternalIdRowProps) {
  const { t } = useTranslation();
  const links = useMemo(() => {
    if (!item.external_ids) return [];
    return EXTERNAL_PROVIDERS.flatMap((p) => {
      const id = item.external_ids?.[p.key];
      return id ? [{ ...p, id }] : [];
    });
  }, [item.external_ids]);

  if (links.length === 0) return null;

  return (
    <div className="flex flex-wrap items-center gap-2">
      {links.map((link) => (
        <a
          key={link.key}
          href={link.href(link.id, item.type)}
          target="_blank"
          rel="noopener noreferrer"
          aria-label={t(`itemDetail.${link.label}`)}
          className="rounded-md border border-border/60 bg-bg-card/40 px-2 py-1 text-xs font-semibold text-text-secondary transition-colors hover:border-border hover:bg-bg-elevated hover:text-text-primary backdrop-blur-sm"
        >
          {link.text}
        </a>
      ))}
    </div>
  );
}

// ─── Overview with read-more toggle ─────────────────────────────────────

interface OverviewProps {
  overview: string | null | undefined;
  /** Optional CSS class override for the <p>; defaults to the hero
   *  light-on-dark colour. Callers like the federation peer hero pass
   *  their own colour to match the surrounding palette. */
  className?: string;
}

// 240 chars is a pragmatic threshold — covers ~3 lines at hero width.
// Below that the clamp wouldn't fire anyway, so the toggle would be a
// no-op affordance that confuses more than it helps.
const OVERVIEW_CLAMP_CHARS = 240;

/**
 * Description block with a "Read more / Read less" affordance when the
 * text would clamp. Plex surfaces the same control in the same place
 * (between meta chips and the play CTA) so the hero stays focused on
 * the play button without throwing the synopsis away.
 */
export function OverviewWithReadMore({ overview, className }: OverviewProps) {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState(false);

  if (overview == null) return null;

  const clampable = overview.length > OVERVIEW_CLAMP_CHARS;
  const baseClass =
    className ??
    "text-sm leading-relaxed text-text-primary/95 sm:text-[15px]";

  return (
    <div className="max-w-2xl">
      <p
        className={[
          baseClass,
          expanded || !clampable ? "" : "line-clamp-3",
        ]
          .filter(Boolean)
          .join(" ")}
      >
        {overview}
      </p>
      {clampable && (
        <button
          type="button"
          onClick={() => setExpanded((v) => !v)}
          className="mt-1 text-sm font-medium text-text-secondary transition-colors hover:text-text-primary cursor-pointer"
          aria-expanded={expanded}
        >
          {expanded ? t("itemDetail.readLess") : t("itemDetail.readMore")}
        </button>
      )}
    </div>
  );
}

// ─── Studio mark ────────────────────────────────────────────────────────

interface StudioMarkProps {
  studio?: string;
  studioLogoUrl?: string;
}

/**
 * Studio / network attribution. Renders the brand-mark image when the
 * scanner persisted one (TMDb's `production_companies[].logo_path`
 * resolved server-side); otherwise falls back to the studio name as
 * dim text — older studios with no TMDb logo still get attribution.
 * The leading "·" only appears with the text fallback so the image
 * reads as its own visual element.
 */
export function StudioMark({ studio, studioLogoUrl }: StudioMarkProps) {
  if (studioLogoUrl) {
    return (
      <img
        src={studioLogoUrl}
        alt={studio ?? ""}
        className="ml-1 h-5 w-auto max-w-[120px] object-contain opacity-80"
        loading="lazy"
      />
    );
  }
  if (studio) {
    return <span className="text-xs text-text-muted">· {studio}</span>;
  }
  return null;
}

// `formatPremiereDate` lives in web/src/utils/heroMeta.ts so this file
// only exports React components (Fast Refresh constraint). Callers
// import the helper directly from there, not from here.
