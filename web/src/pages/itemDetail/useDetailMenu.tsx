import { useTranslation } from "react-i18next";
import type { ItemDetail } from "@/api/types";
import { useRefreshItemMetadata } from "@/api/hooks";
import type { HeroMenuItem } from "@/components/media/HeroSection";
import {
  ImageIcon,
  RefreshIcon,
  InfoIcon,
} from "@/components/media/icons";
import { Search, Edit3, Lock, Unlock } from "lucide-react";

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
  // Editor manual de metadatos. Opcional, mismo razonamiento.
  onOpenMetadataEditor?: () => void;
  // Toggle del lock de metadatos. Opcional. Cuando se pulsa, la UI
  // dispara el PUT /metadata/lock; el padre refresca el item y este
  // hook re-renderiza con el otro label (lock ↔ unlock).
  metadataLocked?: boolean;
  onToggleMetadataLock?: () => void;
}

export function useDetailMenu({
  item,
  itemId,
  isAdmin,
  onOpenImageManager,
  onOpenIdentify,
  onOpenMetadataEditor,
  metadataLocked,
  onToggleMetadataLock,
}: UseDetailMenuArgs): HeroMenuItem[] {
  const { t } = useTranslation();
  const refresh = useRefreshItemMetadata(itemId ?? "");
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

    if (canIdentify && onOpenMetadataEditor) {
      items.push({
        label: t("metadataEditor.menuLabel", { defaultValue: "Editar metadatos…" }),
        icon: <Edit3 className="h-4 w-4" />,
        onClick: onOpenMetadataEditor,
      });
    }

    if (canIdentify && onToggleMetadataLock) {
      items.push({
        label: metadataLocked
          ? t("metadataEditor.unlock", { defaultValue: "Desbloquear metadatos" })
          : t("metadataEditor.lock", { defaultValue: "Bloquear metadatos" }),
        icon: metadataLocked
          ? <Unlock className="h-4 w-4" />
          : <Lock className="h-4 w-4" />,
        onClick: onToggleMetadataLock,
      });
    }

    items.push({
      label: t("itemDetail.refreshMetadata"),
      icon: <RefreshIcon />,
      // Llamada real al backend (no sólo invalidar caché como antes).
      // Re-corre el enrich del scanner: nuevo match TMDb, re-link
      // a estudio/saga, descarga de imágenes. Lock-aware.
      onClick: () => {
        refresh.mutate();
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
