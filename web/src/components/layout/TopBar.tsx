import { useEffect, useRef, useState } from "react";
import { useNavigate, NavLink } from "react-router";
import { useTranslation } from "react-i18next";
import { motion, AnimatePresence } from "framer-motion";
import { Menu, Search as SearchIcon, LogOut, Settings as SettingsIcon, ShieldCheck } from "lucide-react";
import { useAuthStore } from "@/store/auth";
import { getInitials } from "@/utils/userDisplay";
import { useTopBarSlotContent } from "./TopBarSlot";
import { BrandMark } from "./BrandMark";
import { SearchPopover } from "./SearchPopover";

interface TopBarProps {
  title?: string;
  /** Toggles desktop collapse on md+; toggles mobile drawer below. */
  onMenuClick: () => void;
  /** Drives the hamburger icon's animated state hint. */
  sidebarCollapsed: boolean;
}

export function TopBar({ title, onMenuClick, sidebarCollapsed }: TopBarProps) {
  const { t } = useTranslation();
  const { user, logout } = useAuthStore();
  const navigate = useNavigate();
  const slotContent = useTopBarSlotContent();
  const scrolled = useScrolledPast(8);

  const [searchOpen, setSearchOpen] = useState(false);
  const searchTriggerRef = useRef<HTMLButtonElement>(null);
  const initials = getInitials(user);

  // ⌘K / Ctrl+K opens the search popover from anywhere.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setSearchOpen(true);
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  return (
    <>
      <header
        className={[
          "fixed top-0 left-0 right-0 z-40 flex items-center gap-3 px-3 md:px-4",
          "transition-[background-color,backdrop-filter,border-color] duration-200",
          scrolled
            ? "bg-bg-base/85 backdrop-blur-xl border-b border-border-subtle"
            : "bg-bg-base/70 backdrop-blur-xl border-b border-border-subtle/60",
        ].join(" ")}
        style={{ height: "var(--topbar-height)" }}
      >
        {/* Hamburger — toggles sidebar (collapse on desktop, drawer on mobile) */}
        <button
          onClick={onMenuClick}
          className="flex items-center justify-center w-10 h-10 rounded-lg text-text-secondary hover:text-text-primary hover:bg-bg-hover transition-colors"
          aria-label={t("nav.toggleMenu")}
          aria-pressed={!sidebarCollapsed}
        >
          <Menu className="h-[19px] w-[19px]" strokeWidth={1.7} />
        </button>

        {/* Brand */}
        <NavLink
          to="/"
          end
          className="flex items-center gap-2.5 px-1 py-1.5 rounded-lg hover:bg-bg-hover/60 transition-colors min-w-0"
        >
          <BrandMark size={30} />
          <span className="text-[15px] font-semibold tracking-tight text-text-primary truncate hidden sm:inline">
            HubPlay
          </span>
        </NavLink>

        {/* Optional page title (rare — most pages render their own
            PageHeader inside the content area instead). */}
        {title && (
          <div className="hidden md:flex items-center gap-2 ml-2 pl-3 border-l border-border-subtle min-w-0">
            <span className="text-[13px] text-text-secondary truncate">{title}</span>
          </div>
        )}

        <div className="flex-1 min-w-0">
          {/* Page-injected controls (LiveTV tabs, page filters) live
              centered in the available space so they don't fight with
              the brand or the right-side actions. */}
          {slotContent}
        </div>

        {/* Search trigger */}
        <button
          ref={searchTriggerRef}
          onClick={() => setSearchOpen(true)}
          className="hidden sm:flex items-center gap-2 h-9 pl-2.5 pr-2 rounded-lg bg-bg-hover/40 hover:bg-bg-active border border-border-subtle hover:border-border text-text-secondary hover:text-text-primary transition-colors"
          aria-label={t("nav.search")}
        >
          <SearchIcon className="h-[16px] w-[16px]" strokeWidth={1.7} />
          <span className="hidden md:inline text-[12.5px] text-text-muted pr-2">
            {t("nav.search")}
          </span>
          <kbd className="hidden md:inline-flex items-center gap-0.5 px-1.5 py-0.5 rounded text-[10px] font-medium bg-bg-base/60 border border-border-subtle text-text-muted">
            ⌘K
          </kbd>
        </button>

        <button
          onClick={() => setSearchOpen(true)}
          className="flex sm:hidden items-center justify-center w-10 h-10 rounded-lg text-text-secondary hover:text-text-primary hover:bg-bg-hover transition-colors"
          aria-label={t("nav.search")}
        >
          <SearchIcon className="h-[18px] w-[18px]" strokeWidth={1.7} />
        </button>

        {/* User avatar dropdown */}
        <UserAvatarMenu
          user={user}
          initials={initials}
          onLogout={() => {
            logout();
            navigate("/login");
          }}
          isAdmin={user?.role === "admin"}
        />
      </header>

      <SearchPopover open={searchOpen} onClose={() => setSearchOpen(false)} />
    </>
  );
}

// ─── User avatar dropdown ───────────────────────────────────────────────────

function UserAvatarMenu({
  user,
  initials,
  isAdmin,
  onLogout,
}: {
  user: { display_name?: string | null; username: string; role: string } | null;
  initials: string;
  isAdmin: boolean;
  onLogout: () => void;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    function onDocClick(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        setOpen(false);
      }
    }
    document.addEventListener("mousedown", onDocClick);
    return () => document.removeEventListener("mousedown", onDocClick);
  }, [open]);

  return (
    <div ref={ref} className="relative">
      <button
        onClick={() => setOpen((v) => !v)}
        className="relative flex items-center justify-center h-9 w-9 rounded-full text-[12px] font-semibold ring-1 ring-accent/30 hover:ring-accent/60 transition-all"
        style={{
          background:
            "linear-gradient(135deg, color-mix(in srgb, var(--color-accent) 20%, transparent), color-mix(in srgb, var(--color-accent) 8%, transparent))",
          color: "var(--color-accent-light)",
        }}
        aria-label={t("topbar.userMenu")}
        aria-haspopup="menu"
        aria-expanded={open}
      >
        {initials}
        <span
          className="absolute -bottom-0.5 -right-0.5 h-2.5 w-2.5 rounded-full ring-2 ring-bg-base"
          style={{ background: "var(--color-success)" }}
          aria-hidden
        />
      </button>

      <AnimatePresence>
        {open && (
          <motion.div
            initial={{ opacity: 0, y: -6, scale: 0.98 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: -6, scale: 0.98 }}
            transition={{ duration: 0.15, ease: [0.32, 0.72, 0, 1] }}
            className="absolute right-0 top-full mt-2 w-60 rounded-xl border border-border bg-bg-overlay shadow-2xl shadow-black/50 overflow-hidden z-50"
            role="menu"
          >
            <div className="px-3 py-2.5 border-b border-border-subtle">
              <p className="text-[13px] font-medium text-text-primary truncate">
                {user?.display_name || user?.username}
              </p>
              <p className="text-[11px] text-text-muted capitalize mt-0.5">
                {user?.role}
              </p>
            </div>
            <NavLink
              to="/settings"
              onClick={() => setOpen(false)}
              className="flex items-center gap-2.5 px-3 py-2.5 text-[13px] text-text-secondary hover:text-text-primary hover:bg-bg-hover transition-colors"
              role="menuitem"
            >
              <SettingsIcon className="h-[15px] w-[15px]" strokeWidth={1.6} />
              {t("common.settings")}
            </NavLink>
            {isAdmin && (
              <NavLink
                to="/admin"
                onClick={() => setOpen(false)}
                className="flex items-center gap-2.5 px-3 py-2.5 text-[13px] text-text-secondary hover:text-text-primary hover:bg-bg-hover transition-colors"
                role="menuitem"
              >
                <ShieldCheck className="h-[15px] w-[15px]" strokeWidth={1.6} />
                {t("common.administration")}
              </NavLink>
            )}
            <div className="border-t border-border-subtle" />
            <button
              onClick={() => {
                setOpen(false);
                onLogout();
              }}
              className="w-full flex items-center gap-2.5 px-3 py-2.5 text-[13px] text-text-secondary hover:text-text-primary hover:bg-bg-hover transition-colors"
              role="menuitem"
            >
              <LogOut className="h-[15px] w-[15px]" strokeWidth={1.6} />
              {t("common.logOut")}
            </button>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

// ─── Scroll-aware backdrop hook ─────────────────────────────────────────────

/**
 * useScrolledPast — returns true once `window.scrollY` exceeds `threshold`.
 * Throttled with rAF to avoid layout-thrash spirals.
 */
function useScrolledPast(threshold: number): boolean {
  const [scrolled, setScrolled] = useState(() => {
    if (typeof window === "undefined") return false;
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
    window.addEventListener("scroll", onScroll, { passive: true });
    onScroll();
    return () => window.removeEventListener("scroll", onScroll);
  }, [threshold]);
  return scrolled;
}
