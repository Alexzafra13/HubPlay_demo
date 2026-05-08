import { useTranslation } from "react-i18next";
import { Clock, Plus } from "lucide-react";
import { useGenerateInvite, useListInvites } from "@/api/hooks/federation";
import { Button } from "@/components/common/Button";
import { CopyButton, ErrorBanner } from "./_shared";

// InviteSection generates a single-use invite code locally for the
// admin to share out-of-band with another HubPlay admin. Renders bare
// (no <section>/h3 wrapper) because it lives inside a Radix Tab —
// the tab trigger already announces "Generar invite", a nested
// heading would just duplicate. We also list still-active invites so
// an admin can re-copy a code they already generated and didn't
// share yet.

export function InviteSection() {
  const { t } = useTranslation();
  const invites = useListInvites();
  const generate = useGenerateInvite();
  const activeInvites = invites.data ?? [];

  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm leading-relaxed text-text-muted">
        {t("admin.federation.invite.description")}
      </p>

      <div>
        <Button
          variant="primary"
          onClick={() => generate.mutate()}
          disabled={generate.isPending}
        >
          <Plus className="-ml-1 mr-1.5 h-4 w-4" />
          {generate.isPending
            ? t("admin.federation.invite.generating")
            : t("admin.federation.invite.generate")}
        </Button>
      </div>

      {generate.error && <ErrorBanner message={String(generate.error)} />}

      {activeInvites.length > 0 && (
        <div className="flex flex-col gap-2 border-t border-border-subtle pt-4">
          <p className="text-xs font-semibold uppercase tracking-wider text-text-muted">
            {t("admin.federation.invite.activeHeading", {
              defaultValue: "Códigos activos",
            })}
          </p>
          <ul className="flex flex-col gap-2">
            {activeInvites.map((inv) => (
              <li
                key={inv.id}
                className="flex flex-wrap items-center gap-3 rounded-md border border-border bg-bg-base px-3 py-2.5"
              >
                <code className="flex-1 break-all font-mono text-sm font-medium text-accent">
                  {inv.code}
                </code>
                <span className="inline-flex items-center gap-1 text-xs text-text-muted">
                  <Clock className="h-3 w-3" />
                  {t("admin.federation.invite.expiresAt", {
                    when: new Date(inv.expires_at).toLocaleString(),
                  })}
                </span>
                <CopyButton text={inv.code} />
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}
