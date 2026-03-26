import { NavLink, useNavigate } from 'react-router';
import type { ReactNode } from 'react';
import { useAuthStore } from '@/store/auth';

// ─── Inline SVG Icons (20x20 viewBox, stroke-based) ────────────────────────

function IconHome() {
  return (
    <svg width="20" height="20" viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
      <path d="M3 10L10 3l7 7" />
      <path d="M5 8.5V16a1 1 0 001 1h3v-4h2v4h3a1 1 0 001-1V8.5" />
    </svg>
  );
}

function IconFilm() {
  return (
    <svg width="20" height="20" viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
      <rect x="2" y="3" width="16" height="14" rx="1" />
      <path d="M2 7h16M2 13h16M6 3v4M6 13v4M14 3v4M14 13v4" />
    </svg>
  );
}

function IconTv() {
  return (
    <svg width="20" height="20" viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
      <rect x="2" y="4" width="16" height="11" rx="1" />
      <path d="M7 18h6M10 15v3" />
    </svg>
  );
}

function IconAntenna() {
  return (
    <svg width="20" height="20" viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
      <path d="M10 10v7" />
      <path d="M6 17h8" />
      <circle cx="10" cy="7" r="2" />
      <path d="M5.5 3.5a7 7 0 000 7" />
      <path d="M14.5 3.5a7 7 0 010 7" />
    </svg>
  );
}

function IconSearch() {
  return (
    <svg width="20" height="20" viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
      <circle cx="8.5" cy="8.5" r="5" />
      <path d="M12.5 12.5L17 17" />
    </svg>
  );
}

function IconFolder() {
  return (
    <svg width="20" height="20" viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
      <path d="M2 5a1 1 0 011-1h4l2 2h8a1 1 0 011 1v8a1 1 0 01-1 1H3a1 1 0 01-1-1V5z" />
    </svg>
  );
}

function IconUsers() {
  return (
    <svg width="20" height="20" viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
      <circle cx="7" cy="7" r="3" />
      <path d="M2 17a5 5 0 0110 0" />
      <circle cx="14" cy="7" r="2.5" />
      <path d="M13 17a5 5 0 016 0" />
    </svg>
  );
}

function IconGear() {
  return (
    <svg width="20" height="20" viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
      <circle cx="10" cy="10" r="3" />
      <path d="M10 1.5v2M10 16.5v2M3.5 3.5l1.4 1.4M15.1 15.1l1.4 1.4M1.5 10h2M16.5 10h2M3.5 16.5l1.4-1.4M15.1 4.9l1.4-1.4" />
    </svg>
  );
}

function IconLogout() {
  return (
    <svg width="20" height="20" viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
      <path d="M7 17H4a1 1 0 01-1-1V4a1 1 0 011-1h3" />
      <path d="M11 14l4-4-4-4M15 10H7" />
    </svg>
  );
}

function IconChevronLeft() {
  return (
    <svg width="20" height="20" viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
      <path d="M12 4l-6 6 6 6" />
    </svg>
  );
}

function IconChevronRight() {
  return (
    <svg width="20" height="20" viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
      <path d="M8 4l6 6-6 6" />
    </svg>
  );
}

// ─── Nav Item ───────────────────────────────────────────────────────────────

interface NavItemProps {
  to: string;
  icon: ReactNode;
  label: string;
  collapsed: boolean;
}

function NavItem({ to, icon, label, collapsed }: NavItemProps) {
  return (
    <NavLink
      to={to}
      end
      className={({ isActive }) =>
        [
          'flex items-center gap-3 px-3 py-2 rounded-lg text-sm font-medium transition-colors relative',
          collapsed ? 'justify-center' : '',
          isActive
            ? 'bg-accent-soft text-accent-light before:absolute before:left-0 before:top-1 before:bottom-1 before:w-[3px] before:rounded-r before:bg-accent'
            : 'text-text-secondary hover:bg-bg-elevated hover:text-text-primary',
        ].join(' ')
      }
      title={collapsed ? label : undefined}
    >
      <span className="flex-shrink-0">{icon}</span>
      {!collapsed && <span>{label}</span>}
    </NavLink>
  );
}

// ─── Sidebar ────────────────────────────────────────────────────────────────

interface SidebarProps {
  collapsed: boolean;
  onToggleCollapse: () => void;
  onClose?: () => void;
}

export function Sidebar({ collapsed, onToggleCollapse, onClose }: SidebarProps) {
  const { user, logout } = useAuthStore();
  const navigate = useNavigate();
  const isAdmin = user?.role === 'admin';

  const handleLogout = () => {
    logout();
    navigate('/login');
  };

  const initials = user?.display_name
    ? user.display_name
        .split(' ')
        .map((n) => n[0])
        .join('')
        .toUpperCase()
        .slice(0, 2)
    : user?.username?.slice(0, 2).toUpperCase() ?? '?';

  return (
    <aside
      className="fixed top-0 left-0 h-full bg-bg-base/80 backdrop-blur-xl border-r border-white/5 flex flex-col z-40 transition-[width] duration-200"
      style={{ width: collapsed ? 'var(--sidebar-collapsed-width)' : 'var(--sidebar-width)' }}
    >
      {/* Brand */}
      <div className="flex items-center gap-2 px-4 h-14 border-b border-white/5 flex-shrink-0">
        <svg
          width="28"
          height="28"
          viewBox="0 0 28 28"
          fill="none"
          className="text-accent flex-shrink-0"
        >
          <rect width="28" height="28" rx="6" fill="currentColor" fillOpacity="0.15" />
          <path d="M10 7v14l12-7L10 7z" fill="currentColor" />
        </svg>
        {!collapsed && (
          <span className="text-lg font-bold text-text-primary tracking-tight">HubPlay</span>
        )}
        {onClose && (
          <button
            onClick={onClose}
            className="ml-auto p-1.5 rounded-md text-text-secondary hover:text-text-primary hover:bg-bg-elevated transition-colors md:hidden"
            aria-label="Close menu"
          >
            <svg width="20" height="20" viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round">
              <path d="M5 5l10 10M15 5L5 15" />
            </svg>
          </button>
        )}
      </div>

      {/* Navigation */}
      <nav className="flex-1 overflow-y-auto px-2 py-3 space-y-1">
        <NavItem to="/" icon={<IconHome />} label="Home" collapsed={collapsed} />
        <NavItem to="/movies" icon={<IconFilm />} label="Movies" collapsed={collapsed} />
        <NavItem to="/series" icon={<IconTv />} label="Series" collapsed={collapsed} />
        <NavItem to="/live-tv" icon={<IconAntenna />} label="Live TV" collapsed={collapsed} />
        <NavItem to="/search" icon={<IconSearch />} label="Search" collapsed={collapsed} />

        {isAdmin && (
          <>
            <div className="my-3 mx-3 border-t border-white/5" />
            {!collapsed && (
              <span className="px-3 text-xs font-semibold text-text-secondary uppercase tracking-wider">
                Admin
              </span>
            )}
            <div className="mt-1 space-y-1">
              <NavItem to="/admin/libraries" icon={<IconFolder />} label="Libraries" collapsed={collapsed} />
              <NavItem to="/admin/users" icon={<IconUsers />} label="Users" collapsed={collapsed} />
              <NavItem to="/admin/system" icon={<IconGear />} label="System" collapsed={collapsed} />
            </div>
          </>
        )}
      </nav>

      {/* Bottom: User + Collapse Toggle */}
      <div className="border-t border-white/5 px-2 py-3 flex-shrink-0 space-y-2">
        {/* User info */}
        <div className="flex items-center gap-2 px-2">
          <div className="w-8 h-8 rounded-full bg-accent/20 text-accent flex items-center justify-center text-xs font-bold flex-shrink-0">
            {initials}
          </div>
          {!collapsed && (
            <div className="flex-1 min-w-0">
              <p className="text-sm font-medium text-text-primary truncate">
                {user?.display_name || user?.username}
              </p>
            </div>
          )}
          <button
            onClick={handleLogout}
            className="p-1.5 rounded-md text-text-secondary hover:text-text-primary hover:bg-bg-elevated transition-colors flex-shrink-0"
            title="Log out"
          >
            <IconLogout />
          </button>
        </div>

        {/* Collapse toggle */}
        <button
          onClick={onToggleCollapse}
          className="w-full flex items-center justify-center p-2 rounded-lg text-text-secondary hover:text-text-primary hover:bg-bg-elevated transition-colors"
          title={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
        >
          {collapsed ? <IconChevronRight /> : <IconChevronLeft />}
        </button>
      </div>
    </aside>
  );
}
