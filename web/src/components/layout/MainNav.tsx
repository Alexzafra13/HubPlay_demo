import { useCallback, useEffect, useRef, useState } from "react";
import { NavLink, useLocation } from "react-router";
import { useTranslation } from "react-i18next";
import { motion, AnimatePresence } from "framer-motion";
import { ChevronDown, Tv as TvIcon, Users as UsersIcon } from "lucide-react";
import { useAuthStore } from "@/store/auth";
import { useAllPeerLibraries } from "@/api/hooks/federation";
import { useLibraries, useCollections } from "@/api/hooks/media";
import { useChannelFavorites } from "@/api/hooks/channels";
import type {
  FederationUnifiedLibrary,
  Library,
  CollectionListEntry,
  Channel,
} from "@/api/types";
import { MAIN_NAV, PEERS_NAV, type NavItem, type NavGroup } from "./navConfig";

// MainNav — desktop center bar. Renders MAIN_NAV in order; each
// `menu` item opens a dropdown panel below the trigger on hover or
// click (same behavior on keyboard with Enter/Space). Hover-intent
// keeps the panel from flickering: we delay open by 80 ms so a
// quick mouseover on the way to the search bar doesn't pop a panel,
// and delay close by 200 ms so the cursor can travel from trigger to
// panel without the panel disappearing mid-trip.
//
// Switching active panels (cursor moves from one trigger to another
// while a panel is open) happens immediately — once the user has
// committed to "I'm browsing the menu", treat further trigger hovers
// as fast switches, not new openings.

const HOVER_OPEN_DELAY_MS = 80;
const HOVER_CLOSE_DELAY_MS = 200;

export function MainNav() {
  const { user } = useAuthStore();
  const isAdmin = user?.role === "admin";
  const location = useLocation();

  // Peers dropdown is dynamic and admin-only. Hook always runs (rules
  // of hooks); we just gate the UI.
  const { data: peerLibs } = useAllPeerLibraries();
  const peerCount = peerLibs?.length ?? 0;
  const showPeers = isAdmin && peerCount > 0;

  // ── Open-panel coordination ───────────────────────────────────────
  // Single `openId` so two panels never overlap. Two timers: one for
  // delayed open, one for delayed close. Switching between triggers
  // while open bypasses the open delay (immediate switch).
  const [openId, setOpenId] = useState<string | null>(null);
  const openTimerRef = useRef<number | null>(null);
  const closeTimerRef = useRef<number | null>(null);

  const clearTimers = useCallback(() => {
    if (openTimerRef.current !== null) {
      window.clearTimeout(openTimerRef.current);
      openTimerRef.current = null;
    }
    if (closeTimerRef.current !== null) {
      window.clearTimeout(closeTimerRef.current);
      closeTimerRef.current = null;
    }
  }, []);

  const scheduleOpen = useCallback(
    (id: string) => {
      // If something is already open, switch immediately — the user is
      // already in "menu browsing" mode.
      if (openId !== null && openId !== id) {
        clearTimers();
        setOpenId(id);
        return;
      }
      if (openId === id) {
        clearTimers();
        return;
      }
      clearTimers();
      openTimerRef.current = window.setTimeout(() => {
        setOpenId(id);
      }, HOVER_OPEN_DELAY_MS);
    },
    [openId, clearTimers],
  );

  const scheduleClose = useCallback(() => {
    clearTimers();
    closeTimerRef.current = window.setTimeout(() => {
      setOpenId(null);
    }, HOVER_CLOSE_DELAY_MS);
  }, [clearTimers]);

  const closeNow = useCallback(() => {
    clearTimers();
    setOpenId(null);
  }, [clearTimers]);

  // Close when route changes (the user clicked a link inside the panel).
  useEffect(() => {
    closeNow();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [location.pathname, location.search]);

  // Escape closes from anywhere.
  useEffect(() => {
    if (openId === null) return;
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") {
        e.preventDefault();
        closeNow();
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [openId, closeNow]);

  useEffect(() => () => clearTimers(), [clearTimers]);

  // ── Active path detection ─────────────────────────────────────────
  // Items match by path prefix. "/" matches only the exact home route
  // so it doesn't claim every URL.
  const isItemActive = (item: NavItem) => {
    if (item.kind === "link" && item.end) {
      return location.pathname === item.to;
    }
    const base = item.to.split("?")[0];
    return location.pathname === base || location.pathname.startsWith(base + "/");
  };

  // ── Render ────────────────────────────────────────────────────────
  return (
    <nav
      aria-label="Main"
      className="hidden md:flex items-center gap-1 relative"
      onMouseLeave={scheduleClose}
    >
      {MAIN_NAV.map((item) => (
        <MainNavItem
          key={item.id}
          item={item}
          active={isItemActive(item)}
          isOpen={openId === item.id}
          onTriggerEnter={() => scheduleOpen(item.id)}
          onTriggerLeave={scheduleClose}
          onCloseImmediate={closeNow}
          onTriggerClick={() => {
            if (item.kind !== "menu") return;
            setOpenId((cur) => (cur === item.id ? null : item.id));
          }}
        />
      ))}

      {showPeers && (
        <PeersNavItem
          active={isItemActive(PEERS_NAV)}
          isOpen={openId === PEERS_NAV.id}
          peerLibs={peerLibs ?? []}
          onTriggerEnter={() => scheduleOpen(PEERS_NAV.id)}
          onTriggerLeave={scheduleClose}
          onCloseImmediate={closeNow}
          onTriggerClick={() => {
            setOpenId((cur) => (cur === PEERS_NAV.id ? null : PEERS_NAV.id));
          }}
        />
      )}
    </nav>
  );
}

// ─── Single nav item (link or menu trigger + panel) ─────────────────

interface MainNavItemProps {
  item: NavItem;
  active: boolean;
  isOpen: boolean;
  onTriggerEnter: () => void;
  onTriggerLeave: () => void;
  onTriggerClick: () => void;
  onCloseImmediate: () => void;
}

function MainNavItem({
  item,
  active,
  isOpen,
  onTriggerEnter,
  onTriggerLeave,
  onTriggerClick,
  onCloseImmediate,
}: MainNavItemProps) {
  const { t } = useTranslation();
  const label = t(item.labelKey);

  if (item.kind === "link") {
    return (
      <NavLink
        to={item.to}
        end={item.end}
        className={({ isActive }) =>
          [
            "relative flex items-center h-9 px-3 rounded-lg text-[13.5px] font-medium transition-colors",
            isActive || active
              ? "text-text-primary bg-bg-hover/60"
              : "text-text-secondary hover:text-text-primary hover:bg-bg-hover/40",
          ].join(" ")
        }
      >
        {label}
      </NavLink>
    );
  }

  return (
    <div
      className="relative"
      onMouseEnter={onTriggerEnter}
      onMouseLeave={onTriggerLeave}
    >
      <button
        type="button"
        onClick={onTriggerClick}
        aria-haspopup="menu"
        aria-expanded={isOpen}
        className={[
          "relative flex items-center gap-1.5 h-9 px-3 rounded-lg text-[13.5px] font-medium transition-colors",
          active || isOpen
            ? "text-text-primary bg-bg-hover/60"
            : "text-text-secondary hover:text-text-primary hover:bg-bg-hover/40",
        ].join(" ")}
      >
        <span className="relative inline-flex items-center gap-1.5">
          {label}
        </span>
        <ChevronDown
          className={[
            "h-3.5 w-3.5 transition-transform duration-200",
            isOpen ? "rotate-180" : "rotate-0",
          ].join(" ")}
          strokeWidth={1.7}
        />
      </button>

      <AnimatePresence>
        {isOpen && (
          <DropdownPanel
            sectionId={item.id}
            groups={item.groups}
            triggerLabel={label}
            onItemClick={onCloseImmediate}
          />
        )}
      </AnimatePresence>
    </div>
  );
}

// ─── Dropdown panel (shared between Movies / Series / TV en vivo) ───

function DropdownPanel({
  sectionId,
  groups,
  triggerLabel,
  onItemClick,
}: {
  sectionId: string;
  groups: NavGroup[];
  triggerLabel: string;
  onItemClick: () => void;
}) {
  const { t } = useTranslation();

  // Sections that get a third dynamic column (libraries, collections,
  // peers, favorite channels). The component renders its own empty
  // state, so we always reserve the column slot for these sections.
  const hasExtra =
    sectionId === "movies" ||
    sectionId === "series" ||
    sectionId === "live-tv";
  const totalCols = groups.length + (hasExtra ? 1 : 0);

  return (
    <motion.div
      initial={{ opacity: 0, y: -6, scale: 0.985 }}
      animate={{ opacity: 1, y: 0, scale: 1 }}
      exit={{ opacity: 0, y: -6, scale: 0.985 }}
      transition={{ duration: 0.14, ease: [0.32, 0.72, 0, 1] }}
      role="menu"
      aria-label={triggerLabel}
      className="absolute left-1/2 -translate-x-1/2 top-full mt-2 z-50 origin-top"
    >
      {/* Connector arrow — small visual anchor between trigger and panel. */}
      <span
        aria-hidden
        className="absolute left-1/2 -top-1.5 h-3 w-3 -translate-x-1/2 rotate-45 rounded-sm bg-bg-overlay border-l border-t border-border"
      />
      <div
        className="relative rounded-2xl border border-border bg-bg-overlay/95 backdrop-blur-2xl shadow-2xl shadow-black/50 overflow-hidden"
        style={{ minWidth: totalCols >= 3 ? 720 : 460 }}
      >
        <div
          className="grid gap-x-8 gap-y-2 p-5"
          style={{
            gridTemplateColumns: `repeat(${Math.min(totalCols, 3)}, minmax(0, 1fr))`,
          }}
        >
          {groups.map((g) => (
            <div key={g.labelKey} className="min-w-[180px]">
              <p className="px-2 mb-2 text-[10px] font-semibold uppercase tracking-[0.14em] text-text-muted">
                {t(g.labelKey)}
              </p>
              <ul className="flex flex-col">
                {g.links.map((link) => (
                  <li key={link.to}>
                    <NavLink
                      to={link.to}
                      onClick={onItemClick}
                      role="menuitem"
                      className="block px-2 py-1.5 rounded-md text-[13px] text-text-secondary hover:text-text-primary hover:bg-bg-hover transition-colors"
                    >
                      {t(link.labelKey)}
                    </NavLink>
                  </li>
                ))}
              </ul>
            </div>
          ))}
          {hasExtra && (
            <SectionExtra sectionId={sectionId} onItemClick={onItemClick} />
          )}
        </div>
      </div>
    </motion.div>
  );
}

// ─── Peers dropdown (admin-only, dynamic) ───────────────────────────

function PeersNavItem({
  active,
  isOpen,
  peerLibs,
  onTriggerEnter,
  onTriggerLeave,
  onTriggerClick,
  onCloseImmediate,
}: {
  active: boolean;
  isOpen: boolean;
  peerLibs: FederationUnifiedLibrary[];
  onTriggerEnter: () => void;
  onTriggerLeave: () => void;
  onTriggerClick: () => void;
  onCloseImmediate: () => void;
}) {
  const { t } = useTranslation();
  const label = t(PEERS_NAV.labelKey);

  // Group libraries by peer; preserves first-seen order.
  const grouped = new Map<string, { name: string; libs: FederationUnifiedLibrary[] }>();
  for (const row of peerLibs) {
    const entry = grouped.get(row.peer_id);
    if (entry) entry.libs.push(row);
    else grouped.set(row.peer_id, { name: row.peer_name, libs: [row] });
  }

  return (
    <div
      className="relative"
      onMouseEnter={onTriggerEnter}
      onMouseLeave={onTriggerLeave}
    >
      <button
        type="button"
        onClick={onTriggerClick}
        aria-haspopup="menu"
        aria-expanded={isOpen}
        className={[
          "relative flex items-center gap-1.5 h-9 px-3 rounded-lg text-[13.5px] font-medium transition-colors",
          active || isOpen
            ? "text-text-primary bg-bg-hover/60"
            : "text-text-secondary hover:text-text-primary hover:bg-bg-hover/40",
        ].join(" ")}
      >
        {label}
        <ChevronDown
          className={[
            "h-3.5 w-3.5 transition-transform duration-200",
            isOpen ? "rotate-180" : "rotate-0",
          ].join(" ")}
          strokeWidth={1.7}
        />
      </button>

      <AnimatePresence>
        {isOpen && (
          <motion.div
            initial={{ opacity: 0, y: -6, scale: 0.985 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: -6, scale: 0.985 }}
            transition={{ duration: 0.14, ease: [0.32, 0.72, 0, 1] }}
            role="menu"
            aria-label={label}
            className="absolute left-1/2 -translate-x-1/2 top-full mt-2 z-50 origin-top"
          >
            <span
              aria-hidden
              className="absolute left-1/2 -top-1.5 h-3 w-3 -translate-x-1/2 rotate-45 rounded-sm bg-bg-overlay border-l border-t border-border"
            />
            <div
              className="relative rounded-2xl border border-border bg-bg-overlay/95 backdrop-blur-2xl shadow-2xl shadow-black/50 overflow-hidden"
              style={{ minWidth: 320, maxWidth: 380 }}
            >
              <div className="p-4 space-y-3 max-h-[60vh] overflow-y-auto">
                {Array.from(grouped.entries()).map(([peerId, { name, libs }]) => (
                  <div key={peerId}>
                    <p
                      className="px-2 mb-1.5 text-[10px] font-semibold uppercase tracking-[0.14em] text-text-muted truncate"
                      title={name}
                    >
                      {name}
                    </p>
                    <ul className="flex flex-col">
                      {libs.map((lib) => (
                        <li key={lib.library_id}>
                          <NavLink
                            to={`/peers/${peerId}/libraries/${lib.library_id}`}
                            onClick={onCloseImmediate}
                            role="menuitem"
                            className="block px-2 py-1.5 rounded-md text-[13px] text-text-secondary hover:text-text-primary hover:bg-bg-hover transition-colors truncate"
                          >
                            {lib.library_name}
                          </NavLink>
                        </li>
                      ))}
                    </ul>
                  </div>
                ))}
              </div>
              <NavLink
                to={PEERS_NAV.to}
                onClick={onCloseImmediate}
                role="menuitem"
                className="flex items-center justify-between px-5 py-3 text-[12.5px] font-semibold text-accent border-t border-border-subtle hover:bg-bg-hover transition-colors"
              >
                <span>{t("navMenu.peers.viewAll", { defaultValue: "Ver todos" })}</span>
                <span aria-hidden>→</span>
              </NavLink>
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

// ─── Third-column extras (libraries + collections + peers + favs) ───
//
// Each section gets its own component so the data hooks only fire when
// that specific dropdown is open (panel mounts on hover, unmounts on
// close via AnimatePresence). TanStack Query staleTime keeps repeat
// opens cheap.

function SectionExtra({
  sectionId,
  onItemClick,
}: {
  sectionId: string;
  onItemClick: () => void;
}) {
  if (sectionId === "movies") return <MoviesExtra onItemClick={onItemClick} />;
  if (sectionId === "series") return <SeriesExtra onItemClick={onItemClick} />;
  if (sectionId === "live-tv") return <LiveTvExtra onItemClick={onItemClick} />;
  return null;
}

const EXTRA_LIST_LIMIT = 6;

function ExtraColumn({ children }: { children: React.ReactNode }) {
  // No fixed min-width — the grid track (panel 720 - p-5 - gaps,
  // divided by 3 ≈ 205px) sets the column width. A static min-w would
  // overflow when bigger than the track and clip the row's hover
  // background corners against the panel's overflow-hidden.
  return <div className="min-w-0 flex flex-col gap-3">{children}</div>;
}

function ExtraSubheader({ labelKey }: { labelKey: string }) {
  const { t } = useTranslation();
  return (
    <p className="px-2 mb-1 text-[10px] font-semibold uppercase tracking-[0.14em] text-text-muted">
      {t(labelKey)}
    </p>
  );
}

function ExtraRow({
  to,
  onClick,
  leading,
  label,
  trailing,
}: {
  to: string;
  onClick: () => void;
  leading?: React.ReactNode;
  label: string;
  trailing?: React.ReactNode;
}) {
  return (
    <NavLink
      to={to}
      onClick={onClick}
      role="menuitem"
      className="flex items-center gap-2.5 px-2 py-1.5 rounded-md text-[13px] text-text-secondary hover:text-text-primary hover:bg-bg-hover transition-colors min-w-0"
    >
      {leading}
      <span className="truncate flex-1">{label}</span>
      {trailing}
    </NavLink>
  );
}

function ExtraEmpty({ labelKey }: { labelKey: string }) {
  const { t } = useTranslation();
  return (
    <p className="px-2 py-1 text-[12px] text-text-muted italic">
      {t(labelKey)}
    </p>
  );
}

function ExtraSeeAll({
  to,
  labelKey,
  onClick,
}: {
  to: string;
  labelKey: string;
  onClick: () => void;
}) {
  const { t } = useTranslation();
  return (
    <NavLink
      to={to}
      onClick={onClick}
      role="menuitem"
      className="mt-0.5 flex items-center justify-between px-2 py-1.5 rounded-md text-[12px] font-medium text-accent hover:bg-bg-hover transition-colors"
    >
      <span>{t(labelKey)}</span>
      <span aria-hidden>→</span>
    </NavLink>
  );
}

// ─── Movies extra: libraries + collections + peers ──────────────────

function MoviesExtra({ onItemClick }: { onItemClick: () => void }) {
  const { user } = useAuthStore();
  const isAdmin = user?.role === "admin";

  const { data: libraries } = useLibraries({ staleTime: 60_000 });
  const { data: collectionsData } = useCollections({ staleTime: 5 * 60_000 });
  const { data: peerLibs } = useAllPeerLibraries();

  const movieLibs = (libraries ?? []).filter(
    (l: Library) => l.content_type === "movies",
  );
  const collections = (collectionsData?.collections ?? []).slice(
    0,
    EXTRA_LIST_LIMIT,
  );
  const moviePeerLibs = isAdmin
    ? (peerLibs ?? []).filter(
        (l: FederationUnifiedLibrary) => l.content_type === "movies",
      )
    : [];

  const nothing =
    movieLibs.length === 0 &&
    collections.length === 0 &&
    moviePeerLibs.length === 0;

  return (
    <ExtraColumn>
      {movieLibs.length > 0 && (
        <div>
          <ExtraSubheader labelKey="navMenu.libraries" />
          <ul className="flex flex-col">
            {movieLibs.slice(0, EXTRA_LIST_LIMIT).map((lib) => (
              <li key={lib.id}>
                <ExtraRow
                  to={`/movies?library_id=${encodeURIComponent(lib.id)}`}
                  onClick={onItemClick}
                  label={lib.name}
                />
              </li>
            ))}
          </ul>
        </div>
      )}

      {collections.length > 0 && (
        <div>
          <ExtraSubheader labelKey="navMenu.collections" />
          <ul className="flex flex-col">
            {collections.map((c: CollectionListEntry) => (
              <li key={c.id}>
                <ExtraRow
                  to={`/collections/${c.id}`}
                  onClick={onItemClick}
                  leading={<CollectionPoster url={c.poster_url} alt={c.name} />}
                  label={c.name}
                />
              </li>
            ))}
          </ul>
          <ExtraSeeAll
            to="/collections"
            labelKey="navMenu.seeAll.collections"
            onClick={onItemClick}
          />
        </div>
      )}

      {moviePeerLibs.length > 0 && (
        <div>
          <ExtraSubheader labelKey="navMenu.shared" />
          <ul className="flex flex-col">
            {moviePeerLibs.slice(0, EXTRA_LIST_LIMIT).map((lib) => (
              <li key={`${lib.peer_id}:${lib.library_id}`}>
                <ExtraRow
                  to={`/peers/${lib.peer_id}/libraries/${lib.library_id}`}
                  onClick={onItemClick}
                  leading={
                    <UsersIcon
                      className="h-3.5 w-3.5 text-text-muted flex-shrink-0"
                      strokeWidth={1.7}
                    />
                  }
                  label={`${lib.library_name} · ${lib.peer_name}`}
                />
              </li>
            ))}
          </ul>
        </div>
      )}

      {nothing && <ExtraEmpty labelKey="navMenu.empty.movies" />}
    </ExtraColumn>
  );
}

// ─── Series extra: libraries + peers ────────────────────────────────

function SeriesExtra({ onItemClick }: { onItemClick: () => void }) {
  const { user } = useAuthStore();
  const isAdmin = user?.role === "admin";

  const { data: libraries } = useLibraries({ staleTime: 60_000 });
  const { data: peerLibs } = useAllPeerLibraries();

  const seriesLibs = (libraries ?? []).filter(
    (l: Library) => l.content_type === "shows",
  );
  const seriesPeerLibs = isAdmin
    ? (peerLibs ?? []).filter(
        (l: FederationUnifiedLibrary) => l.content_type === "shows",
      )
    : [];

  const nothing = seriesLibs.length === 0 && seriesPeerLibs.length === 0;

  return (
    <ExtraColumn>
      {seriesLibs.length > 0 && (
        <div>
          <ExtraSubheader labelKey="navMenu.libraries" />
          <ul className="flex flex-col">
            {seriesLibs.slice(0, EXTRA_LIST_LIMIT).map((lib) => (
              <li key={lib.id}>
                <ExtraRow
                  to={`/series?library_id=${encodeURIComponent(lib.id)}`}
                  onClick={onItemClick}
                  label={lib.name}
                />
              </li>
            ))}
          </ul>
        </div>
      )}

      {seriesPeerLibs.length > 0 && (
        <div>
          <ExtraSubheader labelKey="navMenu.shared" />
          <ul className="flex flex-col">
            {seriesPeerLibs.slice(0, EXTRA_LIST_LIMIT).map((lib) => (
              <li key={`${lib.peer_id}:${lib.library_id}`}>
                <ExtraRow
                  to={`/peers/${lib.peer_id}/libraries/${lib.library_id}`}
                  onClick={onItemClick}
                  leading={
                    <UsersIcon
                      className="h-3.5 w-3.5 text-text-muted flex-shrink-0"
                      strokeWidth={1.7}
                    />
                  }
                  label={`${lib.library_name} · ${lib.peer_name}`}
                />
              </li>
            ))}
          </ul>
        </div>
      )}

      {nothing && <ExtraEmpty labelKey="navMenu.empty.series" />}
    </ExtraColumn>
  );
}

// ─── LiveTV extra: favorite channels (click → open the player) ──────

function LiveTvExtra({ onItemClick }: { onItemClick: () => void }) {
  const { data: favorites } = useChannelFavorites({ staleTime: 60_000 });
  const top = (favorites ?? []).slice(0, EXTRA_LIST_LIMIT);

  return (
    <ExtraColumn>
      <div>
        <ExtraSubheader labelKey="navMenu.favorites" />
        {top.length > 0 ? (
          <>
            <ul className="flex flex-col">
              {top.map((ch: Channel) => (
                <li key={ch.id}>
                  <ExtraRow
                    to={`/live-tv?channel=${encodeURIComponent(ch.id)}`}
                    onClick={onItemClick}
                    leading={<ChannelLogo url={ch.logo_url} alt={ch.name} />}
                    label={ch.name}
                  />
                </li>
              ))}
            </ul>
            <ExtraSeeAll
              to="/live-tv?tab=favorites"
              labelKey="navMenu.seeAll.favorites"
              onClick={onItemClick}
            />
          </>
        ) : (
          <ExtraEmpty labelKey="navMenu.empty.favorites" />
        )}
      </div>
    </ExtraColumn>
  );
}

// ─── Visual leadings (mini-poster, channel logo) ────────────────────

function CollectionPoster({ url, alt }: { url?: string; alt: string }) {
  if (!url) {
    return (
      <span
        aria-hidden
        className="flex-shrink-0 h-9 w-6 rounded-sm bg-bg-hover/60 flex items-center justify-center text-[9px] text-text-muted"
      >
        ◇
      </span>
    );
  }
  return (
    <img
      src={url}
      alt={alt}
      loading="lazy"
      className="flex-shrink-0 h-9 w-6 rounded-sm object-cover"
    />
  );
}

function ChannelLogo({ url, alt }: { url: string | null; alt: string }) {
  if (!url) {
    return (
      <span
        aria-hidden
        className="flex-shrink-0 h-6 w-6 rounded bg-bg-hover/60 flex items-center justify-center"
      >
        <TvIcon className="h-3 w-3 text-text-muted" strokeWidth={1.7} />
      </span>
    );
  }
  return (
    <img
      src={url}
      alt={alt}
      loading="lazy"
      className="flex-shrink-0 h-6 w-6 rounded object-contain bg-bg-hover/40 p-0.5"
    />
  );
}
