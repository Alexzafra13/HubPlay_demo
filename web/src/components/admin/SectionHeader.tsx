// SectionHeader — the editorial-block header used across the admin
// pages (Sistema, Resumen, …). An icon-tinted square + title + a
// one-line subhead, with optional trailing slot for a value or
// action. Cheap consistency mechanism: every section on every admin
// page reads with the same rhythm regardless of what's inside.
//
// Lives under components/admin/ rather than common/ because the
// shape (icon left, subtle subhead, no card chrome) is admin-
// specific — the public surfaces use a different heading style.

import type { ComponentType, ReactNode } from "react";

interface SectionHeaderProps {
  icon: ComponentType<{ className?: string }>;
  title: string;
  subtitle?: string;
  /** Optional right-aligned content (totals, status pills, count
   *  badges). Sits on the same baseline as the title so it reads
   *  as a metadata caption, not as a separate widget. */
  trailing?: ReactNode;
}

export function SectionHeader({
  icon: Icon,
  title,
  subtitle,
  trailing,
}: SectionHeaderProps) {
  return (
    <header className="flex items-start gap-3">
      <div className="rounded-md bg-bg-elevated p-2 text-text-secondary">
        <Icon className="h-4 w-4" />
      </div>
      <div className="flex-1 min-w-0">
        <h2 className="text-sm font-semibold text-text-primary">{title}</h2>
        {subtitle && (
          <p className="mt-0.5 text-xs text-text-muted">{subtitle}</p>
        )}
      </div>
      {trailing && <div className="flex-none">{trailing}</div>}
    </header>
  );
}
