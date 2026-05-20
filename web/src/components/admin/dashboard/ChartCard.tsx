// ChartCard — contenedor visual para los charts grandes del
// dashboard. Mismo lenguaje que KpiTile (border + bg-card + radius)
// pero con titulo + subtitle + slot trailing (range selector,
// "ver mas", etc.) y un area interior fija para que el chart
// respire.
//
// Vive al lado de KpiTile en components/admin/dashboard porque
// son piezas hermanas; los uso solo en el panel admin /system.

import type { ComponentType, ReactNode } from "react";

interface ChartCardProps {
  /** Icono Lucide opcional. */
  icon?: ComponentType<{ className?: string }>;
  /** Titulo principal corto. */
  title: string;
  /** Subtitulo opcional (caption). */
  subtitle?: string;
  /** Slot derecha (legenda, range picker, "Ver todas"). */
  trailing?: ReactNode;
  /** Estado de carga - el chart se reemplaza por placeholder. */
  loading?: boolean;
  /** Sin datos - empty state. */
  empty?: boolean;
  /** Texto del empty state (default: "Sin datos"). */
  emptyText?: string;
  /** El chart en si. */
  children: ReactNode;
  /** Altura interior del area del chart en px. Default 200. */
  height?: number;
}

export function ChartCard({
  icon: Icon,
  title,
  subtitle,
  trailing,
  loading,
  empty,
  emptyText = "Sin datos todavía",
  children,
  height = 200,
}: ChartCardProps) {
  return (
    <div className="flex h-full flex-col gap-3 rounded-[--radius-lg] border border-border bg-bg-card p-4">
      <header className="flex items-start justify-between gap-3">
        <div className="flex items-start gap-2 min-w-0">
          {Icon && (
            <div className="flex-none rounded-md bg-bg-elevated p-1.5 text-text-secondary">
              <Icon className="size-3.5" />
            </div>
          )}
          <div className="min-w-0">
            <h3 className="text-sm font-semibold text-text-primary truncate">
              {title}
            </h3>
            {subtitle && (
              <p className="mt-0.5 text-[11px] text-text-muted leading-tight">
                {subtitle}
              </p>
            )}
          </div>
        </div>
        {trailing && <div className="flex-none">{trailing}</div>}
      </header>
      <div
        className="relative w-full"
        style={{ height: `${height}px` }}
      >
        {loading ? (
          <div className="absolute inset-0 flex items-center justify-center text-xs text-text-muted">
            …
          </div>
        ) : empty ? (
          <div className="absolute inset-0 flex flex-col items-center justify-center gap-1 text-text-muted">
            <p className="text-xs">{emptyText}</p>
          </div>
        ) : (
          children
        )}
      </div>
    </div>
  );
}
