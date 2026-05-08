// Home — the configurable home page.
//
// The shell is data-driven: the hero stays at the top (curated by
// `HeroBanner` from continue-watching + latest with intent tags so
// each slide tells the user *why* it's there), and the rails below
// are rendered from the user's saved home layout. The list of rails
// is fetched via /me/home/layout, which also tells us which
// library-scoped "Latest in <Library>" rails to mount and what title
// to show. New libraries are reconciled in server-side (always
// visible by default) so the home page never goes empty after a
// fresh install.
//
// Each rail owns its own data fetch + skeleton + empty-state, so
// this page only orchestrates ordering. Adding a new rail type later
// is a one-line case in `renderSection`.

import { useTranslation } from "react-i18next";
import {
  useContinueWatching,
  useHomeLayout,
  useLatestItems,
} from "@/api/hooks";
import type { HomeSection } from "@/api/types";
import {
  ContinueWatchingRail,
  HeroBanner,
  LatestInLibraryRail,
  LiveNowRail,
  NextUpRail,
  PeerRecentRail,
  PeerContinueWatchingRail,
  TrendingRail,
} from "@/components/home";

// ─── Layout-driven section dispatch ───────────────────────────────────────

function renderSection(s: HomeSection) {
  if (!s.visible) return null;
  switch (s.type) {
    case "continue_watching":
      return <ContinueWatchingRail />;
    case "next_up":
      return <NextUpRail />;
    case "trending":
      return <TrendingRail />;
    case "live_now":
      return <LiveNowRail />;
    case "latest_in_library":
      if (!s.library_id) return null;
      return (
        <LatestInLibraryRail
          libraryId={s.library_id}
          libraryName={s.library_name ?? ""}
        />
      );
    default:
      return null;
  }
}

// ─── Home Page ────────────────────────────────────────────────────────────

export default function Home() {
  const { t } = useTranslation();
  // Hero candidates — kept on this page (not a rail) because the
  // hero is the page's "front door": we always want to surface
  // SOMETHING above the fold even on a fresh install with no
  // continue-watching state. Using continue + latest gives both a
  // returning user (their last show) and a cold-start user (a
  // freshly added title) a sensible default.
  const continueWatching = useContinueWatching();
  const latestItems = useLatestItems();

  const layout = useHomeLayout();

  const heroLoading =
    continueWatching.isLoading && latestItems.isLoading;
  const heroEmpty =
    (continueWatching.data?.length ?? 0) === 0 &&
    (latestItems.data?.length ?? 0) === 0;
  const fatalError =
    layout.isError &&
    continueWatching.isError &&
    latestItems.isError &&
    heroEmpty;

  if (fatalError) {
    return (
      <div className="flex flex-col items-center justify-center gap-4 py-32 text-center">
        <svg
          className="h-12 w-12 text-white/20"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth={1.5}
        >
          <path d="M12 9v3.75m9-.75a9 9 0 11-18 0 9 9 0 0118 0zm-9 3.75h.008v.008H12v-.008z" />
        </svg>
        <p className="text-white/50">{t("home.failedToLoad")}</p>
        <button
          type="button"
          onClick={() => {
            continueWatching.refetch();
            latestItems.refetch();
            layout.refetch();
          }}
          className="rounded-lg bg-white/10 px-5 py-2 text-sm font-medium text-white hover:bg-white/20 transition-colors"
        >
          {t("common.retry")}
        </button>
      </div>
    );
  }

  const sections = layout.data?.sections ?? [];

  return (
    <div className="flex flex-col gap-10 bg-bg-base min-h-screen -mx-4 -mb-4 md:-mx-6 md:-mb-6">
      <div className="mx-4 md:mx-6">
        {heroLoading ? (
          <div
            className="relative -mx-4 md:-mx-6 h-[70vh] min-h-[450px] max-h-[750px] bg-bg-base animate-pulse"
            style={{ marginTop: "calc(var(--topbar-height) * -1)" }}
          />
        ) : (
          <HeroBanner
            continueWatching={continueWatching.data ?? []}
            latest={latestItems.data ?? []}
          />
        )}
      </div>

      <div className="flex flex-col gap-10 px-8 pb-12 md:px-12">
        {sections.map((s) => {
          const node = renderSection(s);
          if (!node) return null;
          return <div key={s.id}>{node}</div>;
        })}
        {/* Federated rails. Live outside the layout-driven dispatch
            for v1 because `peer_recent` / `peer_continue_watching`
            aren't registered HomeSection types yet — both self-hide
            when there's nothing to show, so a solo deployment
            renders home identically to pre-federation. Promoting
            them to configurable sections is a follow-up that needs
            the backend `validSectionType` whitelist + the
            home-layout settings UI to grow toggles for them. */}
        <PeerContinueWatchingRail />
        <PeerRecentRail />
      </div>
    </div>
  );
}
