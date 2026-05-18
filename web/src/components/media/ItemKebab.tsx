// ItemKebab — menú flotante de acciones admin sobre un item, pensado
// para vivir en cualquier card de poster (PosterCard, LandscapeCard,
// search results). Click → dropdown con Identify / Editar metadatos.
// La selección abre el modal correspondiente vía useItemActions, que
// los hostea de forma centralizada en App root.
//
// Visibilidad: sólo para admins, y sólo para tipos identificables
// (movie / series). Episodios / temporadas no aplican.
//
// Comportamiento dentro de un <Link>: stopPropagation + preventDefault
// en cada handler para que el click en el kebab no dispare la navegación
// del card.

import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { MoreVertical, Search, Edit3 } from "lucide-react";
import { useItemActions } from "@/store/itemActions";
import { useAuthStore } from "@/store/auth";

interface Props {
  itemID: string;
  itemType: string;
}

export function ItemKebab({ itemID, itemType }: Props) {
  const { t } = useTranslation();
  const isAdmin = useAuthStore((s) => s.user?.role === "admin");
  const openIdentify = useItemActions((s) => s.openIdentify);
  const openEditor = useItemActions((s) => s.openEditor);
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  // Cierra al click fuera y al Escape — mismo patrón que KebabMenu
  // (no lo reusamos directo porque ése es para grids admin y vive
  // con estilo distinto; queremos un kebab flotante sobre el poster).
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

  if (!isAdmin) return null;
  if (itemType !== "movie" && itemType !== "series") return null;

  const stopAll = (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
  };

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
        className="flex h-7 w-7 items-center justify-center rounded-full bg-black/60 text-white opacity-0 backdrop-blur-sm transition-opacity hover:bg-black/80 focus:outline-none focus:ring-2 focus:ring-accent group-hover:opacity-100 focus:opacity-100"
      >
        <MoreVertical className="h-4 w-4" />
      </button>

      {open && (
        <div
          role="menu"
          className="absolute right-0 top-full z-20 mt-1 min-w-[180px] overflow-hidden rounded-[--radius-md] border border-border bg-bg-card shadow-lg"
        >
          <button
            type="button"
            role="menuitem"
            onClick={(e) => {
              stopAll(e);
              openIdentify(itemID);
              setOpen(false);
            }}
            className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm text-text hover:bg-bg-elevated"
          >
            <Search className="h-3.5 w-3.5" />
            {t("identify.menuLabel", { defaultValue: "Identificar…" })}
          </button>
          <button
            type="button"
            role="menuitem"
            onClick={(e) => {
              stopAll(e);
              openEditor(itemID);
              setOpen(false);
            }}
            className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm text-text hover:bg-bg-elevated"
          >
            <Edit3 className="h-3.5 w-3.5" />
            {t("metadataEditor.menuLabel", { defaultValue: "Editar metadatos…" })}
          </button>
        </div>
      )}
    </div>
  );
}
