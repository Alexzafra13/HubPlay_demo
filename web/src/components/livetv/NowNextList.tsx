import type { ReactNode } from "react";

interface NowNextListProps {
  title: string;
  count?: number;
  subtitle?: string;
  /** Optional "see all" affordance shown in the section header. */
  onSeeAll?: () => void;
  seeAllLabel?: string;
  children: ReactNode;
}

/**
 * NowNextList — a titled section that frames a stack of NowNextRow
 * children inside a single bordered card with hairline dividers. The
 * "guide list" container for the Live TV "En directo" surface; replaces
 * the horizontal logo-card rail with a denser, more legible vertical
 * guide.
 */
export function NowNextList({
  title,
  count,
  subtitle,
  onSeeAll,
  seeAllLabel,
  children,
}: NowNextListProps) {
  return (
    <section className="flex flex-col gap-3">
      <header className="flex items-baseline justify-between gap-3">
        <div className="flex min-w-0 flex-col">
          <h2 className="flex items-center gap-2 text-base font-semibold text-tv-fg-0">
            <span className="truncate">{title}</span>
            {typeof count === "number" ? (
              <span className="rounded-full bg-tv-bg-2 px-2 py-0.5 font-mono text-[10px] font-medium tabular-nums text-tv-fg-2">
                {count}
              </span>
            ) : null}
          </h2>
          {subtitle ? (
            <p className="mt-0.5 truncate text-xs text-tv-fg-3">{subtitle}</p>
          ) : null}
        </div>
        {onSeeAll ? (
          <button
            type="button"
            onClick={onSeeAll}
            className="flex-none rounded-full border border-tv-line px-3 py-1 text-xs font-medium text-tv-fg-2 transition-colors hover:border-tv-accent/50 hover:text-tv-fg-0"
          >
            {seeAllLabel ?? "Ver todo"}
          </button>
        ) : null}
      </header>
      <div className="divide-y divide-tv-line overflow-hidden rounded-tv-lg border border-tv-line bg-[linear-gradient(180deg,var(--tv-bg-1),#0c1116)]">
        {children}
      </div>
    </section>
  );
}
