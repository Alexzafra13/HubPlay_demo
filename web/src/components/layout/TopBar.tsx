import { useState, useRef, useEffect } from 'react';
import { useNavigate, NavLink } from 'react-router';
import { useTranslation } from 'react-i18next';
import { useAuthStore } from '@/store/auth';
import { useTopBarSlotContent } from './TopBarSlot';

// ─── Props ──────────────────────────────────────────────────────────────────

interface TopBarProps {
  title?: string;
  onMenuClick: () => void;
}

// ─── TopBar ─────────────────────────────────────────────────────────────────

export function TopBar({ title, onMenuClick }: TopBarProps) {
  const { t } = useTranslation();
  const { user, logout } = useAuthStore();
  const navigate = useNavigate();
  const [dropdownOpen, setDropdownOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState('');
  const dropdownRef = useRef<HTMLDivElement>(null);
  const slotContent = useTopBarSlotContent();
  const scrolled = useScrolledPast(8);

  const initials = user?.display_name
    ? user.display_name
        .split(' ')
        .map((n) => n[0])
        .join('')
        .toUpperCase()
        .slice(0, 2)
    : user?.username?.slice(0, 2).toUpperCase() ?? '?';

  const handleLogout = () => {
    logout();
    setDropdownOpen(false);
    navigate('/login');
  };

  const handleSearch = (e: React.FormEvent) => {
    e.preventDefault();
    if (searchQuery.trim()) {
      navigate(`/search?q=${encodeURIComponent(searchQuery.trim())}`);
      setSearchQuery('');
    }
  };

  // Close dropdown on outside click
  useEffect(() => {
    function handleClickOutside(e: MouseEvent) {
      if (dropdownRef.current && !dropdownRef.current.contains(e.target as Node)) {
        setDropdownOpen(false);
      }
    }
    if (dropdownOpen) {
      document.addEventListener('mousedown', handleClickOutside);
      return () => document.removeEventListener('mousedown', handleClickOutside);
    }
  }, [dropdownOpen]);

  return (
    <header
      className={[
        'sticky top-0 z-30 flex items-center px-4 gap-3',
        'transition-[background-color,backdrop-filter,border-color] duration-200',
        scrolled
          ? 'bg-bg-base/70 backdrop-blur-xl border-b border-white/5'
          : 'bg-bg-base/70 backdrop-blur-xl border-b border-white/5 md:bg-transparent md:backdrop-blur-none md:border-b-0',
      ].join(' ')}
      style={{ height: 'var(--topbar-height)' }}
    >
      {/* Hamburger (mobile) */}
      <button
        onClick={onMenuClick}
        className="md:hidden p-2 -ml-1 rounded-lg text-text-secondary hover:text-text-primary hover:bg-white/5 transition-colors"
        aria-label={t('nav.toggleMenu')}
      >
        <svg width="20" height="20" viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round">
          <path d="M3 5h14M3 10h14M3 15h14" />
        </svg>
      </button>

      {/* Brand (mobile only) */}
      <NavLink to="/" className="flex items-center gap-2 md:hidden">
        <svg
          width="24"
          height="24"
          viewBox="0 0 28 28"
          fill="none"
          className="text-accent"
        >
          <rect width="28" height="28" rx="6" fill="currentColor" fillOpacity="0.15" />
          <path d="M10 7v14l12-7L10 7z" fill="currentColor" />
        </svg>
        <span className="text-base font-bold text-text-primary tracking-tight">HubPlay</span>
      </NavLink>

      {/* Page Title (desktop) */}
      {title && (
        <h1 className="hidden md:block text-base font-semibold text-text-primary truncate">{title}</h1>
      )}

      <div className="flex-1" />

      {/* Page-injected controls (LiveTV tabs+search, future page filters)
          take precedence over the global search. Falls back to the
          global search when no page registers content via useTopBarSlot. */}
      {slotContent ?? (
        <form onSubmit={handleSearch} className="hidden sm:flex items-center">
          <div className="relative">
            <svg
              width="16"
              height="16"
              viewBox="0 0 20 20"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.5"
              strokeLinecap="round"
              strokeLinejoin="round"
              className="absolute left-2.5 top-1/2 -translate-y-1/2 text-text-secondary pointer-events-none"
            >
              <circle cx="8.5" cy="8.5" r="5" />
              <path d="M12.5 12.5L17 17" />
            </svg>
            <input
              type="text"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder={t('topbar.searchPlaceholder')}
              className="w-48 pl-8 pr-3 py-1.5 rounded-lg bg-bg-base border border-border text-sm text-text-primary placeholder:text-text-secondary focus:outline-none focus:ring-1 focus:ring-accent focus:border-accent transition-colors"
            />
          </div>
        </form>
      )}

      {/* User Avatar Dropdown */}
      <div className="relative" ref={dropdownRef}>
        <button
          onClick={() => setDropdownOpen(!dropdownOpen)}
          className="w-8 h-8 rounded-full bg-accent/20 text-accent flex items-center justify-center text-xs font-bold hover:bg-accent/30 transition-colors"
          aria-label="User menu"
        >
          {initials}
        </button>

        {dropdownOpen && (
          <div className="absolute right-0 top-full mt-1 w-48 bg-bg-card border border-border rounded-lg shadow-lg py-1 z-50">
            <div className="px-3 py-2 border-b border-border">
              <p className="text-sm font-medium text-text-primary truncate">
                {user?.display_name || user?.username}
              </p>
              <p className="text-xs text-text-secondary truncate">{user?.role}</p>
            </div>
            <NavLink
              to="/settings"
              onClick={() => setDropdownOpen(false)}
              className="w-full flex items-center gap-2 px-3 py-2 text-sm text-text-secondary hover:text-text-primary hover:bg-bg-elevated transition-colors"
            >
              <svg width="16" height="16" viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
                <circle cx="10" cy="10" r="3" />
                <path d="M10 1.5v2M10 16.5v2M3.5 3.5l1.4 1.4M15.1 15.1l1.4 1.4M1.5 10h2M16.5 10h2M3.5 16.5l1.4-1.4M15.1 4.9l1.4-1.4" />
              </svg>
              {t('common.settings')}
            </NavLink>
            {user?.role === 'admin' && (
              <NavLink
                to="/admin/libraries"
                onClick={() => setDropdownOpen(false)}
                className="w-full flex items-center gap-2 px-3 py-2 text-sm text-text-secondary hover:text-text-primary hover:bg-bg-elevated transition-colors"
              >
                <svg width="16" height="16" viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
                  <rect x="3" y="3" width="6" height="6" rx="1" />
                  <rect x="11" y="3" width="6" height="6" rx="1" />
                  <rect x="3" y="11" width="6" height="6" rx="1" />
                  <rect x="11" y="11" width="6" height="6" rx="1" />
                </svg>
                {t('common.administration')}
              </NavLink>
            )}
            <div className="border-t border-border" />
            <button
              onClick={handleLogout}
              className="w-full flex items-center gap-2 px-3 py-2 text-sm text-text-secondary hover:text-text-primary hover:bg-bg-elevated transition-colors"
            >
              <svg width="16" height="16" viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
                <path d="M7 17H4a1 1 0 01-1-1V4a1 1 0 011-1h3" />
                <path d="M11 14l4-4-4-4M15 10H7" />
              </svg>
              {t('common.logOut')}
            </button>
          </div>
        )}
      </div>
    </header>
  );
}

/**
 * useScrolledPast — returns true once `window.scrollY` exceeds `threshold`.
 *
 * The app uses window-level scrolling (no inner scroll container in
 * AppLayout), so we listen on `window`. Throttled with rAF — every native
 * `scroll` event schedules at most one read+setState per animation frame,
 * which is what avoids the layout-thrash spiral that happens if you call
 * setState directly in the listener.
 *
 * SSR-safe lazy initializer (typeof window) so the bundle can import this
 * file in non-browser contexts (tests, future SSR) without crashing.
 */
function useScrolledPast(threshold: number): boolean {
  const [scrolled, setScrolled] = useState(() => {
    if (typeof window === 'undefined') return false;
    return window.scrollY > threshold;
  });
  useEffect(() => {
    let ticking = false;
    const onScroll = () => {
      if (ticking) return;
      ticking = true;
      window.requestAnimationFrame(() => {
        setScrolled(window.scrollY > threshold);
        ticking = false;
      });
    };
    window.addEventListener('scroll', onScroll, { passive: true });
    onScroll();
    return () => window.removeEventListener('scroll', onScroll);
  }, [threshold]);
  return scrolled;
}
