import { useTranslation } from "react-i18next";
import { EmptyState } from "@/components/common";

// SystemActivity — sub-tab at /admin/system/activity for everything
// happening *now or scheduled*: live playback sessions, background
// tasks, server logs.
//
// Phase A1 ships the scaffold; the three sections (sessions, tasks,
// logs) are populated by phases B/C/D respectively. Until then this
// page renders friendly placeholders so the navigation works end-to-
// end and the user can see the planned shape.
export default function SystemActivity() {
  const { t } = useTranslation();

  return (
    <div className="flex flex-col gap-8">
      <Section title={t("admin.activity.sessions")}>
        <EmptyState
          title={t("admin.activity.sessionsComingSoon")}
          description={t("admin.activity.sessionsComingSoonHint")}
        />
      </Section>

      <Section title={t("admin.activity.tasks")}>
        <EmptyState
          title={t("admin.activity.tasksComingSoon")}
          description={t("admin.activity.tasksComingSoonHint")}
        />
      </Section>

      <Section title={t("admin.activity.logs")}>
        <EmptyState
          title={t("admin.activity.logsComingSoon")}
          description={t("admin.activity.logsComingSoonHint")}
        />
      </Section>
    </div>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="flex flex-col gap-3">
      <h3 className="text-xs font-semibold uppercase tracking-wider text-text-muted">
        {title}
      </h3>
      {children}
    </section>
  );
}
