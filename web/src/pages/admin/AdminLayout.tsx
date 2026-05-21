import { NavLink, Outlet } from "react-router";
import { useTranslation } from 'react-i18next';

// Pestañas raíz del admin. La pestaña "Servidores" (federación) vivía
// como sección al final de "Usuarios", pero conceptualmente son
// cosas distintas: usuarios = personas con login en este servidor;
// servidores = peers HubPlay que comparten catálogo. Las separo
// para que cada panel tenga un único modelo mental.
const tabs = [
  { key: "admin.tabs.summary", to: "/admin/dashboard" },
  { key: "admin.tabs.library", to: "/admin/libraries" },
  { key: "admin.tabs.users", to: "/admin/users" },
  { key: "admin.tabs.servers", to: "/admin/federation" },
  { key: "admin.tabs.system", to: "/admin/system" },
] as const;

export default function AdminLayout() {
  const { t } = useTranslation();

  return (
    <div className="flex flex-col gap-6 px-4 py-6 sm:px-10 sm:py-8">
      <h1 className="text-2xl font-semibold text-text-primary sm:text-3xl">
        {t('admin.title')}
      </h1>

      {/* Tab Navigation. On narrow viewports the nav scrolls horizontally
          inside its own track (negative margin pulls it to the screen
          edge so the first tab isn't visually clipped) instead of
          forcing the whole page wider than the viewport. */}
      <nav className="-mx-4 flex gap-5 overflow-x-auto border-b border-border px-4 sm:mx-0 sm:gap-6 sm:px-0">
        {tabs.map((tab) => (
          <NavLink
            key={tab.to}
            to={tab.to}
            className={({ isActive }) =>
              [
                "shrink-0 whitespace-nowrap pb-3 text-sm font-medium transition-colors",
                isActive
                  ? "border-b-2 border-accent text-accent"
                  : "text-text-muted hover:text-text-primary",
              ].join(" ")
            }
          >
            {t(tab.key)}
          </NavLink>
        ))}
      </nav>

      {/* Nested route content */}
      <Outlet />
    </div>
  );
}
