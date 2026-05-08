import { useState } from "react";
import { NavLink, useLocation } from "react-router";
import { useTranslation } from "react-i18next";
import { motion, AnimatePresence } from "framer-motion";
import { ChevronDown, X, LogOut, Settings, ShieldCheck, Smartphone } from "lucide-react";
import { useAuthStore } from "@/store/auth";
import { useAllPeerLibraries } from "@/api/hooks/federation";
import { getInitials } from "@/utils/userDisplay";
import { avatarColorFor } from "@/utils/avatarColor";
import { MAIN_NAV, PEERS_NAV, type NavItem } from "./navConfig";

// MobileDrawer — replaces the legacy mobile sidebar drawer. Renders
// the same `navConfig` schema as MainNav but stacked: link items as
// rows, menu items as accordions that expand inline. Lives below the
// TopBar (top: var(--topbar-height)) and slides in from the left.
//
// Personal/admin actions (Settings · Vincular dispositivo · Admin ·
// Logout) sit at the bottom inside a fixed pod so they're always
// reachable even if the user has scrolled the nav.

interface MobileDrawerProps {
  onClose: () => void;
  onLogout: () => void;
}

export function MobileDrawer({ onClose, onLogout }: MobileDrawerProps) {
  const { t } = useTranslation();
  const { user } = useAuthStore();
  const isAdmin = user?.role === "admin";
  const initials = getInitials(user);
  const palette = avatarColorFor(user?.username);

  const { data: peerLibs } = useAllPeerLibraries();
  const showPeers = isAdmin && (peerLibs?.length ?? 0) > 0;

  return (
    <aside
      className="h-full w-[88vw] max-w-[320px] flex flex-col select-none"
      style={{
        background:
          "linear-gradient(180deg, rgba(11,15,23,0.96) 0%, rgba(7,9,14,0.98) 100%)",
        backdropFilter: "blur(20px) saturate(140%)",
        borderRight: "1px solid var(--color-border-subtle)",
      }}
    >
      {/* Close button — mobile only, anchors top-right inside the drawer. */}
      <div className="flex items-center justify-end px-3 h-12 flex-shrink-0">
        <button
          onClick={onClose}
          className="p-2 rounded-lg text-text-secondary hover:text-text-primary hover:bg-bg-hover transition-colors"
          aria-label={t("nav.closeMenu")}
        >
          <X className="h-[18px] w-[18px]" strokeWidth={1.6} />
        </button>
      </div>

      {/* Main scrollable nav stack */}
      <nav className="flex-1 overflow-y-auto px-3 pb-3 scrollbar-hide">
        <ul className="flex flex-col gap-0.5">
          {MAIN_NAV.map((item) => (
            <DrawerItem key={item.id} item={item} onNavigate={onClose} />
          ))}
          {showPeers && (
            <PeersDrawerItem
              peerLibs={peerLibs ?? []}
              onNavigate={onClose}
            />
          )}
        </ul>

        {/* Personal section — Settings + Vincular dispositivo + (admin) */}
        <div className="mt-4">
          <p className="px-3 mb-1.5 text-[10px] font-semibold uppercase tracking-[0.12em] text-text-muted">
            {t("nav.account")}
          </p>
          <ul className="flex flex-col gap-0.5">
            <li>
              <DrawerLink
                to="/settings"
                icon={<Settings className="h-[18px] w-[18px]" strokeWidth={1.6} />}
                label={t("nav.settings")}
                onClick={onClose}
              />
            </li>
            <li>
              <DrawerLink
                to="/link"
                icon={<Smartphone className="h-[18px] w-[18px]" strokeWidth={1.6} />}
                label={t("nav.linkDevice")}
                onClick={onClose}
              />
            </li>
            {isAdmin && (
              <li>
                <DrawerLink
                  to="/admin/dashboard"
                  icon={<ShieldCheck className="h-[18px] w-[18px]" strokeWidth={1.6} />}
                  label={t("common.administration")}
                  onClick={onClose}
                />
              </li>
            )}
          </ul>
        </div>
      </nav>

      {/* User pod — pinned bottom; "Cerrar sesión" lives here. */}
      <div className="px-3 pb-3 flex-shrink-0">
        <div className="flex items-center gap-3 p-3 rounded-xl border border-border-subtle bg-bg-card/40">
          <span
            className="relative flex h-9 w-9 items-center justify-center rounded-full text-[12px] font-semibold text-white ring-1 ring-white/15 flex-shrink-0"
            style={{ background: palette.background }}
          >
            {initials}
          </span>
          <div className="flex-1 min-w-0">
            <p className="text-[13px] font-medium text-text-primary truncate leading-tight">
              {user?.display_name || user?.username}
            </p>
            <p className="text-[11px] text-text-muted truncate leading-tight mt-0.5 capitalize">
              {user?.role}
            </p>
          </div>
          <button
            onClick={() => {
              onClose();
              onLogout();
            }}
            className="p-2 rounded-lg text-text-secondary hover:text-text-primary hover:bg-bg-hover transition-colors flex-shrink-0"
            aria-label={t("common.logOut")}
            title={t("common.logOut")}
          >
            <LogOut className="h-[16px] w-[16px]" strokeWidth={1.6} />
          </button>
        </div>
      </div>
    </aside>
  );
}

// ─── Single drawer row (link or expandable menu) ────────────────────

function DrawerItem({
  item,
  onNavigate,
}: {
  item: NavItem;
  onNavigate: () => void;
}) {
  const { t } = useTranslation();
  const Icon = item.icon;
  const label = t(item.labelKey);

  if (item.kind === "link") {
    return (
      <li>
        <DrawerLink
          to={item.to}
          end={item.end}
          icon={<Icon className="h-[18px] w-[18px]" strokeWidth={1.6} />}
          label={label}
          onClick={onNavigate}
        />
      </li>
    );
  }

  return (
    <li>
      <DrawerAccordion
        triggerLabel={label}
        triggerIcon={<Icon className="h-[18px] w-[18px]" strokeWidth={1.6} />}
        primaryHref={item.to}
        groups={item.groups}
        onNavigate={onNavigate}
      />
    </li>
  );
}

function DrawerLink({
  to,
  end,
  icon,
  label,
  onClick,
}: {
  to: string;
  end?: boolean;
  icon: React.ReactNode;
  label: string;
  onClick: () => void;
}) {
  return (
    <NavLink
      to={to}
      end={end}
      onClick={onClick}
      className={({ isActive }) =>
        [
          "flex items-center gap-3 h-10 px-3 rounded-lg text-[13.5px] font-medium transition-colors",
          isActive
            ? "bg-bg-hover text-text-primary"
            : "text-text-secondary hover:text-text-primary hover:bg-bg-hover/60",
        ].join(" ")
      }
    >
      <span className="flex-shrink-0">{icon}</span>
      <span className="truncate">{label}</span>
    </NavLink>
  );
}

function DrawerAccordion({
  triggerLabel,
  triggerIcon,
  primaryHref,
  groups,
  onNavigate,
}: {
  triggerLabel: string;
  triggerIcon: React.ReactNode;
  primaryHref: string;
  groups: { labelKey: string; links: { labelKey: string; to: string }[] }[];
  onNavigate: () => void;
}) {
  const { t } = useTranslation();
  const location = useLocation();
  const base = primaryHref.split("?")[0];
  const isActiveSection =
    location.pathname === base || location.pathname.startsWith(base + "/");
  const [open, setOpen] = useState(isActiveSection);

  return (
    <div>
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className={[
          "w-full flex items-center gap-3 h-10 px-3 rounded-lg text-[13.5px] font-medium transition-colors",
          isActiveSection
            ? "bg-bg-hover text-text-primary"
            : "text-text-secondary hover:text-text-primary hover:bg-bg-hover/60",
        ].join(" ")}
      >
        <span className="flex-shrink-0">{triggerIcon}</span>
        <span className="flex-1 truncate text-left">{triggerLabel}</span>
        <ChevronDown
          className={[
            "h-3.5 w-3.5 flex-shrink-0 transition-transform duration-200",
            open ? "rotate-180" : "rotate-0",
          ].join(" ")}
          strokeWidth={1.7}
        />
      </button>

      <AnimatePresence initial={false}>
        {open && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: "auto", opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={{ duration: 0.18, ease: [0.32, 0.72, 0, 1] }}
            className="overflow-hidden"
          >
            <div className="pl-9 pr-2 pt-1 pb-2 space-y-2.5">
              {groups.map((g) => (
                <div key={g.labelKey}>
                  <p className="px-2 mb-1 text-[10px] font-semibold uppercase tracking-[0.12em] text-text-muted">
                    {t(g.labelKey)}
                  </p>
                  <ul className="flex flex-col gap-0.5">
                    {g.links.map((link) => (
                      <li key={link.to}>
                        <NavLink
                          to={link.to}
                          onClick={onNavigate}
                          className="block px-2 py-1.5 rounded-md text-[12.5px] text-text-secondary hover:text-text-primary hover:bg-bg-hover transition-colors"
                        >
                          {t(link.labelKey)}
                        </NavLink>
                      </li>
                    ))}
                  </ul>
                </div>
              ))}
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

// ─── Peers (dynamic, admin-only) ────────────────────────────────────

function PeersDrawerItem({
  peerLibs,
  onNavigate,
}: {
  peerLibs: import("@/api/types").FederationUnifiedLibrary[];
  onNavigate: () => void;
}) {
  const { t } = useTranslation();
  const Icon = PEERS_NAV.icon;
  const label = t(PEERS_NAV.labelKey);

  // Group libraries by peer (same shape as MainNav).
  const grouped = new Map<
    string,
    { name: string; libs: typeof peerLibs }
  >();
  for (const row of peerLibs) {
    const entry = grouped.get(row.peer_id);
    if (entry) entry.libs.push(row);
    else grouped.set(row.peer_id, { name: row.peer_name, libs: [row] });
  }

  const [open, setOpen] = useState(false);

  return (
    <li>
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className="w-full flex items-center gap-3 h-10 px-3 rounded-lg text-[13.5px] font-medium text-text-secondary hover:text-text-primary hover:bg-bg-hover/60 transition-colors"
      >
        <span className="flex-shrink-0">
          <Icon className="h-[18px] w-[18px]" strokeWidth={1.6} />
        </span>
        <span className="flex-1 truncate text-left">{label}</span>
        <ChevronDown
          className={[
            "h-3.5 w-3.5 flex-shrink-0 transition-transform duration-200",
            open ? "rotate-180" : "rotate-0",
          ].join(" ")}
          strokeWidth={1.7}
        />
      </button>

      <AnimatePresence initial={false}>
        {open && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: "auto", opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={{ duration: 0.18, ease: [0.32, 0.72, 0, 1] }}
            className="overflow-hidden"
          >
            <div className="pl-9 pr-2 pt-1 pb-2 space-y-2.5">
              {Array.from(grouped.entries()).map(([peerId, { name, libs }]) => (
                <div key={peerId}>
                  <p
                    className="px-2 mb-1 text-[10px] font-semibold uppercase tracking-[0.12em] text-text-muted truncate"
                    title={name}
                  >
                    {name}
                  </p>
                  <ul className="flex flex-col gap-0.5">
                    {libs.map((lib) => (
                      <li key={lib.library_id}>
                        <NavLink
                          to={`/peers/${peerId}/libraries/${lib.library_id}`}
                          onClick={onNavigate}
                          className="block px-2 py-1.5 rounded-md text-[12.5px] text-text-secondary hover:text-text-primary hover:bg-bg-hover transition-colors truncate"
                        >
                          {lib.library_name}
                        </NavLink>
                      </li>
                    ))}
                  </ul>
                </div>
              ))}
              <NavLink
                to={PEERS_NAV.to}
                onClick={onNavigate}
                className="block px-2 py-1.5 rounded-md text-[12.5px] font-semibold text-accent hover:bg-bg-hover transition-colors"
              >
                {t("navMenu.peers.viewAll", { defaultValue: "Ver todos" })} →
              </NavLink>
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </li>
  );
}
