import { useState, useCallback, useEffect } from 'react';
import { Outlet } from 'react-router';
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

  const toggleCollapse = useCallback(() => {
    setCollapsed((prev) => !prev);
  }, []);

  const toggleMobile = useCallback(() => {
    setMobileOpen((prev) => !prev);
  }, []);

  const closeMobile = useCallback(() => {
    setMobileOpen(false);
  }, []);

  // Close mobile sidebar when switching to desktop
  useEffect(() => {
    if (!isMobile) {
      setMobileOpen(false);
    }
  }, [isMobile]);

  return (
    <div className="min-h-screen bg-bg-base font-sans">
      {/* Mobile overlay */}
      {mobileOpen && (
        <div
          className="fixed inset-0 z-30 bg-black/50 md:hidden"
          onClick={closeMobile}
        />
      )}

      {/* Sidebar: hidden on mobile unless mobileOpen */}
      <div
        className={[
          'md:block',
          mobileOpen ? 'block' : 'hidden',
        ].join(' ')}
      >
        <Sidebar
          collapsed={collapsed}
          onToggleCollapse={toggleCollapse}
          onClose={closeMobile}
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

        <main className="p-4 md:p-6">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
