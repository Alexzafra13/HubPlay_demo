import { useTranslation } from "react-i18next";
import { useGenerateInvite, useListInvites } from "@/api/hooks/federation";
import { Button } from "@/components/common/Button";
import { CopyButton, ErrorBanner } from "./_shared";

// InviteSection generates a single-use invite code locally for the
// admin to share out-of-band with another HubPlay admin. The remote
// admin pastes it into THEIR AcceptSection; the handshake completes
// server-to-server. We also list still-active invites so an admin
// can re-copy a code they already generated.

export function InviteSection() {
  const { t } = useTranslation();
  const invites = useListInvites();
  const generate = useGenerateInvite();

  return (
    <section>
      <h3 className="mb-3 text-sm font-semibold uppercase tracking-wide text-text-muted">
        {t("admin.federation.invite.heading")}
      </h3>
      <div className="rounded-lg border border-border bg-bg-elevated p-5">
        <p className="mb-4 text-sm text-text-muted">
          {t("admin.federation.invite.description")}
        </p>
        <Button
          variant="primary"
          onClick={() => generate.mutate()}
          disabled={generate.isPending}
        >
          {generate.isPending
            ? t("admin.federation.invite.generating")
            : t("admin.federation.invite.generate")}
        </Button>
        {generate.error && (
          <ErrorBanner className="mt-3" message={String(generate.error)} />
        )}

        {invites.data && invites.data.length > 0 && (
          <ul className="mt-5 flex flex-col gap-2">
            {invites.data.map((inv) => (
              <li
                key={inv.id}
                className="flex flex-wrap items-center gap-2 rounded border border-border bg-bg-base p-3"
              >
                <code className="flex-1 break-all font-mono text-sm text-accent">
                  {inv.code}
                </code>
                <CopyButton text={inv.code} />
                <span className="text-xs text-text-muted">
                  {t("admin.federation.invite.expiresAt", {
                    when: new Date(inv.expires_at).toLocaleString(),
                  })}
                </span>
              </li>
            ))}
          </ul>
        )}
      </div>
    </section>
  );
}
