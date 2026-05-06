import {
  Home as HomeIcon,
  Film,
  Tv,
  Radio,
  Users,
  type LucideIcon,
} from "lucide-react";

// navConfig — single source of truth for the topbar nav. The render
// layer (MainNav on desktop, MobileDrawer on mobile) consumes this
// shape; adding/removing a section or dropdown link only requires a
// change here. Per-section gating (e.g. peers admin-only) is left to
// the consumer because it depends on auth state.

export type NavGroup = {
  /** i18n key for the column header inside the dropdown panel. */
  labelKey: string;
  links: NavLink[];
};

export type NavLink = {
  /** i18n key for the link's visible label. */
  labelKey: string;
  /** Absolute path or full search-string-bearing path. */
  to: string;
};

export type NavItem =
  | {
      kind: "link";
      id: string;
      to: string;
      labelKey: string;
      icon: LucideIcon;
      end?: boolean;
    }
  | {
      kind: "menu";
      id: string;
      to: string;
      labelKey: string;
      icon: LucideIcon;
      groups: NavGroup[];
    };

// Genre presets for movies — labels are HubPlay-style Spanish names,
// `to` uses the canonical English genre token the backend stores
// (matches what TMDb provides). Order is rough popularity / breadth.
const MOVIE_GENRES: NavLink[] = [
  { labelKey: "navMenu.genre.action", to: "/movies?genre=Action" },
  { labelKey: "navMenu.genre.adventure", to: "/movies?genre=Adventure" },
  { labelKey: "navMenu.genre.animation", to: "/movies?genre=Animation" },
  { labelKey: "navMenu.genre.comedy", to: "/movies?genre=Comedy" },
  { labelKey: "navMenu.genre.drama", to: "/movies?genre=Drama" },
  { labelKey: "navMenu.genre.scifi", to: "/movies?genre=Science Fiction" },
  { labelKey: "navMenu.genre.horror", to: "/movies?genre=Horror" },
  { labelKey: "navMenu.genre.thriller", to: "/movies?genre=Thriller" },
];

const SERIES_GENRES: NavLink[] = [
  { labelKey: "navMenu.genre.action", to: "/series?genre=Action" },
  { labelKey: "navMenu.genre.animation", to: "/series?genre=Animation" },
  { labelKey: "navMenu.genre.comedy", to: "/series?genre=Comedy" },
  { labelKey: "navMenu.genre.drama", to: "/series?genre=Drama" },
  { labelKey: "navMenu.genre.scifi", to: "/series?genre=Science Fiction" },
  { labelKey: "navMenu.genre.documentary", to: "/series?genre=Documentary" },
  { labelKey: "navMenu.genre.kids", to: "/series?genre=Kids" },
  { labelKey: "navMenu.genre.crime", to: "/series?genre=Crime" },
];

// LiveTV categories use the same `cat=` token the page already
// understands (see CategoryFilter in LiveTV.tsx). Keep this list in
// sync with that union if it grows.
const LIVETV_CATEGORIES: NavLink[] = [
  { labelKey: "navMenu.live.general", to: "/live-tv?cat=general" },
  { labelKey: "navMenu.live.news", to: "/live-tv?cat=news" },
  { labelKey: "navMenu.live.sports", to: "/live-tv?cat=sports" },
  { labelKey: "navMenu.live.movies", to: "/live-tv?cat=movies" },
  { labelKey: "navMenu.live.music", to: "/live-tv?cat=music" },
  { labelKey: "navMenu.live.kids", to: "/live-tv?cat=kids" },
  { labelKey: "navMenu.live.documentaries", to: "/live-tv?cat=documentaries" },
  { labelKey: "navMenu.live.international", to: "/live-tv?cat=international" },
];

// MAIN_NAV — the desktop center bar (and mobile drawer). Items render
// in declared order. `to` on a "menu" item is the route the user
// lands on when they click the trigger label itself (vs. a link
// inside the dropdown).
export const MAIN_NAV: NavItem[] = [
  {
    kind: "link",
    id: "home",
    to: "/",
    end: true,
    labelKey: "nav.home",
    icon: HomeIcon,
  },
  {
    kind: "menu",
    id: "movies",
    to: "/movies",
    labelKey: "nav.movies",
    icon: Film,
    groups: [
      {
        labelKey: "navMenu.explore",
        links: [
          { labelKey: "navMenu.movies.all", to: "/movies" },
          { labelKey: "navMenu.movies.recentlyAdded", to: "/movies?sort=added" },
          { labelKey: "navMenu.movies.byYear", to: "/movies?sort=year" },
          { labelKey: "navMenu.movies.byTitle", to: "/movies?sort=title" },
        ],
      },
      {
        labelKey: "navMenu.genres",
        links: MOVIE_GENRES,
      },
    ],
  },
  {
    kind: "menu",
    id: "series",
    to: "/series",
    labelKey: "nav.series",
    icon: Tv,
    groups: [
      {
        labelKey: "navMenu.explore",
        links: [
          { labelKey: "navMenu.series.all", to: "/series" },
          { labelKey: "navMenu.series.recentlyAdded", to: "/series?sort=added" },
          { labelKey: "navMenu.series.byYear", to: "/series?sort=year" },
          { labelKey: "navMenu.series.byTitle", to: "/series?sort=title" },
        ],
      },
      {
        labelKey: "navMenu.genres",
        links: SERIES_GENRES,
      },
    ],
  },
  {
    kind: "menu",
    id: "live-tv",
    to: "/live-tv",
    labelKey: "nav.liveTV",
    icon: Radio,
    groups: [
      {
        labelKey: "navMenu.views",
        links: [
          { labelKey: "navMenu.live.now", to: "/live-tv?tab=now" },
          { labelKey: "navMenu.live.discover", to: "/live-tv?tab=discover" },
          { labelKey: "navMenu.live.guide", to: "/live-tv?tab=guide" },
          { labelKey: "navMenu.live.favorites", to: "/live-tv?tab=favorites" },
        ],
      },
      {
        labelKey: "navMenu.categories",
        links: LIVETV_CATEGORIES,
      },
    ],
  },
];

// PEERS_NAV — admin-only entry that's appended to MAIN_NAV when the
// current user is an admin AND has at least one paired peer. The
// dropdown is dynamic (built from `useAllPeerLibraries`), so it lives
// outside this static config and is rendered directly by MainNav.
export const PEERS_NAV: NavItem = {
  kind: "link",
  id: "peers",
  to: "/peers",
  labelKey: "nav.peers",
  icon: Users,
};
