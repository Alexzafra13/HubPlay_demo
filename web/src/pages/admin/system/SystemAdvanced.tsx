import { useTranslation } from "react-i18next";
import { AuthKeysPanel } from "@/components/admin/AuthKeysPanel";

// SystemAdvanced — sub-tab at /admin/system/advanced for power-user and
// destructive actions: signing-key rotation (live), database backup,
// force-logout, update channel.
//
// Phase A1 hosts the existing AuthKeysPanel here (moved from the old
// System page). Phases E/F/G add backup, update check, and force
// logout to this same page so everything sensitive lives behind one
// click and the eye doesn't land on it from the default view.
//
// The page leads with a banner so the admin has a moment to register
// they're in the destructive area — Plex does the same in its
// "Settings > Advanced" prefix.
export default function SystemAdvanced() {
  const { t } = useTranslation();

  return (
    <div className="flex flex-col gap-8">
      <div
        role="note"
        className="rounded-[--radius-md] border border-warning/30 bg-warning/10 px-4 py-3 text-sm text-warning"
      >
        {t("admin.advanced.warning")}
      </div>

      <AuthKeysPanel />
    </div>
  );
}
