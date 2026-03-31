import { NavLink, Outlet } from "react-router";
import { useTranslation } from 'react-i18next';

const tabs = [
  { key: "admin.tabs.libraries", to: "/admin/libraries" },
  { key: "admin.tabs.providers", to: "/admin/providers" },
  { key: "admin.tabs.users", to: "/admin/users" },
  { key: "admin.tabs.system", to: "/admin/system" },
] as const;

export default function AdminLayout() {
  const { t } = useTranslation();

  return (
    <div className="flex flex-col gap-6 px-6 py-8 sm:px-10">
      <h1 className="text-2xl font-bold text-text-primary sm:text-3xl">
        {t('admin.title')}
      </h1>

      {/* Tab Navigation */}
      <nav className="flex gap-6 border-b border-border">
        {tabs.map((tab) => (
          <NavLink
            key={tab.to}
            to={tab.to}
            className={({ isActive }) =>
              [
                "pb-3 text-sm font-medium transition-colors",
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
