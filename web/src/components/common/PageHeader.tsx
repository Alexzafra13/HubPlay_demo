// HMR caveat: this file exports a component AND a type consumed by
// other modules. Splitting them into a separate file would gain
// Fast Refresh but cost a per-page edit shape that's worse than
// the (mild) HMR limitation.
/* eslint-disable react-refresh/only-export-components */
import type { ReactNode } from "react";
import { Link } from "react-router";
import { ChevronRight } from "lucide-react";

// PageHeader — single source of truth for the visual rhythm at the top
// of every authenticated route. Title + optional subtitle on the left,
// optional CTA(s) on the right, and an optional breadcrumb above. The
// trailing thin border is what makes admin pages feel "anchored" to a
// section instead of floating in a content blob.

interface BreadcrumbItem {
  label: string;
  to?: string;
}

interface PageHeaderProps {
  title: string;
  subtitle?: string;
  breadcrumbs?: BreadcrumbItem[];
  actions?: ReactNode;
  /** Optional eyebrow above the title (e.g. "ADMINISTRACIÓN"). */
  eyebrow?: string;
  /** When true the title row is denser — used inside Sheets/Modals. */
  compact?: boolean;
}

export function PageHeader({
  title,
  subtitle,
  breadcrumbs,
  actions,
  eyebrow,
  compact,
}: PageHeaderProps) {
  return (
    <header
      className={[
        "flex flex-col gap-3 border-b border-border-subtle",
        compact ? "py-3" : "py-5 md:py-6",
      ].join(" ")}
    >
      {breadcrumbs && breadcrumbs.length > 0 && (
        <Breadcrumbs items={breadcrumbs} />
      )}
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="min-w-0 flex-1">
          {eyebrow && (
            <p className="text-[10px] font-semibold uppercase tracking-[0.14em] text-text-muted mb-1.5">
              {eyebrow}
            </p>
          )}
          <h1
            className={[
              "font-semibold tracking-tight text-text-primary truncate",
              compact ? "text-lg" : "text-2xl md:text-[26px]",
            ].join(" ")}
            style={{ letterSpacing: "-0.015em" }}
          >
            {title}
          </h1>
          {subtitle && (
            <p className="mt-1 text-sm text-text-secondary line-clamp-2 max-w-prose">
              {subtitle}
            </p>
          )}
        </div>
        {actions && (
          <div className="flex flex-shrink-0 items-center gap-2">{actions}</div>
        )}
      </div>
    </header>
  );
}

function Breadcrumbs({ items }: { items: BreadcrumbItem[] }) {
  return (
    <nav aria-label="Breadcrumb" className="flex items-center gap-1.5 text-[12px] text-text-muted">
      {items.map((item, i) => {
        const isLast = i === items.length - 1;
        // Key estable: el label (visible al usuario) es único dentro de
        // la breadcrumb. Si no, lo combinamos con el destino.
        return (
          <span key={item.label + (item.to ?? "")} className="flex items-center gap-1.5">
            {item.to && !isLast ? (
              <Link
                to={item.to}
                className="hover:text-text-secondary transition-colors"
              >
                {item.label}
              </Link>
            ) : (
              <span className={isLast ? "text-text-secondary" : ""}>
                {item.label}
              </span>
            )}
            {!isLast && (
              <ChevronRight className="size-3 opacity-60" strokeWidth={1.6} />
            )}
          </span>
        );
      })}
    </nav>
  );
}

