// Sparkline — pure-SVG mini chart for at-a-glance trend lines.
//
// Lives outside any chart library on purpose. The admin Resumen needs
// a small (≤ 200 px wide), always-mounted sparkline next to a label,
// and pulling in Recharts / Chart.js for that one element would add
// 60-80 KB gzipped for two lines worth of work. A bare <polyline>
// inside an inline SVG paints in <1 ms and matches the brand tone
// (no gridlines, no axes — the eye reads the shape).
//
// Accessibility: the chart is a presentation aid; the textual figure
// next to it (e.g. "8.4 h esta semana") carries the data for screen
// readers. We mark the SVG as `aria-hidden` so AT users don't get a
// decorative-only line read out as a graph.

import type { CSSProperties } from "react";

interface SparklineProps {
  values: number[];
  width?: number;
  height?: number;
  /** Stroke colour. Defaults to the brand accent token. */
  strokeColor?: string;
  /** Optional dotted baseline at value 0 — useful when the series
   *  occasionally dips back to zero so the eye has a reference. */
  showBaseline?: boolean;
  className?: string;
  style?: CSSProperties;
}

export function Sparkline({
  values,
  width = 160,
  height = 36,
  strokeColor = "var(--color-accent)",
  showBaseline = false,
  className,
  style,
}: SparklineProps) {
  if (!values || values.length === 0) {
    // No data → keep the layout slot occupied with a flat dotted line
    // so the surrounding label doesn't reflow when the data arrives.
    return (
      <svg
        width={width}
        height={height}
        viewBox={`0 0 ${width} ${height}`}
        aria-hidden="true"
        className={className}
        style={style}
      >
        <line
          x1={0}
          x2={width}
          y1={height / 2}
          y2={height / 2}
          stroke="var(--color-border)"
          strokeDasharray="2 4"
          strokeWidth={1}
        />
      </svg>
    );
  }

  const max = Math.max(...values, 1);
  const min = Math.min(...values, 0);
  const range = max - min || 1;
  const stepX = values.length > 1 ? width / (values.length - 1) : 0;

  // Reserve a 2 px top/bottom margin so the stroke isn't clipped by
  // the SVG edge when a value lands at exact min/max.
  const usableHeight = height - 4;
  const yFor = (v: number) =>
    2 + (1 - (v - min) / range) * usableHeight;

  const points = values
    .map((v, i) => `${(i * stepX).toFixed(1)},${yFor(v).toFixed(1)}`)
    .join(" ");

  // Closed polygon under the line for the soft fill — uses the same
  // accent token at low alpha. Drawn first so the line strokes on top.
  const fillPoints = `0,${height} ${points} ${width},${height}`;

  return (
    <svg
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      aria-hidden="true"
      className={className}
      style={style}
    >
      <polygon
        points={fillPoints}
        fill={strokeColor}
        opacity={0.12}
      />
      {showBaseline && (
        <line
          x1={0}
          x2={width}
          y1={yFor(0)}
          y2={yFor(0)}
          stroke="var(--color-border)"
          strokeDasharray="2 4"
          strokeWidth={1}
        />
      )}
      <polyline
        points={points}
        fill="none"
        stroke={strokeColor}
        strokeWidth={1.5}
        strokeLinejoin="round"
        strokeLinecap="round"
      />
    </svg>
  );
}
