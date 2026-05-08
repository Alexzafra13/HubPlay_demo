import { useTranslation } from "react-i18next";
import * as Tabs from "@radix-ui/react-tabs";
import { Inbox, Send, Shield } from "lucide-react";
import { usePeers, useServerIdentity } from "@/api/hooks/federation";
import { Spinner } from "@/components/common";
import { AcceptSection } from "./federation/AcceptSection";
import { ErrorBanner } from "./federation/_shared";
import { IdentityCard } from "./federation/IdentityCard";
import { InviteSection } from "./federation/InviteSection";
import { PeersTable } from "./federation/PeersTable";

// FederationAdmin — admin tab for HubPlay-to-HubPlay peering. Pure
// orchestrator. The page reads as three blocks of decreasing
// "you-vs-them" hierarchy:
//
//   1. "Mi servidor" (left column) — the identity the admin
//      publishes, anchored by the fingerprint as the visual hero. It
//      stays in view while you operate the right column so the
//      remote admin can ask "¿qué huella tienes?" mid-handshake.
//
//   2. "Conectar con otro" (right column) — Radix Tabs split into
//      `Generar invite` and `Aceptar invite`. Only one mode is
//      visible at a time so the admin doesn't accidentally fill the
//      wrong form. The flows are mutually exclusive (you're either
//      the inviting side or the accepting side) so tabs match the
//      mental model perfectly.
//
//   3. "Servidores emparejados" (full-width below) — the read/manage
//      surface for everything you've already paired with.

export default function FederationAdmin() {
  const { t } = useTranslation();
  const identity = useServerIdentity();
  const peers = usePeers();
  const peerCount = peers.data?.length ?? 0;

  return (
    <div className="flex flex-col gap-8">
      <header className="flex items-start gap-3">
        <div className="rounded-lg bg-accent/10 p-2.5 text-accent">
          <Shield className="h-5 w-5" />
        </div>
        <div className="flex-1">
          <h2 className="text-xl font-semibold text-text-primary">
            {t("admin.federation.title")}
          </h2>
          <p className="mt-1 max-w-3xl text-sm text-text-muted">
            {t("admin.federation.subtitle")}
          </p>
        </div>
      </header>

      {/* Hero row: identity (anchor) on the left, connect tabs on
          the right. Identity stays visible during handshake so the
          admin can read fingerprint + words to the remote admin
          without scrolling — `lg:sticky lg:top-4 self-start` pins
          the column while the right side scrolls past it. */}
      <div className="grid items-start gap-6 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.1fr)]">
        <section className="lg:sticky lg:top-4 self-start">
          <SectionHeading>
            {t("admin.federation.identity.heading")}
          </SectionHeading>
          {identity.isLoading ? (
            <Spinner />
          ) : identity.error ? (
            <ErrorBanner message={String(identity.error)} />
          ) : identity.data ? (
            <IdentityCard info={identity.data} />
          ) : null}
        </section>

        <section>
          <SectionHeading>
            {t("admin.federation.connect.heading", {
              defaultValue: "Conectar con otro servidor",
            })}
          </SectionHeading>
          <ConnectTabs />
        </section>
      </div>

      <section>
        <div className="mb-3 flex items-center justify-between">
          <SectionHeading className="mb-0">
            {t("admin.federation.peers.heading")}
          </SectionHeading>
          {peerCount > 0 && (
            <span className="rounded-full border border-border-subtle bg-bg-elevated px-2 py-0.5 text-[10px] font-medium text-text-secondary">
              {peerCount === 1
                ? t("admin.federation.peers.count.one", {
                    defaultValue: "1 servidor",
                  })
                : t("admin.federation.peers.count.other", {
                    defaultValue: "{{count}} servidores",
                    count: peerCount,
                  })}
            </span>
          )}
        </div>
        {peers.isLoading ? (
          <Spinner />
        ) : peers.error ? (
          <ErrorBanner message={String(peers.error)} />
        ) : (
          <PeersTable peers={peers.data ?? []} />
        )}
      </section>
    </div>
  );
}

function SectionHeading({
  children,
  className = "mb-3",
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <h3
      className={`text-xs font-semibold uppercase tracking-[0.08em] text-text-muted ${className}`}
    >
      {children}
    </h3>
  );
}

// ConnectTabs — Radix Tabs wrapping the existing Invite/Accept
// sub-pages. Each Section now renders bare (no <section>/h3 of its
// own) since the tab trigger already states which mode you're in;
// keeping a duplicate heading inside would just add noise.
function ConnectTabs() {
  const { t } = useTranslation();
  return (
    <Tabs.Root
      defaultValue="invite"
      className="overflow-hidden rounded-lg border border-border bg-bg-elevated"
    >
      <Tabs.List className="flex border-b border-border bg-bg-card/40">
        <Tabs.Trigger
          value="invite"
          className="group flex flex-1 items-center justify-center gap-2 px-4 py-3 text-sm font-medium text-text-muted transition-colors hover:text-text-primary data-[state=active]:bg-bg-elevated data-[state=active]:text-text-primary data-[state=active]:shadow-[inset_0_-2px_0_0_var(--color-accent)]"
        >
          <Send className="h-4 w-4" />
          {t("admin.federation.invite.tab", {
            defaultValue: "Generar invite",
          })}
        </Tabs.Trigger>
        <Tabs.Trigger
          value="accept"
          className="group flex flex-1 items-center justify-center gap-2 px-4 py-3 text-sm font-medium text-text-muted transition-colors hover:text-text-primary data-[state=active]:bg-bg-elevated data-[state=active]:text-text-primary data-[state=active]:shadow-[inset_0_-2px_0_0_var(--color-accent)]"
        >
          <Inbox className="h-4 w-4" />
          {t("admin.federation.accept.tab", {
            defaultValue: "Aceptar invite",
          })}
        </Tabs.Trigger>
      </Tabs.List>
      <Tabs.Content value="invite" className="p-5 focus:outline-none">
        <InviteSection />
      </Tabs.Content>
      <Tabs.Content value="accept" className="p-5 focus:outline-none">
        <AcceptSection />
      </Tabs.Content>
    </Tabs.Root>
  );
}
