import { NavLink, Outlet } from "react-router";
import { useTranslation } from "react-i18next";

// Sub-tabs inside the top-level "System" admin page. Mirrors the Plex
// pattern of grouping server detail (Status > Dashboard / Logs / Tasks /
// Activity) under one parent — the user picks "System" once, then
// drills down to the right surface without leaving the admin section.
//
//   status    — the current real-time snapshot (what was the old
//               System tab): server, streaming, runtime, storage.
//   activity  — anything happening *now or scheduled*: live sessions,
//               background tasks, server logs.
//   advanced  — destructive / power-user actions kept off the default
//               view: signing-key rotation, backup, force logout,
//               update channel.
const subTabs = [
  { key: "admin.system.tabs.status", to: "/admin/system/status" },
  { key: "admin.system.tabs.activity", to: "/admin/system/activity" },
  { key: "admin.system.tabs.advanced", to: "/admin/system/advanced" },
] as const;

export default function SystemLayout() {
  const { t } = useTranslation();

  return (
    <div className="flex flex-col gap-6">
      {/* Sub-tab navigation. Visually lighter than the top-level admin
          tabs in AdminLayout so the hierarchy is obvious — small
          rounded pills on a card background instead of bold underline
          tabs. Active state uses the accent colour to match the
          parent tab's active state. */}
      <nav
        aria-label={t("admin.system.tabs.aria")}
        className="flex gap-1 rounded-[--radius-md] border border-border bg-bg-card p-1 self-start"
      >
        {subTabs.map((tab) => (
          <NavLink
            key={tab.to}
            to={tab.to}
            className={({ isActive }) =>
              [
                "px-3 py-1.5 rounded-[--radius-sm] text-sm font-medium transition-colors",
                isActive
                  ? "bg-accent/15 text-accent"
                  : "text-text-secondary hover:text-text-primary",
              ].join(" ")
            }
          >
            {t(tab.key)}
          </NavLink>
        ))}
      </nav>

      <Outlet />
    </div>
  );
}
