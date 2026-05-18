// AreaTimeline — wrapper de Recharts AreaChart con el tema del
// proyecto aplicado (colores CSS vars, tooltip dark, grid sutil).
//
// Pensado para series temporales cortas (1h, 30s/sample). Acepta
// un solo trazo - si en algun momento quieres multi-serie, lo
// extendemos a un array de configs.
//
// Recharts es responsive via ResponsiveContainer; el padre debe
// tener height fijo (lo hace ChartCard con su prop `height`).

import {
  Area,
  AreaChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";

interface AreaTimelineProps {
  data: Array<Record<string, unknown>>;
  /** Clave del eje X (típicamente "ts" o "label"). */
  xKey: string;
  /** Clave del valor a graficar (típicamente "value"). */
  yKey: string;
  /** Color HEX o CSS var del trazo + relleno. */
  color: string;
  /** Sufijo del valor en tooltip ("%", " MB", etc.). */
  unit?: string;
  /** Min/max forzados; default auto. */
  yDomain?: [number | "auto", number | "auto"];
  /** Formato del tick del eje X (lo aplica solo en hover; el eje X
   *  queda oculto en este chart para mantener look "ambient"). */
  formatX?: (v: unknown) => string;
  /** Formato del valor Y en tooltip. */
  formatY?: (v: number) => string;
}

export function AreaTimeline({
  data,
  xKey,
  yKey,
  color,
  unit = "",
  yDomain = [0, "auto"],
  formatX,
  formatY,
}: AreaTimelineProps) {
  const gradientId = `area-gradient-${yKey}`;
  return (
    <ResponsiveContainer width="100%" height="100%">
      <AreaChart
        data={data}
        margin={{ top: 4, right: 8, bottom: 0, left: 0 }}
      >
        <defs>
          <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor={color} stopOpacity={0.35} />
            <stop offset="100%" stopColor={color} stopOpacity={0} />
          </linearGradient>
        </defs>
        <CartesianGrid
          strokeDasharray="2 4"
          stroke="var(--color-border-subtle)"
          vertical={false}
        />
        <XAxis dataKey={xKey} tick={false} axisLine={false} tickLine={false} />
        <YAxis
          domain={yDomain}
          tick={{ fill: "var(--color-text-muted)", fontSize: 10 }}
          axisLine={false}
          tickLine={false}
          width={32}
          tickFormatter={(v) =>
            formatY ? formatY(Number(v)) : `${Math.round(Number(v))}${unit}`
          }
        />
        <Tooltip
          cursor={{
            stroke: "var(--color-border)",
            strokeWidth: 1,
            strokeDasharray: "2 2",
          }}
          contentStyle={{
            background: "var(--color-bg-elevated)",
            border: "1px solid var(--color-border)",
            borderRadius: "var(--radius-md, 8px)",
            color: "var(--color-text-primary)",
            fontSize: "11px",
            padding: "6px 10px",
          }}
          labelStyle={{
            color: "var(--color-text-muted)",
            fontSize: "10px",
            marginBottom: "2px",
          }}
          labelFormatter={(v) => (formatX ? formatX(v) : String(v))}
          formatter={(v) => {
            const n = Number(v);
            if (Number.isNaN(n)) return [String(v ?? ""), ""];
            return [formatY ? formatY(n) : `${n.toFixed(1)}${unit}`, ""];
          }}
        />
        <Area
          type="monotone"
          dataKey={yKey}
          stroke={color}
          strokeWidth={2}
          fill={`url(#${gradientId})`}
          isAnimationActive={false}
        />
      </AreaChart>
    </ResponsiveContainer>
  );
}
