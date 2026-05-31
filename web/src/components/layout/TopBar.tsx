import { useEffect, useRef, useState } from "react";
import { useNavigate, NavLink } from "react-router";
import { useTranslation } from "react-i18next";
import { m, AnimatePresence } from "framer-motion";
import {
  Menu,
  LogOut,
  Settings as SettingsIcon,
  ShieldCheck,
  Smartphone,
  Upload,
  UserCog,
} from "lucide-react";
import { useMe, useProfiles } from "@/api/hooks";
import { useAuthStore } from "@/store/auth";
import { useIsMobile } from "@/hooks/useIsMobile";
import { UserAvatar } from "@/components/common";
import { NotificationsBell } from "@/components/notifications/NotificationsBell";
import { BrandWordmark } from "./BrandWordmark";
import { SearchBar } from "./SearchBar";
import { MainNav } from "./MainNav";

interface TopBarProps {
  /** Toggles mobile drawer (only shown <md). */
  onMobileMenuClick: () => void;
}

export function TopBar({ onMobileMenuClick }: TopBarProps) {
  const { t } = useTranslation();
  const { user, logout } = useAuthStore();
  const navigate = useNavigate();
  const scrolled = useScrolledPast(8);

  // No calculamos iniciales aquí — UserAvatar las deriva sólo
  // cuando el usuario no tiene foto subida. El componente vive
  // dentro de UserAvatarMenu y se entera del color/imagen vía
  // useMe (datos frescos en lugar del cache de login).

  return (
    <header
      className={[
        // px-4 md:px-8 buys the brand mark visible breathing room from
        // the viewport edge — the previous md:px-4 (16px) read as
        // "stuck to the left" against the search bar and avatar
        // sitting comfortably to the right with their own padding.
        "fixed top-0 left-0 right-0 z-40 flex items-center gap-3 px-4 md:px-8",
        "transition-[background-color,backdrop-filter,border-color] duration-200",
        scrolled
          ? "bg-bg-base/85 backdrop-blur-xl border-b border-border-subtle"
          : "bg-bg-base/70 backdrop-blur-xl border-b border-border-subtle/60",
      ].join(" ")}
      style={{ height: "var(--topbar-height)" }}
    >
      {/* Hamburger — mobile only. Desktop has no sidebar to toggle. */}
      <button
        onClick={onMobileMenuClick}
        className="flex md:hidden items-center justify-center size-10 rounded-lg text-text-secondary hover:text-text-primary hover:bg-bg-hover transition-colors"
        aria-label={t("nav.toggleMenu")}
      >
        <Menu className="size-[19px]" strokeWidth={1.7} />
      </button>

      {/* Brand */}
      <NavLink
        to="/"
        end
        aria-label="HubPlay"
        className="flex items-center px-1 py-1.5 rounded-lg hover:bg-bg-hover/60 transition-colors min-w-0 flex-shrink-0"
      >
        <BrandWordmark height={32} />
      </NavLink>

      {/* Center nav — desktop only; on mobile the drawer holds it. */}
      <div className="flex-1 flex items-center justify-center min-w-0">
        <MainNav />
      </div>

      {/* Search — animated icon → input expansion. ⌘K opens from anywhere.
          On /live-tv it switches to filter-mode and mirrors `?q=` to
          the URL so the page filters channels in place (same pattern
          /movies and /series already use). See FILTER_ROUTES inside
          SearchBar for the routing list. */}
      <SearchBar />

      {/* Notifications bell — el componente devuelve null cuando no
          hay notificaciones (leidas + no leidas en cero). Posicion
          deliberada justo a la izquierda del avatar para que se lea
          como parte del cluster de "tu perfil" en la derecha. */}
      <NotificationsBell />

      {/* User avatar dropdown — single home for all personal/admin actions */}
      <UserAvatarMenu
        user={user}
        onLogout={() => {
          logout();
          navigate("/login");
        }}
        isAdmin={user?.role === "admin"}
      />
    </header>
  );
}

// ─── User avatar dropdown ───────────────────────────────────────────

function UserAvatarMenu({
  user,
  isAdmin,
  onLogout,
}: {
  user: { display_name?: string | null; username: string; role: string } | null;
  isAdmin: boolean;
  onLogout: () => void;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  // Only show "Cambiar perfil" when there's actually more than one
  // profile under this account. Solo deploys (parent only) had the
  // link sitting in the menu doing nothing — clicking it would land
  // on /select-profile, which auto-bounces home, so the affordance
  // was lying about availability.
  const { data: profiles } = useProfiles();
  const canSwitchProfile = (profiles?.length ?? 0) > 1;

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

  // useMe da datos frescos (incluye avatar_image_url cuando el usuario
  // sube foto desde Settings); useAuthStore.user es el cache del login
  // y no incluye la foto, así que sirve sólo de fallback para el
  // username/role mientras /me todavía está resolviendo.
  const { data: me } = useMe();
  const avatarUser = me ?? (user ? { username: user.username, display_name: user.display_name ?? "" } : null);
  const isMobile = useIsMobile();

  // Enlaces de cuenta para el drawer de móvil. El dropdown de desktop
  // de abajo los mantiene inline; mantener ambas listas en sync si se
  // añade una entrada nueva.
  const accountLinks: {
    to: string;
    Icon: React.ComponentType<{ className?: string; strokeWidth?: number }>;
    label: string;
  }[] = [
    { to: "/settings", Icon: SettingsIcon, label: t("nav.settings") },
    { to: "/link", Icon: Smartphone, label: t("nav.linkDevice") },
    ...(me?.can_upload
      ? [{ to: "/uploads", Icon: Upload, label: t("nav.uploads") }]
      : []),
    ...(canSwitchProfile
      ? [{ to: "/select-profile", Icon: UserCog, label: t("topbar.switchProfile") }]
      : []),
    ...(isAdmin
      ? [{ to: "/admin", Icon: ShieldCheck, label: t("common.administration") }]
      : []),
  ];

  return (
    <div ref={ref} className="relative flex-shrink-0">
      <button
        onClick={() => setOpen((v) => !v)}
        className="relative flex items-center justify-center size-9 rounded-full ring-1 ring-white/15 hover:ring-white/35 transition-all"
        aria-label={t("topbar.userMenu")}
        aria-haspopup="menu"
        aria-expanded={open}
      >
        <UserAvatar user={avatarUser} size="md" />
      </button>

      {/* Desktop: dropdown flotante. En móvil usamos el drawer de abajo
          (mismo lenguaje que el hamburguesa de la izquierda). */}
      <AnimatePresence>
        {open && !isMobile && (
          <m.div
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
              <SettingsIcon className="size-[15px]" strokeWidth={1.6} />
              {t("nav.settings")}
            </NavLink>
            <NavLink
              to="/link"
              onClick={() => setOpen(false)}
              className="flex items-center gap-2.5 px-3 py-2.5 text-[13px] text-text-secondary hover:text-text-primary hover:bg-bg-hover transition-colors"
              role="menuitem"
            >
              <Smartphone className="size-[15px]" strokeWidth={1.6} />
              {t("nav.linkDevice")}
            </NavLink>
            {me?.can_upload && (
              <NavLink
                to="/uploads"
                onClick={() => setOpen(false)}
                className="flex items-center gap-2.5 px-3 py-2.5 text-[13px] text-text-secondary hover:text-text-primary hover:bg-bg-hover transition-colors"
                role="menuitem"
              >
                <Upload className="size-[15px]" strokeWidth={1.6} />
                {t("nav.uploads")}
              </NavLink>
            )}
            {canSwitchProfile && (
              <NavLink
                to="/select-profile"
                onClick={() => setOpen(false)}
                className="flex items-center gap-2.5 px-3 py-2.5 text-[13px] text-text-secondary hover:text-text-primary hover:bg-bg-hover transition-colors"
                role="menuitem"
              >
                <UserCog className="size-[15px]" strokeWidth={1.6} />
                {t("topbar.switchProfile")}
              </NavLink>
            )}
            {isAdmin && (
              <NavLink
                to="/admin"
                onClick={() => setOpen(false)}
                className="flex items-center gap-2.5 px-3 py-2.5 text-[13px] text-text-secondary hover:text-text-primary hover:bg-bg-hover transition-colors"
                role="menuitem"
              >
                <ShieldCheck className="size-[15px]" strokeWidth={1.6} />
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
              <LogOut className="size-[15px]" strokeWidth={1.6} />
              {t("common.logOut")}
            </button>
          </m.div>
        )}
      </AnimatePresence>

      {/* Móvil: drawer de cuenta desde la derecha. El hamburguesa de la
          izquierda lleva la navegación; este lleva la cuenta (perfil +
          ajustes + logout). Cada menú una función, sin solape. */}
      <AnimatePresence>
        {open && isMobile && (
          <>
            <m.button
              type="button"
              aria-label={t("common.close")}
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              transition={{ duration: 0.2 }}
              onClick={() => setOpen(false)}
              className="fixed inset-0 z-40 bg-black/60 backdrop-blur-sm md:hidden cursor-default"
              style={{ top: "var(--topbar-height)" }}
            />
            <m.aside
              initial={{ x: "100%" }}
              animate={{ x: 0 }}
              exit={{ x: "100%" }}
              transition={{ duration: 0.25, ease: [0.32, 0.72, 0, 1] }}
              className="fixed right-0 z-40 flex w-[80vw] max-w-[300px] flex-col md:hidden"
              style={{
                top: "var(--topbar-height)",
                height: "calc(100dvh - var(--topbar-height))",
                background:
                  "linear-gradient(180deg, rgba(11,15,23,0.96) 0%, rgba(7,9,14,0.98) 100%)",
                backdropFilter: "blur(8px) saturate(140%)",
                borderLeft: "1px solid var(--color-border-subtle)",
              }}
              role="menu"
            >
              <div className="flex items-center gap-3 border-b border-border-subtle px-4 py-4">
                <UserAvatar user={avatarUser} size="md" className="flex-shrink-0" />
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium text-text-primary">
                    {user?.display_name || user?.username}
                  </p>
                  <p className="mt-0.5 text-[11px] capitalize text-text-muted">
                    {user?.role}
                  </p>
                </div>
              </div>
              <nav className="scrollbar-hide flex-1 overflow-y-auto px-3 py-3">
                <ul className="flex flex-col gap-0.5">
                  {accountLinks.map((l) => (
                    <li key={l.to}>
                      <NavLink
                        to={l.to}
                        onClick={() => setOpen(false)}
                        className={({ isActive }) =>
                          [
                            "flex h-11 items-center gap-3 rounded-lg px-3 text-[13.5px] font-medium transition-colors",
                            isActive
                              ? "bg-bg-hover text-text-primary"
                              : "text-text-secondary hover:bg-bg-hover/60 hover:text-text-primary",
                          ].join(" ")
                        }
                        role="menuitem"
                      >
                        <l.Icon className="size-[18px]" strokeWidth={1.6} />
                        <span className="truncate">{l.label}</span>
                      </NavLink>
                    </li>
                  ))}
                </ul>
              </nav>
              <div className="flex-shrink-0 px-3 pb-3">
                <button
                  onClick={() => {
                    setOpen(false);
                    onLogout();
                  }}
                  className="flex h-11 w-full items-center gap-3 rounded-lg px-3 text-[13.5px] font-medium text-text-secondary transition-colors hover:bg-bg-hover hover:text-text-primary"
                  role="menuitem"
                >
                  <LogOut className="size-[18px]" strokeWidth={1.6} />
                  {t("common.logOut")}
                </button>
              </div>
            </m.aside>
          </>
        )}
      </AnimatePresence>
    </div>
  );
}

// ─── Scroll-aware backdrop hook ─────────────────────────────────────

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
