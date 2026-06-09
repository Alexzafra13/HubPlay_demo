import { useRef, useState, useEffect, useLayoutEffect, useCallback } from "react";
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

// Gap entre tarjetas en px (≈ gap-4 de Tailwind). Se usa para calcular el
// ancho de cada celda y la altura de fila en la virtualización.
const GAP = 16;
// Alto del bloque de info bajo el póster: pt-2 (8) + título text-sm de 1
// línea (~20) + gap-0.5 (2) + meta text-xs de 1 línea (~16) ≈ 46px. El
// título y la meta usan `truncate` (siempre 1 línea), así que la altura de
// cada tarjeta es DETERMINISTA y podemos usar filas de altura fija — sin
// medición por fila (que en jsdom/SSR daría 0 y rompería el cálculo).
const META_BLOCK = 46;

// columnsForWidth replica los breakpoints del grid CSS original
// (grid-cols-2 / sm:3 / md:4 / lg:5 / xl:6) usando el ancho de viewport,
// para que el número de columnas sea idéntico al de antes en cada tamaño.
function columnsForWidth(w: number): number {
  if (w >= 1280) return 6;
  if (w >= 1024) return 5;
  if (w >= 768) return 4;
  if (w >= 640) return 3;
  return 2;
}

const MediaGrid: FC<MediaGridProps> = ({
  items,
  loading,
  emptyMessage = "No items found",
  hrefFor,
  cornerBadgeFor,
}) => {
  // Opt-out del React Compiler para este componente. El virtualizador de
  // @tanstack/react-virtual es un store externo mutable que fuerza
  // re-renders vía su propio onChange; el compiler, al memoizar la salida,
  // cacheaba `getVirtualItems()` y el grid no se actualizaba al scrollear
  // (no reciclaba). Este es el escape-hatch oficial hasta que la librería
  // sea compiler-ready. Verificado en navegador real (ver web/verify/).
  "use no memo";

  const containerRef = useRef<HTMLDivElement>(null);

  // Columnas (responsive) y ancho del contenedor — gobiernan cuántas
  // tarjetas entran por fila y la altura estimada de cada fila.
  const [columns, setColumns] = useState(() =>
    typeof window === "undefined" ? 6 : columnsForWidth(window.innerWidth),
  );
  const [containerWidth, setContainerWidth] = useState(0);

  useLayoutEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const measure = () => {
      setColumns(columnsForWidth(window.innerWidth));
      setContainerWidth(el.getBoundingClientRect().width);
    };
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  // Offset del contenedor respecto al top del documento — necesario para
  // alinear las coordenadas del virtualizador de ventana con el scroll.
  const [scrollMargin, setScrollMargin] = useState(0);
  useLayoutEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const update = () =>
      setScrollMargin(el.getBoundingClientRect().top + window.scrollY);
    update();
    window.addEventListener("resize", update);
    return () => window.removeEventListener("resize", update);
  }, [columns, items.length]);

  const rowCount = Math.ceil(items.length / columns);

  // Altura de fila (fija): alto del póster (2:3) + bloque de info + gap.
  const cellWidth =
    containerWidth > 0 ? (containerWidth - GAP * (columns - 1)) / columns : 180;
  const rowHeight = cellWidth * 1.5 + META_BLOCK + GAP;

  const virtualizer = useWindowVirtualizer({
    count: rowCount,
    estimateSize: useCallback(() => rowHeight, [rowHeight]),
    overscan: 4,
    scrollMargin,
  });

  // Recalcula la ventana de filas en el montaje y cuando cambia algún valor
  // de layout (alto de fila, columnas, offset del contenedor). Hace falta
  // un measure() explícito porque en el primer render —y en entornos sin
  // layout como jsdom— getVirtualItems() vuelve vacío pese a tener la altura
  // total correcta, hasta que llega un evento de scroll/resize. Incluir
  // `scrollMargin` asegura que se re-mida DESPUÉS de que el layout effect
  // fije el offset (si no, la primera medición usaría 0). La instancia del
  // virtualizador es estable (el hook la crea con useState), así que el
  // efecto solo corre cuando cambian esos valores, no en cada render.
  useEffect(() => {
    virtualizer.measure();
  }, [virtualizer, rowHeight, scrollMargin, columns]);

  if (loading) {
    return (
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 sm:gap-4 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
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

  // Virtualización por FILAS contra el scroll de la ventana: solo las
  // filas visibles (+overscan) están en el DOM, así una biblioteca de
  // miles de títulos no acumula miles de PosterCard (cada una con su
  // canvas de blurhash + img). El contenedor reserva la altura total para
  // que la barra de scroll y el sentinel de "cargar más" del padre sigan
  // funcionando igual.
  const virtualRows = virtualizer.getVirtualItems();

  return (
    <div
      ref={containerRef}
      style={{ height: virtualizer.getTotalSize(), position: "relative", width: "100%" }}
    >
      {virtualRows.map((virtualRow) => {
        const start = virtualRow.index * columns;
        const rowItems = items.slice(start, start + columns);
        return (
          <div
            key={virtualRow.key}
            data-index={virtualRow.index}
            style={{
              position: "absolute",
              top: 0,
              left: 0,
              width: "100%",
              height: rowHeight,
              transform: `translateY(${virtualRow.start - virtualizer.options.scrollMargin}px)`,
              display: "grid",
              gridTemplateColumns: `repeat(${columns}, minmax(0, 1fr))`,
              gap: GAP,
              paddingBottom: GAP,
            }}
          >
            {rowItems.map((item) => (
              <PosterCard
                key={item.id}
                item={item}
                href={hrefFor?.(item)}
                cornerBadge={cornerBadgeFor?.(item)}
              />
            ))}
          </div>
        );
      })}
    </div>
  );
};

export { MediaGrid };
