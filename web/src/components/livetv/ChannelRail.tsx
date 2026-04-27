import { useRef, type ReactNode } from "react";
import { useTranslation } from "react-i18next";

interface ChannelRailProps {
  title: string;
  count?: number;
  /** Optional one-liner under the title. Used by the "Apagados" rail to
   * explain its treatment without overloading the header with chrome. */
  subtitle?: string;
  /** When provided, shows a "Ver todo" action that calls this. */
  onSeeAll?: () => void;
  /** Channel cards (or other items) to render in the scrolling track. */
  children: ReactNode;
}

/**
 * ChannelRail — a horizontally scrolling strip of channel cards with
 * a title header and optional navigation arrows. Arrows are hidden on
 * touch devices (`@media (hover: none)`) where swiping is more natural.
 *
 * The scroll distance is tuned to ~80% of the container width so each
 * click reveals a fresh page while keeping the trailing card as an
 * anchor. Tracked in a ref rather than a callback because we never
 * recompute it and hooks discourage reading DOM layout eagerly.
 */
export function ChannelRail({
  title,
  count,
  subtitle,
  onSeeAll,
  children,
}: ChannelRailProps) {
  const { t } = useTranslation();
  const trackRef = useRef<HTMLDivElement>(null);

  const scroll = (direction: 1 | -1) => {
    const el = trackRef.current;
    if (!el) return;
    const delta = Math.round(el.clientWidth * 0.8) * direction;
    el.scrollBy({ left: delta, behavior: "smooth" });
  };

  return (
    <section className="flex flex-col gap-3">
      <header className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          {/* When `onSeeAll` is wired the whole heading becomes the
              same affordance as the "Ver todo" button on the right —
              the user expects "click the section title to drill in".
              We keep both: the title for natural reading + the explicit
              CTA for users who scan the right edge. The pill count
              tags along inside so the click target is generous. */}
          {onSeeAll ? (
            <button
              type="button"
              onClick={onSeeAll}
              className="group/title flex max-w-full items-center gap-2 text-left"
            >
              <h2 className="flex items-center gap-2 truncate text-base font-semibold text-tv-fg-0 transition-colors group-hover/title:text-tv-accent">
                <span className="truncate">{title}</span>
                {count !== undefined && (
                  <span className="shrink-0 rounded-full bg-tv-bg-2 px-2 py-0.5 font-mono text-[10px] font-medium tabular-nums text-tv-fg-2">
                    {count}
                  </span>
                )}
                <span
                  aria-hidden="true"
                  className="text-tv-fg-3 opacity-0 transition-opacity group-hover/title:opacity-100"
                >
                  →
                </span>
              </h2>
            </button>
          ) : (
            <h2 className="flex items-center gap-2 text-base font-semibold text-tv-fg-0">
              {title}
              {count !== undefined && (
                <span className="rounded-full bg-tv-bg-2 px-2 py-0.5 font-mono text-[10px] font-medium tabular-nums text-tv-fg-2">
                  {count}
                </span>
              )}
            </h2>
          )}
          {subtitle ? (
            <p className="mt-0.5 text-xs text-tv-fg-3">{subtitle}</p>
          ) : null}
        </div>

        <div className="flex items-center gap-1.5 shrink-0">
          {onSeeAll && (
            <button
              type="button"
              onClick={onSeeAll}
              className="rounded-full px-3 py-1 text-xs font-medium text-tv-fg-2 transition-colors hover:text-tv-accent"
            >
              {t("common.seeAll", { defaultValue: "Ver todo" })} →
            </button>
          )}
          <div className="hidden items-center gap-1 [@media(hover:hover)]:flex">
            <RailNavButton direction="left" onClick={() => scroll(-1)} />
            <RailNavButton direction="right" onClick={() => scroll(1)} />
          </div>
        </div>
      </header>

      <div
        ref={trackRef}
        className="grid auto-cols-[260px] grid-flow-col gap-3 overflow-x-auto pb-1 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
      >
        {children}
      </div>
    </section>
  );
}

function RailNavButton({
  direction,
  onClick,
}: {
  direction: "left" | "right";
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={direction === "left" ? "Anterior" : "Siguiente"}
      className="flex h-8 w-8 items-center justify-center rounded-full border border-tv-line bg-tv-bg-1 text-tv-fg-1 transition-colors hover:bg-tv-bg-2 hover:text-tv-fg-0"
    >
      <svg
        width="14"
        height="14"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        {direction === "left" ? (
          <polyline points="15 18 9 12 15 6" />
        ) : (
          <polyline points="9 18 15 12 9 6" />
        )}
      </svg>
    </button>
  );
}
