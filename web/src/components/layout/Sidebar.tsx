import { useRef, useState } from "react";
import { NavLink, useLocation, useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import { motion, AnimatePresence } from "framer-motion";
import {
  Home,
  Film,
  Tv,
  Radio,
  Users,
  Search,
  Settings,
  Smartphone,
  FolderTree,
  ServerCog,
  LogOut,
  ChevronsLeft,
  ChevronsRight,
  X,
  type LucideIcon,
} from "lucide-react";
import { useAuthStore } from "@/store/auth";
import { getInitials } from "@/utils/userDisplay";

// ─── Item model ─────────────────────────────────────────────────────────────

type Badge =
  | { kind: "live" }
  | { kind: "health"; tone: "ok" | "warn" | "error" }
  | { kind: "count"; value: number };

interface NavItemDef {
  to: string;
  icon: LucideIcon;
  labelKey: string;
  badge?: Badge;
}

const MAIN: NavItemDef[] = [
  { to: "/", icon: Home, labelKey: "nav.home" },
  { to: "/movies", icon: Film, labelKey: "nav.movies" },
  { to: "/series", icon: Tv, labelKey: "nav.series" },
  { to: "/live-tv", icon: Radio, labelKey: "nav.liveTV", badge: { kind: "live" } },
  { to: "/peers", icon: Users, labelKey: "nav.peers" },
];

const PERSONAL: NavItemDef[] = [
  { to: "/settings", icon: Settings, labelKey: "nav.settings" },
  { to: "/link", icon: Smartphone, labelKey: "nav.linkDevice" },
];

const ADMIN: NavItemDef[] = [
  { to: "/admin/libraries", icon: FolderTree, labelKey: "nav.libraries" },
  { to: "/admin/users", icon: Users, labelKey: "nav.users" },
  { to: "/admin/system", icon: ServerCog, labelKey: "nav.system" },
];

// ─── Sidebar ────────────────────────────────────────────────────────────────

interface SidebarProps {
  collapsed: boolean;
  onToggleCollapse: () => void;
  onClose?: () => void;
}

export function Sidebar({ collapsed, onToggleCollapse, onClose }: SidebarProps) {
  const { t } = useTranslation();
  const { user, logout } = useAuthStore();
  const navigate = useNavigate();
  const location = useLocation();
  const isAdmin = user?.role === "admin";
  const initials = getInitials(user);

  const handleLogout = () => {
    logout();
    navigate("/login");
  };

  // Single flat list so the active-indicator's layoutId can animate
  // between any two items (main → personal → admin) seamlessly.
  const allItems = [...MAIN, ...PERSONAL, ...(isAdmin ? ADMIN : [])];
  const activePath =
    allItems
      .map((i) => i.to)
      .filter((p) => (p === "/" ? location.pathname === "/" : location.pathname.startsWith(p)))
      .sort((a, b) => b.length - a.length)[0] ?? null;

  return (
    <aside
      className="h-full flex flex-col select-none"
      style={{
        width: collapsed ? "var(--sidebar-collapsed-width)" : "var(--sidebar-width)",
        background:
          "linear-gradient(180deg, rgba(11,15,23,0.85) 0%, rgba(7,9,14,0.88) 100%)",
        backdropFilter: "blur(20px) saturate(140%)",
        borderRight: "1px solid var(--color-border-subtle)",
        transition: "width var(--duration-base) var(--ease-emphasized)",
      }}
    >
      {/* Brand */}
      <div className="flex items-center h-16 px-3 flex-shrink-0">
        <NavLink
          to="/"
          end
          className="flex items-center gap-2.5 px-2 py-1.5 rounded-lg hover:bg-bg-hover transition-colors flex-1 min-w-0"
        >
          <BrandMark />
          <AnimatePresence initial={false}>
            {!collapsed && (
              <motion.span
                key="brand-text"
                initial={{ opacity: 0, x: -4 }}
                animate={{ opacity: 1, x: 0 }}
                exit={{ opacity: 0, x: -4 }}
                transition={{ duration: 0.16 }}
                className="text-[15px] font-semibold tracking-tight text-text-primary truncate"
              >
                HubPlay
              </motion.span>
            )}
          </AnimatePresence>
        </NavLink>
        {onClose && (
          <button
            onClick={onClose}
            className="ml-1 p-2 rounded-lg text-text-secondary hover:text-text-primary hover:bg-bg-hover transition-colors md:hidden"
            aria-label={t("nav.closeMenu")}
          >
            <X className="h-[18px] w-[18px]" strokeWidth={1.6} />
          </button>
        )}
      </div>

      {/* Search trigger */}
      <div className="px-3 mb-2">
        <SearchTrigger collapsed={collapsed} onClick={() => navigate("/search")} />
      </div>

      {/* Navigation */}
      <nav className="flex-1 overflow-y-auto px-3 pb-3 scrollbar-hide">
        <NavGroup items={MAIN} collapsed={collapsed} activePath={activePath} />

        <Divider collapsed={collapsed} />

        <NavGroup items={PERSONAL} collapsed={collapsed} activePath={activePath} />

        {isAdmin && (
          <>
            <SectionHeader collapsed={collapsed} label={t("nav.admin")} />
            <NavGroup items={ADMIN} collapsed={collapsed} activePath={activePath} />
          </>
        )}
      </nav>

      {/* Bottom: user pod + collapse toggle */}
      <div className="px-3 pb-3 flex-shrink-0 space-y-2">
        <UserPod
          collapsed={collapsed}
          name={user?.display_name || user?.username || ""}
          role={isAdmin ? t("nav.admin") : ""}
          initials={initials}
          onLogout={handleLogout}
        />

        <button
          onClick={onToggleCollapse}
          className="hidden md:flex w-full items-center justify-center h-9 rounded-lg text-text-muted hover:text-text-primary hover:bg-bg-hover transition-colors"
          title={collapsed ? t("nav.expandSidebar") : t("nav.collapseSidebar")}
          aria-label={collapsed ? t("nav.expandSidebar") : t("nav.collapseSidebar")}
        >
          {collapsed ? (
            <ChevronsRight className="h-[18px] w-[18px]" strokeWidth={1.6} />
          ) : (
            <ChevronsLeft className="h-[18px] w-[18px]" strokeWidth={1.6} />
          )}
        </button>
      </div>
    </aside>
  );
}

// ─── Sub-components ─────────────────────────────────────────────────────────

function BrandMark() {
  return (
    <span className="relative flex h-8 w-8 items-center justify-center rounded-lg bg-accent/10 ring-1 ring-accent/20 flex-shrink-0">
      <span
        className="absolute inset-0 rounded-lg opacity-60 blur-md"
        style={{
          background:
            "radial-gradient(circle at 30% 30%, var(--color-accent-glow), transparent 65%)",
        }}
        aria-hidden
      />
      <svg viewBox="0 0 24 24" className="relative h-[16px] w-[16px] text-accent fill-current" aria-hidden>
        <path d="M8 5.5v13l11-6.5L8 5.5z" />
      </svg>
    </span>
  );
}

function SearchTrigger({ collapsed, onClick }: { collapsed: boolean; onClick: () => void }) {
  const { t } = useTranslation();
  if (collapsed) {
    return (
      <button
        onClick={onClick}
        className="w-full h-10 flex items-center justify-center rounded-lg text-text-secondary hover:text-text-primary hover:bg-bg-hover transition-colors"
        title={t("nav.search")}
      >
        <Search className="h-[18px] w-[18px]" strokeWidth={1.6} />
      </button>
    );
  }
  return (
    <button
      onClick={onClick}
      className="w-full h-10 flex items-center gap-2.5 px-3 rounded-lg bg-bg-hover/60 hover:bg-bg-active border border-border-subtle hover:border-border text-left text-sm text-text-secondary hover:text-text-primary transition-colors"
    >
      <Search className="h-[16px] w-[16px] flex-shrink-0" strokeWidth={1.6} />
      <span className="flex-1 truncate text-[13px]">{t("nav.search")}</span>
      <kbd className="hidden md:flex items-center gap-0.5 px-1.5 py-0.5 rounded text-[10px] font-medium bg-bg-base/60 border border-border-subtle text-text-muted">
        ⌘K
      </kbd>
    </button>
  );
}

function Divider({ collapsed }: { collapsed: boolean }) {
  return (
    <div
      className="my-2"
      style={{
        height: 1,
        background:
          "linear-gradient(to right, transparent, var(--color-border-subtle), transparent)",
        marginLeft: collapsed ? 8 : 12,
        marginRight: collapsed ? 8 : 12,
      }}
    />
  );
}

function SectionHeader({ collapsed, label }: { collapsed: boolean; label: string }) {
  if (collapsed) return <Divider collapsed />;
  return (
    <div className="mt-4 mb-1.5 px-3">
      <span className="text-[10px] font-semibold uppercase tracking-[0.12em] text-text-muted">
        {label}
      </span>
    </div>
  );
}

function NavGroup({
  items,
  collapsed,
  activePath,
}: {
  items: NavItemDef[];
  collapsed: boolean;
  activePath: string | null;
}) {
  return (
    <ul className="space-y-0.5">
      {items.map((item) => (
        <NavRow key={item.to} item={item} collapsed={collapsed} isActive={activePath === item.to} />
      ))}
    </ul>
  );
}

function NavRow({
  item,
  collapsed,
  isActive,
}: {
  item: NavItemDef;
  collapsed: boolean;
  isActive: boolean;
}) {
  const { t } = useTranslation();
  const Icon = item.icon;
  const label = t(item.labelKey);

  return (
    <li className="relative">
      <NavLink
        to={item.to}
        end={item.to === "/"}
        className="relative flex items-center gap-3 h-9 px-3 rounded-lg text-[13px] font-medium transition-colors group"
        style={{
          color: isActive ? "var(--color-accent-light)" : "var(--color-text-secondary)",
        }}
        title={collapsed ? label : undefined}
      >
        {isActive && (
          <>
            <motion.span
              layoutId="sidebar-active-bg"
              className="absolute inset-0 rounded-lg"
              style={{
                background:
                  "linear-gradient(180deg, var(--color-accent-soft), color-mix(in srgb, var(--color-accent-soft) 50%, transparent))",
                boxShadow:
                  "inset 0 0 0 1px color-mix(in srgb, var(--color-accent) 20%, transparent)",
              }}
              transition={{ type: "spring", stiffness: 380, damping: 32, mass: 0.8 }}
            />
            <motion.span
              layoutId="sidebar-active-bar"
              className="absolute left-0 top-1.5 bottom-1.5 w-[3px] rounded-r-full"
              style={{
                background: "var(--color-accent)",
                boxShadow: "0 0 12px var(--color-accent-glow)",
              }}
              transition={{ type: "spring", stiffness: 380, damping: 32, mass: 0.8 }}
            />
          </>
        )}
        <span
          className={`relative flex-shrink-0 transition-transform duration-150 group-hover:scale-105 ${collapsed ? "mx-auto" : ""}`}
        >
          <Icon className="h-[18px] w-[18px]" strokeWidth={isActive ? 2 : 1.6} />
        </span>
        <AnimatePresence initial={false}>
          {!collapsed && (
            <motion.span
              key="label"
              initial={{ opacity: 0, x: -4 }}
              animate={{ opacity: 1, x: 0 }}
              exit={{ opacity: 0, x: -4 }}
              transition={{ duration: 0.14 }}
              className="relative truncate flex-1 group-hover:text-text-primary transition-colors"
            >
              {label}
            </motion.span>
          )}
        </AnimatePresence>
        {!collapsed && item.badge && (
          <span className="relative flex-shrink-0">
            <BadgeView badge={item.badge} />
          </span>
        )}
        {collapsed && item.badge?.kind === "live" && (
          <span className="absolute top-1.5 right-1.5">
            <LivePulse />
          </span>
        )}
      </NavLink>
    </li>
  );
}

function BadgeView({ badge }: { badge: Badge }) {
  if (badge.kind === "live") {
    return (
      <span className="inline-flex items-center gap-1 h-5 px-1.5 rounded-full bg-live-soft text-[10px] font-semibold uppercase tracking-wider text-live">
        <LivePulse />
        Live
      </span>
    );
  }
  if (badge.kind === "health") {
    const color =
      badge.tone === "ok" ? "bg-success" : badge.tone === "warn" ? "bg-warning" : "bg-error";
    return <span className={`inline-block h-1.5 w-1.5 rounded-full ${color}`} />;
  }
  return (
    <span className="inline-flex items-center justify-center min-w-[20px] h-5 px-1.5 rounded-full bg-bg-active text-[10px] font-semibold text-text-secondary">
      {badge.value}
    </span>
  );
}

function LivePulse() {
  return (
    <span className="relative inline-block h-1.5 w-1.5">
      <span className="absolute inset-0 rounded-full bg-live opacity-75 animate-ping" />
      <span className="relative block h-full w-full rounded-full bg-live" />
    </span>
  );
}

function UserPod({
  collapsed,
  name,
  role,
  initials,
  onLogout,
}: {
  collapsed: boolean;
  name: string;
  role: string;
  initials: string;
  onLogout: () => void;
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const { t } = useTranslation();

  if (collapsed) {
    return (
      <button
        onClick={onLogout}
        className="w-full flex items-center justify-center h-10"
        title={`${name} · ${t("common.logOut")}`}
      >
        <Avatar initials={initials} />
      </button>
    );
  }

  return (
    <div ref={ref} className="relative">
      <button
        onClick={() => setOpen((v) => !v)}
        className="w-full flex items-center gap-2.5 px-2 py-2 rounded-xl border border-border-subtle hover:border-border bg-bg-card/40 hover:bg-bg-card transition-colors text-left"
      >
        <Avatar initials={initials} />
        <div className="flex-1 min-w-0">
          <p className="text-[13px] font-medium text-text-primary truncate leading-tight">{name}</p>
          {role && (
            <p className="text-[11px] text-text-muted truncate leading-tight mt-0.5">{role}</p>
          )}
        </div>
        <span className="text-text-muted">
          <svg width="14" height="14" viewBox="0 0 14 14" fill="none">
            <path
              d="M3 5l4 4 4-4"
              stroke="currentColor"
              strokeWidth="1.5"
              strokeLinecap="round"
              strokeLinejoin="round"
              transform={open ? "rotate(180 7 7)" : undefined}
            />
          </svg>
        </span>
      </button>

      <AnimatePresence>
        {open && (
          <motion.div
            initial={{ opacity: 0, y: 6 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: 6 }}
            transition={{ duration: 0.14, ease: [0.32, 0.72, 0, 1] }}
            className="absolute bottom-full mb-2 left-0 right-0 rounded-xl border border-border bg-bg-overlay shadow-2xl shadow-black/40 overflow-hidden z-50"
          >
            <button
              onClick={onLogout}
              className="w-full flex items-center gap-2.5 px-3 py-2.5 text-[13px] text-text-secondary hover:text-text-primary hover:bg-bg-hover transition-colors"
            >
              <LogOut className="h-[16px] w-[16px]" strokeWidth={1.6} />
              {t("common.logOut")}
            </button>
          </motion.div>
        )}
      </AnimatePresence>

      {open && (
        <button
          aria-hidden
          tabIndex={-1}
          className="fixed inset-0 z-40 cursor-default"
          onClick={() => setOpen(false)}
        />
      )}
    </div>
  );
}

function Avatar({ initials }: { initials: string }) {
  return (
    <span
      className="relative flex h-8 w-8 items-center justify-center rounded-full text-[12px] font-semibold flex-shrink-0 ring-1 ring-accent/30"
      style={{
        background:
          "linear-gradient(135deg, color-mix(in srgb, var(--color-accent) 18%, transparent), color-mix(in srgb, var(--color-accent) 8%, transparent))",
        color: "var(--color-accent-light)",
      }}
    >
      {initials}
      <span
        className="absolute -bottom-0.5 -right-0.5 h-2.5 w-2.5 rounded-full ring-2 ring-bg-base"
        style={{ background: "var(--color-success)" }}
        aria-hidden
      />
    </span>
  );
}
