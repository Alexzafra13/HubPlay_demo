import { useTranslation } from "react-i18next";
import { useQueryClient } from "@tanstack/react-query";
import type { ItemDetail } from "@/api/types";
import { queryKeys } from "@/api/hooks";
import type { HeroMenuItem } from "@/components/media/HeroSection";
import {
  ImageIcon,
  RefreshIcon,
  InfoIcon,
} from "@/components/media/icons";
import { Search } from "lucide-react";

// Builds the hero kebab menu rows for a detail page. Lives here as a
// hook (not a useMemo inside the page) because the result depends on
// the queryClient + i18n context that the page would otherwise plumb
// in by hand.
//
// Composition rules:
//
//   - Admin-only tools come first (image manager + metadata refresh).
//   - "Media info" jumps to the section anchor on the same page.
//
// External-provider deep links (IMDb / TMDb) used to live here too
// but moved to inline chips in the hero (`<ExternalIdRow>`) since
// they're a frequent destination — duplicating them in the kebab made
// the menu read as crowded for no payoff.

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
  // Identify (admin rematch). Opcional: si el padre no monta el
  // diálogo, el item del menú no se renderiza — sin diálogo no hay
  // acción que disparar.
  onOpenIdentify?: () => void;
}

export function useDetailMenu({
  item,
  itemId,
  isAdmin,
  onOpenImageManager,
  onOpenIdentify,
}: UseDetailMenuArgs): HeroMenuItem[] {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const items: HeroMenuItem[] = [];

  // El flujo de "Identify" sólo aplica a películas y series — los
  // episodios y temporadas heredan el match del padre, y los canales
  // IPTV viven en una jerarquía paralela sin TMDb. Filtramos aquí en
  // el frontend para no enseñar una acción que el backend rechazaría.
  const canIdentify =
    item?.type === "movie" || item?.type === "series";

  if (isAdmin && itemId) {
    items.push({
      label: t("imageManager.title"),
      icon: <ImageIcon />,
      onClick: onOpenImageManager,
    });

    if (canIdentify && onOpenIdentify) {
      items.push({
        label: t("identify.menuLabel", { defaultValue: "Identify…" }),
        icon: <Search className="h-4 w-4" />,
        onClick: onOpenIdentify,
      });
    }

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

  return items;
}
