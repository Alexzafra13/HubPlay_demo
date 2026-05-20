// ItemKebab — menú flotante de acciones admin sobre un item, pensado
// para vivir en cualquier card de poster (PosterCard, LandscapeCard,
// search results). Click → dropdown con todas las acciones que el
// detalle ofrece, sin tener que abrir la página.
//
// Acciones (filtradas por tipo y rol):
//   - Identificar          (admin, movie/series)
//   - Editar metadatos     (admin, movie/series)
//   - Cambiar imágenes     (admin, todos los tipos — incluye seasons y
//                           episodes, que tienen su propio póster +
//                           fondo editable)
//   - Refrescar metadatos  (admin, todos los tipos)
//   - Información del archivo (todos los usuarios, sólo cuando hay
//                              detailHref + tipo con media_streams)
//
// Visibilidad: el kebab entero se oculta cuando no hay ninguna acción
// aplicable (usuario no admin sobre canal IPTV, etc.).
//
// Comportamiento dentro de un <Link>: stopPropagation + preventDefault
// en cada handler para que el click en el kebab no dispare la
// navegación del card.

import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router";
import {
  MoreVertical,
  Search,
  Edit3,
  ImageIcon as ImagePicto,
  RefreshCw,
  Info,
} from "lucide-react";
import { useItemActions } from "@/store/itemActions";
import { useAuthStore } from "@/store/auth";
import { useRefreshItemMetadata } from "@/api/hooks";

interface Props {
  itemID: string;
  itemType: string;
  /** Path al detalle del item — usado para "Información del archivo"
   *  cuando el kebab está en un poster fuera de la página detail.
   *  Cuando lo omitimos, esa entrada del menú no se renderiza. */
  detailHref?: string;
}

export function ItemKebab({ itemID, itemType, detailHref }: Props) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const isAdmin = useAuthStore((s) => s.user?.role === "admin");
  const openIdentify = useItemActions((s) => s.openIdentify);
  const openEditor = useItemActions((s) => s.openEditor);
  const openImages = useItemActions((s) => s.openImages);
  const refresh = useRefreshItemMetadata(itemID);
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  // Cierra al click fuera y al Escape — mismo patrón que KebabMenu.
  useEffect(() => {
    if (!open) return;
    const handleDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", handleDown);
    document.addEventListener("keydown", handleKey);
    return () => {
      document.removeEventListener("mousedown", handleDown);
      document.removeEventListener("keydown", handleKey);
    };
  }, [open]);

  const stopAll = (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
  };

  // Determina qué acciones aplican. Si no hay ninguna, devolvemos null
  // — el botón del kebab desaparece y la card queda limpia.
  const canIdentify = isAdmin && (itemType === "movie" || itemType === "series");
  const canEditImages = isAdmin; // todos los tipos incluyen seasons + episodes
  const canRefresh = isAdmin;
  const canShowFileInfo =
    !!detailHref && (itemType === "movie" || itemType === "episode");

  if (!canIdentify && !canEditImages && !canRefresh && !canShowFileInfo) {
    return null;
  }

  return (
    <div ref={ref} className="relative" onClick={stopAll}>
      <button
        type="button"
        onClick={(e) => {
          stopAll(e);
          setOpen((o) => !o);
        }}
        aria-label={t("itemKebab.label", { defaultValue: "Acciones del item" })}
        aria-expanded={open}
        aria-haspopup="menu"
        className="flex size-7 items-center justify-center rounded-full bg-black/60 text-white opacity-0 backdrop-blur-sm transition-opacity hover:bg-black/80 focus:outline-none focus:ring-2 focus:ring-accent group-hover:opacity-100 focus:opacity-100"
      >
        <MoreVertical className="size-4" />
      </button>

      {open && (
        <div
          role="menu"
          className="absolute right-0 top-full z-20 mt-1 min-w-[200px] overflow-hidden rounded-[--radius-md] border border-border bg-bg-card shadow-lg"
        >
          {canEditImages && (
            <MenuButton
              icon={<ImagePicto className="size-3.5" />}
              label={t("itemKebab.images", { defaultValue: "Cambiar imágenes…" })}
              onClick={(e) => {
                stopAll(e);
                openImages(itemID, itemType);
                setOpen(false);
              }}
            />
          )}
          {canIdentify && (
            <MenuButton
              icon={<Search className="size-3.5" />}
              label={t("identify.menuLabel", { defaultValue: "Identificar…" })}
              onClick={(e) => {
                stopAll(e);
                openIdentify(itemID, itemType);
                setOpen(false);
              }}
            />
          )}
          {canIdentify && (
            <MenuButton
              icon={<Edit3 className="size-3.5" />}
              label={t("metadataEditor.menuLabel", {
                defaultValue: "Editar metadatos…",
              })}
              onClick={(e) => {
                stopAll(e);
                openEditor(itemID, itemType);
                setOpen(false);
              }}
            />
          )}
          {canRefresh && (
            <MenuButton
              icon={<RefreshCw className={`size-3.5 ${refresh.isPending ? "animate-spin" : ""}`} />}
              label={t("itemDetail.refreshMetadata", {
                defaultValue: "Actualizar metadatos",
              })}
              onClick={(e) => {
                stopAll(e);
                // Llama al backend — re-corre el enrich del scanner
                // y actualiza items/imágenes en DB. Antes esto sólo
                // invalidaba caché del cliente y por eso no resolvía
                // los items con metadata stale (filename como título,
                // sin póster, sin estudio enlazado, etc.).
                refresh.mutate();
                setOpen(false);
              }}
            />
          )}
          {canShowFileInfo && detailHref && (
            <MenuButton
              icon={<Info className="size-3.5" />}
              label={t("itemKebab.fileInfo", {
                defaultValue: "Información del archivo",
              })}
              onClick={(e) => {
                stopAll(e);
                navigate(`${detailHref}#media-info-section`);
                setOpen(false);
              }}
            />
          )}
        </div>
      )}
    </div>
  );
}

interface MenuButtonProps {
  icon: React.ReactNode;
  label: string;
  onClick: (e: React.MouseEvent) => void;
}

function MenuButton({ icon, label, onClick }: MenuButtonProps) {
  return (
    <button
      type="button"
      role="menuitem"
      onClick={onClick}
      className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm text-text hover:bg-bg-elevated"
    >
      {icon}
      {label}
    </button>
  );
}
