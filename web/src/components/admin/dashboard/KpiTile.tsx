// KpiTile — la unidad atomica del dashboard admin. Misma altura
// fija + mismo shape para que un row de 5 KPIs lea como una sola
// "fila de hechos" del servidor.
//
// Anatomia (compacta, ~96px de alto):
//
//   ┌──────────────────────┐
//   │ ICON   LABEL         │   ← row superior: icono tinted + label
//   │   12.4 GB    47%     │   ← row central: valor grande + secondary
//   │ ━━━━━━━━━━━━━━━━━━━━ │   ← bar opcional + sparkline opcional
//   └──────────────────────┘
//
// Severity: la barra cambia de color segun el ratio (0.0-1.0):
// success < 0.75, warning < 0.9, error >= 0.9. Mismo umbral que
// la rama Host del SystemStatus original.

import type { ComponentType, ReactNode } from "react";
import { Sparkline } from "@/components/admin/Sparkline";

interface KpiTileProps {
  /** Etiqueta corta arriba (ej "CPU", "RAM"). */
  label: string;
  /** Icono Lucide o similar, mismo lenguaje que SectionHeader. */
  icon: ComponentType<{ className?: string }>;
  /** Valor principal (formato libre - "23.5", "12.4 GB", "—"). */
  value: ReactNode;
  /** Valor secundario inline (ej "%", "/ 16 GB"). */
  unit?: ReactNode;
  /** Texto al lado, abajo (ej "6 núcleos · 12 hilos"). */
  hint?: ReactNode;
  /**
   * Ratio 0..1 para colorear severidad + opcional progress bar.
   * undefined = sin bar.
   */
  ratio?: number;
  /** Datos para el sparkline inline. Si vacio, no se pinta. */
  sparkline?: number[];
  /**
   * Override de tono cuando no hay ratio (e.g. KPI sin umbral
   * conceptual como "sesiones activas" cuando max=infinito).
   */
  tone?: "neutral" | "success" | "warning" | "error";
}

const TONE_COLOUR: Record<NonNullable<KpiTileProps["tone"]>, string> = {
  neutral: "var(--color-accent)",
  success: "var(--color-success)",
  warning: "var(--color-warning)",
  error: "var(--color-error)",
};

function severityFromRatio(r: number): NonNullable<KpiTileProps["tone"]> {
  if (r < 0.75) return "success";
  if (r < 0.9) return "warning";
  return "error";
}

export function KpiTile({
  label,
  icon: Icon,
  value,
  unit,
  hint,
  ratio,
  sparkline,
  tone,
}: KpiTileProps) {
  const effectiveTone: NonNullable<KpiTileProps["tone"]> =
    tone ?? (ratio !== undefined ? severityFromRatio(ratio) : "neutral");
  const colour = TONE_COLOUR[effectiveTone];
  const pct = ratio !== undefined ? Math.max(0, Math.min(100, ratio * 100)) : 0;

  return (
    <div className="flex h-full flex-col gap-2 rounded-[--radius-lg] border border-border bg-bg-card p-4">
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-1.5 text-text-muted">
          <Icon className="h-3.5 w-3.5" aria-hidden />
          <span className="text-[10px] font-medium uppercase tracking-wider">
            {label}
          </span>
        </div>
        {sparkline && sparkline.length > 1 && (
          <Sparkline
            values={sparkline}
            width={64}
            height={20}
            strokeColor={colour}
          />
        )}
      </div>
      <div className="flex items-baseline gap-1.5">
        <span
          className="text-2xl font-semibold leading-none tabular-nums"
          style={{ color: ratio !== undefined ? colour : "var(--color-text-primary)" }}
        >
          {value}
        </span>
        {unit && (
          <span className="text-sm text-text-muted tabular-nums">{unit}</span>
        )}
      </div>
      {ratio !== undefined && (
        <div className="h-1 w-full overflow-hidden rounded-full bg-bg-elevated">
          <div
            className="h-full transition-all"
            style={{ width: `${pct}%`, background: colour }}
          />
        </div>
      )}
      {hint && (
        <p className="text-[11px] leading-tight text-text-muted">{hint}</p>
      )}
    </div>
  );
}
