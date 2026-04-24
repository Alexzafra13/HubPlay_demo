import { useState, useSyncExternalStore } from 'react';
import { Outlet, useLocation } from 'react-router';
import { Sidebar } from './Sidebar';
import { TopBar } from './TopBar';

// ─── Props ──────────────────────────────────────────────────────────────────

interface AppLayoutProps {
  title?: string;
}

// ─── Responsive Hook ────────────────────────────────────────────────────────

/**
 * useIsMobile subscribes to a `matchMedia` query via useSyncExternalStore,
 * which is the React-18+ canonical way to mirror browser state into React
 * without an effect. This avoids the cascading render `setState-in-effect`
 * produced (initial state from innerWidth, effect then reconciles with
 * matchMedia).
 */
function useIsMobile(breakpoint = 768) {
  const query = `(max-width: ${breakpoint - 1}px)`;
  return useSyncExternalStore(
    (onChange) => {
      const mql = window.matchMedia(query);
      mql.addEventListener('change', onChange);
      return () => mql.removeEventListener('change', onChange);
    },
    () => window.matchMedia(query).matches,
    () => false, // SSR fallback — we're a mobile-last client app.
  );
}

// ─── AppLayout ──────────────────────────────────────────────────────────────

export function AppLayout({ title }: AppLayoutProps) {
  const [collapsed, setCollapsed] = useState(false);
  const [mobileOpen, setMobileOpen] = useState(false);
  const isMobile = useIsMobile();
  const location = useLocation();

  // Force-close the mobile drawer whenever we leave the mobile viewport or
  // navigate to a new route. Done via the React-docs "reset on prop change"
  // pattern (track the last seen values, compare during render) instead of
  // useEffect + setState, which fires a second render after the first paint.
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

  const toggleCollapse = () => setCollapsed((prev) => !prev);
  const toggleMobile = () => setMobileOpen((prev) => !prev);
  const closeMobile = () => setMobileOpen(false);

  return (
    <div className="min-h-screen bg-bg-base font-sans">
      {/* Mobile drawer overlay */}
      <div
        className={[
          'fixed inset-0 z-30 bg-black/60 backdrop-blur-sm transition-opacity duration-300 md:hidden',
          mobileOpen ? 'opacity-100' : 'opacity-0 pointer-events-none',
        ].join(' ')}
        onClick={closeMobile}
      />

      {/* Mobile drawer */}
      <div
        className={[
          'fixed top-0 left-0 h-full z-40 transition-transform duration-300 ease-out md:hidden',
          mobileOpen ? 'translate-x-0' : '-translate-x-full pointer-events-none',
        ].join(' ')}
      >
        <Sidebar
          collapsed={false}
          onToggleCollapse={toggleCollapse}
          onClose={closeMobile}
        />
      </div>

      {/* Desktop sidebar */}
      <div className="hidden md:block fixed top-0 left-0 h-full z-40">
        <Sidebar
          collapsed={collapsed}
          onToggleCollapse={toggleCollapse}
        />
      </div>

      {/* Main content area */}
      <div
        className="transition-[margin-left] duration-200"
        style={{
          marginLeft: isMobile
            ? 0
            : collapsed
              ? 'var(--sidebar-collapsed-width)'
              : 'var(--sidebar-width)',
        }}
      >
        <TopBar title={title} onMenuClick={toggleMobile} />

        <main className="px-4 pb-4 md:px-6 md:pb-6">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
