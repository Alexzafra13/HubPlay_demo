import { useTranslation } from "react-i18next";
import { useQueryClient } from "@tanstack/react-query";
import type { ItemDetail } from "@/api/types";
import { queryKeys } from "@/api/hooks";
import type { HeroMenuItem } from "@/components/media/HeroSection";
import {
  ImageIcon,
  RefreshIcon,
  InfoIcon,
  ExternalLinkIcon,
} from "@/components/media/icons";

// Builds the hero kebab menu rows for a detail page. Lives here as a
// hook (not a useMemo inside the page) because the result depends on
// the queryClient + i18n context that the page would otherwise plumb
// in by hand.
//
// Composition rules (kept identical to the inline version that lived
// in ItemDetail before the split):
//
//   - Admin-only tools come first (image manager + metadata refresh).
//   - "Media info" jumps to the section anchor on the same page.
//   - External-provider deep links are filtered to providers we know
//     how to URL-build, so an unknown key in the wire (a future
//     `wikidata`) is silently ignored rather than emitting a dead
//     row pointing nowhere.
//   - Series/season/episode use TMDb /tv/, movies use /movie/.
//
// The returned array IS rebuilt on every render — that was true of
// the inline IIFE too. With React Compiler auto-memoising the parent,
// a manual useMemo would just allocate dependency arrays for nothing.

export interface UseDetailMenuArgs {
  // ItemDetail (not the bare MediaItem) because the menu reads
  // `media_streams` and `external_ids`, which only exist on the
  // detail-shape returned by GET /items/{id}. The page passes the
  // `useItem(id).data` value directly so the type matches by
  // construction.
  item: ItemDetail | undefined;
  itemId: string | undefined;
  isAdmin: boolean;
  onOpenImageManager: () => void;
}

export function useDetailMenu({
  item,
  itemId,
  isAdmin,
  onOpenImageManager,
}: UseDetailMenuArgs): HeroMenuItem[] {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const items: HeroMenuItem[] = [];

  if (isAdmin && itemId) {
    items.push({
      label: t("imageManager.title"),
      icon: <ImageIcon />,
      onClick: onOpenImageManager,
    });

    items.push({
      label: t("itemDetail.refreshMetadata"),
      icon: <RefreshIcon />,
      onClick: () => {
        queryClient.invalidateQueries({ queryKey: queryKeys.item(itemId) });
      },
    });
  }

  if (item?.media_streams && item.media_streams.length > 0) {
    items.push({
      label: t("itemDetail.mediaInfo"),
      icon: <InfoIcon />,
      onClick: () => {
        document
          .getElementById("media-info-section")
          ?.scrollIntoView({ behavior: "smooth" });
      },
    });
  }

  if (item?.external_ids) {
    const ids = item.external_ids;
    const tmdbType: "tv" | "movie" =
      item.type === "series" ||
      item.type === "season" ||
      item.type === "episode"
        ? "tv"
        : "movie";

    if (ids.imdb) {
      items.push({
        label: t("itemDetail.openInIMDb", { defaultValue: "Ver en IMDb" }),
        icon: <ExternalLinkIcon />,
        onClick: () => {
          window.open(
            `https://www.imdb.com/title/${ids.imdb}/`,
            "_blank",
            "noopener,noreferrer",
          );
        },
      });
    }
    if (ids.tmdb) {
      items.push({
        label: t("itemDetail.openInTMDb", { defaultValue: "Ver en TMDb" }),
        icon: <ExternalLinkIcon />,
        onClick: () => {
          window.open(
            `https://www.themoviedb.org/${tmdbType}/${ids.tmdb}`,
            "_blank",
            "noopener,noreferrer",
          );
        },
      });
    }
  }

  return items;
}
