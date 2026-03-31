import { useState, useCallback, useEffect } from 'react';
import { Outlet, useLocation } from 'react-router';
import { Sidebar } from './Sidebar';
import { TopBar } from './TopBar';

// ─── Props ──────────────────────────────────────────────────────────────────

interface AppLayoutProps {
  title?: string;
}

// ─── Responsive Hook ────────────────────────────────────────────────────────

function useIsMobile(breakpoint = 768) {
  const [isMobile, setIsMobile] = useState(
    typeof window !== 'undefined' ? window.innerWidth < breakpoint : false,
  );

  useEffect(() => {
    const mql = window.matchMedia(`(max-width: ${breakpoint - 1}px)`);
    const handler = (e: MediaQueryListEvent) => setIsMobile(e.matches);
    mql.addEventListener('change', handler);
    setIsMobile(mql.matches);
    return () => mql.removeEventListener('change', handler);
  }, [breakpoint]);

  return isMobile;
}

// ─── AppLayout ──────────────────────────────────────────────────────────────

export function AppLayout({ title }: AppLayoutProps) {
  const [collapsed, setCollapsed] = useState(false);
  const [mobileOpen, setMobileOpen] = useState(false);
  const isMobile = useIsMobile();
  const location = useLocation();

  const toggleCollapse = useCallback(() => {
    setCollapsed((prev) => !prev);
  }, []);

  const toggleMobile = useCallback(() => {
    setMobileOpen((prev) => !prev);
  }, []);

  const closeMobile = useCallback(() => {
    setMobileOpen(false);
  }, []);

  // Close mobile drawer when switching to desktop
  useEffect(() => {
    if (!isMobile) {
      setMobileOpen(false);
    }
  }, [isMobile]);

  // Close mobile drawer on navigation
  useEffect(() => {
    setMobileOpen(false);
  }, [location.pathname]);

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
