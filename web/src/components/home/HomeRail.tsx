// HomeRail — the shared shell every home rail wraps itself in.
//
// One section header + a horizontally scrolling row underneath. Owns
// nothing about data: each rail component decides its own loading /
// empty / loaded state and feeds either skeleton tiles or real cards
// into the row. This keeps the rails decoupled — the Home shell
// doesn't have to coordinate which rail finished loading first.

import type { ReactNode } from "react";
import { Link } from "react-router";
import { useTranslation } from "react-i18next";
import { HorizontalScroller } from "@/components/common";

interface HomeRailProps {
  title: string;
  /** Optional "see all" link target rendered next to the title. */
  linkTo?: string;
  children: ReactNode;
}

export function HomeRail({ title, linkTo, children }: HomeRailProps) {
  const { t } = useTranslation();
  return (
    <section className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        {linkTo ? (
          // Whole title is a link when the rail has a destination —
          // the underline grows L→R on hover (Netflix-style row
          // header) so the title visibly invites a click. Falls back
          // to a plain h2 when no destination is set (e.g. Trending,
          // which is a server-wide aggregate with no canonical "see
          // all" page yet).
          <Link
            to={linkTo}
            className="group/title inline-flex flex-col items-start"
          >
            <h2 className="text-lg font-semibold text-white group-hover/title:text-white transition-colors">
              {title}
            </h2>
            <span
              aria-hidden="true"
              className="block h-0.5 w-0 bg-accent transition-all duration-300 group-hover/title:w-full"
            />
          </Link>
        ) : (
          <h2 className="text-lg font-semibold text-white">{title}</h2>
        )}
        {linkTo && (
          <Link
            to={linkTo}
            className="text-xs text-white/40 hover:text-white/70 transition-colors"
          >
            {t("common.seeAll")}
          </Link>
        )}
      </div>
      <ScrollRow>{children}</ScrollRow>
    </section>
  );
}

function ScrollRow({ children }: { children: ReactNode }) {
  // Hidden scrollbar everywhere — desktop gets the chevron arrows
  // on hover (HorizontalScroller), mobile keeps native swipe-to-
  // scroll. The Plex playbook: never advertise the scrollbar.
  return <HorizontalScroller>{children}</HorizontalScroller>;
}
