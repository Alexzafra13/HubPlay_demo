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
 *
 * Chips carry brand colours instead of plain text — IMDb's signature
 * yellow, TMDb's dark-blue/green pill — so the row reads at a glance
 * the way Plex / Letterboxd surface external links. The TMDb chip
 * also shows the rating inline (`community_rating` IS TMDb's
 * vote_average); IMDb's rating isn't on the wire (no free API for
 * IMDb), so its chip stays as a deep-link only.
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
          className="inline-flex items-center gap-1.5 rounded-md transition-transform hover:scale-[1.04]"
        >
          <ProviderBadge provider={link.key} />
          {link.key === "tmdb" && item.community_rating != null && (
            <span className="text-xs font-semibold text-text-primary/85 tabular-nums">
              {item.community_rating.toFixed(1)}
            </span>
          )}
        </a>
      ))}
    </div>
  );
}

// Per-provider brand mark. Inline SVG/CSS so we don't depend on a
// third-party logo dependency or ship binary assets — each is small
// enough to sit next to the rating without bloating the bundle.
// Colours match the official press kits (IMDb yellow #F5C518, TMDb
// dark teal-blue #032541 with a #01B4E4 accent).
function ProviderBadge({ provider }: { provider: string }) {
  switch (provider) {
    case "imdb":
      return (
        <span className="inline-flex h-5 items-center rounded-[3px] bg-[#F5C518] px-1.5 text-[11px] font-extrabold leading-none tracking-tight text-black">
          IMDb
        </span>
      );
    case "tmdb":
      return (
        <span className="inline-flex h-5 items-center gap-[2px] rounded-[3px] bg-[#032541] px-1.5 text-[10px] font-extrabold leading-none tracking-tight">
          <span className="text-white">TM</span>
          <span className="text-[#01B4E4]">DB</span>
        </span>
      );
    case "tvdb":
      return (
        <span className="inline-flex h-5 items-center rounded-[3px] bg-[#6CD491] px-1.5 text-[11px] font-extrabold leading-none tracking-tight text-[#0a3d28]">
          TVDB
        </span>
      );
    default:
      return (
        <span className="inline-flex h-5 items-center rounded-[3px] bg-bg-elevated px-1.5 text-[11px] font-bold leading-none text-text-primary">
          {provider}
        </span>
      );
  }
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
 *
 * The pill gets a **fixed footprint** so every studio reads with the
 * same visual weight regardless of the underlying logo's aspect ratio.
 * TMDb's `production_companies[].logo_path` ships logos with wildly
 * different dimensions: square shields (Warner Bros, Disney) used to
 * render as tiny dots while horizontal pillboxes (Marvel Studios,
 * Pixar) ballooned to 3× the size. The fixed pill + centred image with
 * vertical breathing room (max-h smaller than the pill height) makes
 * both look like the same "credits card" lifted off the artwork.
 *
 * The translucent white background also fixes a TMDb foreground-colour
 * issue: their logos arrive with arbitrary fill (Marvel black,
 * Lucasfilm sometimes white, Disney blue, Pixar yellow). On the dark
 * hero a black logo was near-invisible without the white pill.
 */
export function StudioMark({ studio, studioLogoUrl }: StudioMarkProps) {
  if (studioLogoUrl) {
    return (
      <span
        className="ml-1 inline-flex h-8 w-[112px] items-center justify-center rounded-md bg-white/95 px-2 shadow-sm shadow-black/30"
        aria-label={studio}
        title={studio}
      >
        <img
          src={studioLogoUrl}
          alt={studio ?? ""}
          className="max-h-5 max-w-full object-contain"
          loading="lazy"
        />
      </span>
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
