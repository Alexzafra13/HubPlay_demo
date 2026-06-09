import { useRef, useState, useEffect } from "react";
import type { FC, ReactNode } from "react";
import { useWindowVirtualizer } from "@tanstack/react-virtual";
import type { MediaItem } from "@/api/types";
import { Skeleton } from "@/components/common/Skeleton";
import { EmptyState } from "@/components/common/EmptyState";
import { PosterCard } from "./PosterCard";

interface MediaGridProps {
  items: MediaItem[];
  loading: boolean;
  emptyMessage?: string;
  /**
   * Optional per-item href builder. When provided, every PosterCard
   * uses this to derive its destination instead of the default
   * `/movies/{id}` / `/series/{id}` mapping. Federated grids pass a
   * builder that returns `/peers/{peerID}/items/{itemID}` so the
   * same grid component routes into a different detail page.
   */
  hrefFor?: (item: MediaItem) => string;
  /**
   * Optional per-item badge node rendered at the bottom-left of each
   * card. Federated grids use this to attribute the source peer.
   */
  cornerBadgeFor?: (item: MediaItem) => ReactNode;
}

const SKELETON_KEYS = [
  "grid-sk-a",
  "grid-sk-b",
  "grid-sk-c",
  "grid-sk-d",
  "grid-sk-e",
  "grid-sk-f",
  "grid-sk-g",
  "grid-sk-h",
];

// Gap entre tarjetas en px (≈ gap-4 de Tailwind), usado tanto en la
// cuadrícula directa como en la virtualizada para que ambas coincidan.
const GAP = 16;
// Alto del bloque de info bajo el póster: pt-2 (8) + título text-sm de 1
// línea (~20) + gap-0.5 (2) + meta text-xs de 1 línea (~16) ≈ 46px. El
// título y la meta de PosterCard usan `truncate` (siempre 1 línea), así
// que la altura de cada tarjeta es DETERMINISTA y la fila virtualizada
// puede ser de altura fija. Si PosterCard pasara a varias líneas, habría
// que revisar este valor (o medir por fila).
const META_BLOCK = 46;
// Por debajo de este nº de ítems se renderiza la cuadrícula completa sin
// virtualizar: el coste en DOM es trivial, la ruta es más simple y es la
// que ejercitan los tests jsdom (donde el virtualizador de ventana no
// recibe el evento de layout inicial). Mismo enfoque que EPGGrid.
const VIRTUALIZE_THRESHOLD = 60;

// columnsForWidth es la ÚNICA fuente de verdad del nº de columnas
// (responsive). Reemplaza a las clases `grid-cols-*` para que la
// cuadrícula directa y la virtualizada usen exactamente el mismo cálculo.
// Los cortes coinciden con los breakpoints de Tailwind (sm/md/lg/xl).
function columnsForWidth(w: number): number {
  if (w >= 1280) return 6;
  if (w >= 1024) return 5;
  if (w >= 768) return 4;
  if (w >= 640) return 3;
  return 2;
}

// useGridColumns devuelve el nº de columnas actual y lo recalcula al
// redimensionar la ventana. Compartido por ambas rutas (directa /
// virtualizada).
function useGridColumns(): number {
  const [cols, setCols] = useState(() =>
    typeof window === "undefined" ? 6 : columnsForWidth(window.innerWidth),
  );
  useEffect(() => {
    const onResize = () => setCols(columnsForWidth(window.innerWidth));
    onResize();
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);
  return cols;
}

function gridTemplate(columns: number): React.CSSProperties {
  return { gridTemplateColumns: `repeat(${columns}, minmax(0, 1fr))` };
}

function renderCards(
  items: MediaItem[],
  hrefFor?: (item: MediaItem) => string,
  cornerBadgeFor?: (item: MediaItem) => ReactNode,
) {
  return items.map((item) => (
    <PosterCard
      key={item.id}
      item={item}
      href={hrefFor?.(item)}
      cornerBadge={cornerBadgeFor?.(item)}
    />
  ));
}

const MediaGrid: FC<MediaGridProps> = ({
  items,
  loading,
  emptyMessage = "No items found",
  hrefFor,
  cornerBadgeFor,
}) => {
  const columns = useGridColumns();

  if (loading) {
    return (
      <div className="grid gap-3 sm:gap-4" style={gridTemplate(columns)}>
        {SKELETON_KEYS.map((k) => (
          <div key={k} className="flex flex-col gap-2">
            <Skeleton
              variant="rectangular"
              className="aspect-[2/3] w-full rounded-[--radius-lg]"
            />
            <Skeleton variant="text" width="80%" />
            <Skeleton variant="text" width="40%" />
          </div>
        ))}
      </div>
    );
  }

  if (items.length === 0) {
    return (
      <EmptyState
        title={emptyMessage}
        icon={
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.5}>
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              d="M7 4v16m10-16v16M3 8h4m10 0h4M3 12h18M3 16h4m10 0h4M4 20h16a1 1 0 001-1V5a1 1 0 00-1-1H4a1 1 0 00-1 1v14a1 1 0 001 1z"
            />
          </svg>
        }
      />
    );
  }

  // Catálogos pequeños: cuadrícula completa, ruta simple.
  if (items.length <= VIRTUALIZE_THRESHOLD) {
    return (
      <div
        data-testid="media-grid"
        className="grid gap-3 sm:gap-4"
        style={gridTemplate(columns)}
      >
        {renderCards(items, hrefFor, cornerBadgeFor)}
      </div>
    );
  }

  // Catálogos grandes: virtualización por filas (DOM acotado).
  return (
    <VirtualizedMediaGrid
      items={items}
      columns={columns}
      hrefFor={hrefFor}
      cornerBadgeFor={cornerBadgeFor}
    />
  );
};

interface VirtualizedMediaGridProps {
  items: MediaItem[];
  columns: number;
  hrefFor?: (item: MediaItem) => string;
  cornerBadgeFor?: (item: MediaItem) => ReactNode;
}

// VirtualizedMediaGrid aísla el uso de @tanstack/react-virtual en su
// propio componente (igual que EPGGrid → VirtualizedRows). Virtualiza por
// filas contra el scroll de la PÁGINA (`useWindowVirtualizer`), que es la
// UX de las páginas de catálogo.
//
// Lleva el directivo `"use no memo"`: el virtualizador es un store externo
// mutable cuyas funciones no son memoizables; el React Compiler (babel
// v1.0) cachearía getVirtualItems() y el grid dejaría de reciclar al
// scrollear (verificado en navegador, ver web/verify/). La regla de lint
// `react-hooks/incompatible-library` haría el auto-bail automáticamente,
// pero solo reconoce `useVirtualizer`, no `useWindowVirtualizer`; y este
// último es el que acota bien el DOM con scroll de página (useVirtualizer
// sobre el elemento raíz renderiza todo). De ahí el directivo explícito,
// confinado a este pequeño componente.
function VirtualizedMediaGrid({
  items,
  columns,
  hrefFor,
  cornerBadgeFor,
}: VirtualizedMediaGridProps) {
  "use no memo";

  const containerRef = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(0);
  // Offset del contenedor respecto al top del documento — alinea las
  // coordenadas del virtualizador con el scroll de la página.
  const [scrollMargin, setScrollMargin] = useState(0);

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const measure = () => {
      const rect = el.getBoundingClientRect();
      setWidth(rect.width);
      setScrollMargin(rect.top + window.scrollY);
    };
    measure();
    window.addEventListener("resize", measure);
    return () => window.removeEventListener("resize", measure);
  }, []);

  const cellWidth = width > 0 ? (width - GAP * (columns - 1)) / columns : 180;
  // Altura de fila fija = póster (2:3) + bloque de info + gap.
  const rowHeight = cellWidth * 1.5 + META_BLOCK + GAP;
  const rowCount = Math.ceil(items.length / columns);

  const virtualizer = useWindowVirtualizer({
    count: rowCount,
    estimateSize: () => rowHeight,
    overscan: 4,
    scrollMargin,
  });

  return (
    <div
      ref={containerRef}
      data-testid="media-grid-virtualized"
      style={{ height: virtualizer.getTotalSize(), position: "relative", width: "100%" }}
    >
      {virtualizer.getVirtualItems().map((virtualRow) => {
        const start = virtualRow.index * columns;
        return (
          <div
            key={virtualRow.key}
            className="grid"
            style={{
              ...gridTemplate(columns),
              position: "absolute",
              top: 0,
              left: 0,
              width: "100%",
              height: rowHeight,
              transform: `translateY(${virtualRow.start - scrollMargin}px)`,
              gap: GAP,
              paddingBottom: GAP,
            }}
          >
            {renderCards(items.slice(start, start + columns), hrefFor, cornerBadgeFor)}
          </div>
        );
      })}
    </div>
  );
}

export { MediaGrid };
