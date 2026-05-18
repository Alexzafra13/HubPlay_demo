// BarTimeline — wrapper Recharts BarChart con mismo styling que
// AreaTimeline. Pensado para series tipo "minutos vistos por dia"
// donde cada bucket es un dia/hora discreta.
//
// Hover muestra tooltip con label + valor formateado. Las barras
// tienen radio en la parte superior (look "card-stack" tipico de
// dashboards modernos).

import {
  Bar,
  BarChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";

interface BarTimelineProps {
  data: Array<Record<string, unknown>>;
  xKey: string;
  yKey: string;
  color: string;
  unit?: string;
  /**
   * Formato del eje X. Default: muestra el valor crudo. Para fechas
   * pasa una funcion que devuelva "lun", "08/05" o similar.
   */
  formatX?: (v: unknown) => string;
  formatY?: (v: number) => string;
}

export function BarTimeline({
  data,
  xKey,
  yKey,
  color,
  unit = "",
  formatX,
  formatY,
}: BarTimelineProps) {
  return (
    <ResponsiveContainer width="100%" height="100%">
      <BarChart data={data} margin={{ top: 4, right: 4, bottom: 4, left: 0 }}>
        <CartesianGrid
          strokeDasharray="2 4"
          stroke="var(--color-border-subtle)"
          vertical={false}
        />
        <XAxis
          dataKey={xKey}
          tick={{ fill: "var(--color-text-muted)", fontSize: 10 }}
          axisLine={false}
          tickLine={false}
          tickFormatter={(v) => (formatX ? formatX(v) : String(v))}
        />
        <YAxis
          tick={{ fill: "var(--color-text-muted)", fontSize: 10 }}
          axisLine={false}
          tickLine={false}
          width={32}
          tickFormatter={(v) =>
            formatY ? formatY(Number(v)) : `${Math.round(Number(v))}${unit}`
          }
        />
        <Tooltip
          cursor={{ fill: "var(--color-bg-elevated)", opacity: 0.5 }}
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
            return [formatY ? formatY(n) : `${n}${unit}`, ""];
          }}
        />
        <Bar
          dataKey={yKey}
          fill={color}
          radius={[4, 4, 0, 0]}
          isAnimationActive={false}
        />
      </BarChart>
    </ResponsiveContainer>
  );
}
