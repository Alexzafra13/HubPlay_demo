import { useTranslation } from "react-i18next";
import { ExternalLink } from "lucide-react";
import { IndexersPanel } from "@/components/admin/IndexersPanel";

// IndexersAdmin — centralized admin home for the torrent/source stack:
// manage Torznab/Prowlarr indexers, plus quick links to the bundled
// Prowlarr and FlareSolverr UIs (served under /prowlarr on the same
// domain when using the bundled reverse-proxy setup).
export default function IndexersAdmin() {
  const { t } = useTranslation();

  // Same-origin path-based links (work local + remote via the proxy).
  const origin = typeof window !== "undefined" ? window.location.origin : "";

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-center gap-2">
        <a
          href={`${origin}/prowlarr/`}
          target="_blank"
          rel="noreferrer"
          className="inline-flex items-center gap-1.5 rounded-lg border border-border bg-bg-card px-3 py-2 text-sm text-text-secondary transition-colors hover:border-border-strong hover:text-text-primary"
        >
          <ExternalLink className="size-4" />
          {t("indexersAdmin.openProwlarr", { defaultValue: "Abrir Prowlarr" })}
        </a>
        <p className="text-xs text-text-muted">
          {t("indexersAdmin.prowlarrHint", {
            defaultValue:
              "En Prowlarr añades/activas los trackers. Para los protegidos por Cloudflare, usa el proxy FlareSolverr.",
          })}
        </p>
      </div>

      <IndexersPanel />
    </div>
  );
}
