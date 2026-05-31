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
      <div className="flex items-center justify-between gap-3">
        {linkTo ? (
          // Whole title is a link when the rail has a destination —
          // the underline grows L→R on hover (Netflix-style row
          // header) so the title visibly invites a click. Falls back
          // to a plain h2 when no destination is set (e.g. Trending,
          // which is a server-wide aggregate with no canonical "see
          // all" page yet).
          <Link
            to={linkTo}
            className="group/title inline-flex min-w-0 flex-col items-start"
          >
            <h2 className="max-w-full truncate text-lg font-semibold text-white transition-colors group-hover/title:text-white">
              {title}
            </h2>
            <span
              aria-hidden="true"
              className="block h-0.5 w-0 bg-accent transition-all duration-300 group-hover/title:w-full"
            />
          </Link>
        ) : (
          <h2 className="min-w-0 truncate text-lg font-semibold text-white">
            {title}
          </h2>
        )}
        {linkTo && (
          <Link
            to={linkTo}
            className="flex-shrink-0 text-xs text-white/40 transition-colors hover:text-white/70"
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
  //
  // En móvil la fila sangra a borde completo (`-mx-4` cancela el
  // `px-4` del contenedor de rails) y re-inseta las cards con
  // `px-4`, así la primera card queda alineada con el título pero el
  // scroll llega hasta el borde de la pantalla (patrón Netflix/Plex).
  // En desktop (`md:`) se desactiva — el contenedor ya da el gutter.
  return (
    <HorizontalScroller
      className="-mx-4 md:mx-0"
      paddingClassName="px-4 pb-2 md:px-0"
    >
      {children}
    </HorizontalScroller>
  );
}
