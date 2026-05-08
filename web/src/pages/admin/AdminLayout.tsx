import { NavLink, Outlet } from "react-router";
import { useTranslation } from 'react-i18next';

// Top-level admin tabs. Collapsed from the previous six (Dashboard,
// Libraries, Providers, Users, Federation, System) to four because:
//   - Providers is a property of the catalogue, not its own entity →
//     merged inside "Biblioteca" as a section under the libraries grid.
//   - Federation is an advanced feature most installs never touch →
//     merged inside "Usuarios" as a "Servidores conectados" section.
//   - System lost its sub-tabs (Status / Activity / Advanced) and
//     became a single page modelled after macOS Settings: stacked
//     sections with the destructive ones at the bottom.
//   - Dashboard renamed to "Resumen" because the page is no longer a
//     bento of stat cards — it's an editorial overview.
const tabs = [
  { key: "admin.tabs.summary", to: "/admin/dashboard" },
  { key: "admin.tabs.library", to: "/admin/libraries" },
  { key: "admin.tabs.users", to: "/admin/users" },
  { key: "admin.tabs.system", to: "/admin/system" },
] as const;

export default function AdminLayout() {
  const { t } = useTranslation();

  return (
    <div className="flex flex-col gap-6 px-4 py-6 sm:px-10 sm:py-8">
      <h1 className="text-2xl font-bold text-text-primary sm:text-3xl">
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
