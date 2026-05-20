// HealthPill — pildora compacta que comunica "este subsistema esta
// OK / degradado / KO" en un row horizontal. Pensado para el "health
// strip" del dashboard admin, justo bajo el identity strip.
//
// Tres tonos: success / warning / error. La etiqueta del subsistema
// va inline; el dot lleva el color. El tooltip nativo (title) lleva
// el detalle largo para no ocupar espacio horizontal.

import type { ReactNode } from "react";

export type HealthTone = "success" | "warning" | "error" | "neutral";

interface HealthPillProps {
  label: string;
  tone: HealthTone;
  /** Detalle largo - aparece en title attribute. */
  detail?: string;
  /** Slot opcional para mas info inline (icono, valor). */
  trailing?: ReactNode;
}

const TONE_BG: Record<HealthTone, string> = {
  success: "bg-success/10 text-success",
  warning: "bg-warning/10 text-warning",
  error: "bg-error/10 text-error",
  neutral: "bg-bg-elevated text-text-muted",
};

const TONE_DOT: Record<HealthTone, string> = {
  success: "var(--color-success)",
  warning: "var(--color-warning)",
  error: "var(--color-error)",
  neutral: "var(--color-text-muted)",
};

export function HealthPill({ label, tone, detail, trailing }: HealthPillProps) {
  return (
    <span
      title={detail}
      className={[
        "inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-medium",
        TONE_BG[tone],
      ].join(" ")}
    >
      <span
        aria-hidden
        className="size-1.5 rounded-full"
        style={{ background: TONE_DOT[tone] }}
      />
      <span>{label}</span>
      {trailing && (
        <span className="ml-0.5 text-text-muted/80">{trailing}</span>
      )}
    </span>
  );
}
