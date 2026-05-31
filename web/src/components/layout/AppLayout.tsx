import { useState } from "react";
import { Outlet, useLocation } from "react-router";
import { useTranslation } from "react-i18next";
import { TopBar } from "./TopBar";
import { MobileDrawer } from "./MobileDrawer";
import { MiniPlayer } from "@/components/livetv/MiniPlayer";
import { usePlaylistRefreshEvents } from "@/hooks/usePlaylistRefreshEvents";
import { useUserDataSync } from "@/hooks/useUserDataSync";
import { useIsMobile } from "@/hooks/useIsMobile";

// ─── AppLayout ──────────────────────────────────────────────────────────────

export function AppLayout() {
  const { t } = useTranslation();
  // App-wide listener for IPTV M3U refresh completion. Mounted here so
  // every authenticated route picks up backend invalidations regardless
  // of which page kicked off the import (or whether the kick came from
  // the scheduler with no UI at all). See hook docstring for rationale.
  usePlaylistRefreshEvents();

  // Cross-device watch state sync — when the same user updates progress
  // / favourites / played on another device, the SSE stream tells us
  // and we invalidate the right TanStack queries so the UI catches up.
  // Mounts at the shell so it works regardless of the active route.
  useUserDataSync();

  const [mobileOpen, setMobileOpen] = useState(false);
  const isMobile = useIsMobile();
  const location = useLocation();

  // Close drawer on navigation or when leaving the mobile viewport.
  // React-docs "reset on prop change" pattern (track last seen, compare
  // during render) avoids the extra render that useEffect+setState pays.
  const [lastResetKey, setLastResetKey] = useState({
    pathname: location.pathname,
    isMobile,
  });
  if (
    lastResetKey.pathname !== location.pathname ||
    lastResetKey.isMobile !== isMobile
  ) {
    setLastResetKey({ pathname: location.pathname, isMobile });
    if (mobileOpen) setMobileOpen(false);
  }

  const toggleMobile = () => setMobileOpen((prev) => !prev);
  const closeMobile = () => setMobileOpen(false);

  return (
    /*
     * No `bg-bg-base` on this wrapper — the body has it globally
     * (see styles/globals.css). Painting it again here would cover
     * any fixed-positioned z<0 backdrop a route renders (e.g.
     * ItemDetail's ambient-aurora canvas), since the static wrapper
     * paints in z=auto over the negative-z siblings. Keeping the
     * layout transparent lets per-route backgrounds show through.
     */
    <div className="min-h-screen font-sans">
      {/* TopBar — full-width fixed at the top (z-40). Owns brand,
          center MainNav (desktop), search, avatar dropdown. */}
      <TopBar onMobileMenuClick={toggleMobile} />

      {/* Backdrop del drawer móvil. Usamos <button> en lugar de <div>
          para que el cierre sea accesible por teclado (Enter / Space)
          y los lectores de pantalla lo anuncien como acción. */}
      <button
        type="button"
        aria-label={t("common.close", { defaultValue: "Cerrar" })}
        className={[
          "fixed inset-0 z-30 bg-black/60 backdrop-blur-sm transition-opacity duration-300 md:hidden cursor-default",
          mobileOpen ? "opacity-100" : "opacity-0 pointer-events-none",
        ].join(" ")}
        style={{ top: "var(--topbar-height)" }}
        onClick={closeMobile}
      />
      <div
        className={[
          "fixed left-0 z-40 transition-transform duration-300 ease-out md:hidden",
          mobileOpen ? "translate-x-0" : "-translate-x-full pointer-events-none",
        ].join(" ")}
        style={{
          top: "var(--topbar-height)",
          height: "calc(100dvh - var(--topbar-height))",
        }}
      >
        <MobileDrawer onClose={closeMobile} />
      </div>

      {/* Main content — full width below the topbar. No sidebar means
          no horizontal margin gymnastics; pages get the entire viewport. */}
      <main
        className="px-4 pb-4 md:px-6 md:pb-6"
        style={{ paddingTop: "var(--topbar-height)" }}
      >
        <Outlet />
      </main>

      {/* Mini-player — fixed bottom-right, lives at the shell level so
          it survives navigation between routes. Renders nothing when
          either nothing is playing or the full overlay is up. */}
      <MiniPlayer />
    </div>
  );
}
