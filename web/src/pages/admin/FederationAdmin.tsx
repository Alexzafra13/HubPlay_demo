import { useTranslation } from "react-i18next";
import { usePeers, useServerIdentity } from "@/api/hooks/federation";
import { Spinner } from "@/components/common";
import { AcceptSection } from "./federation/AcceptSection";
import { ErrorBanner } from "./federation/_shared";
import { IdentityCard } from "./federation/IdentityCard";
import { InviteSection } from "./federation/InviteSection";
import { PeersTable } from "./federation/PeersTable";

// FederationAdmin — admin tab for HubPlay-to-HubPlay peering. Pure
// orchestrator: each of the four logical surfaces (identity / invite /
// accept / peers) lives in its own file under `federation/` so adding
// or extending one stays a single-file edit. Shared primitives
// (Label, Value, FieldInput, CopyButton, ErrorBanner) live in
// `federation/_shared.tsx`.
//
// The three flows the admin needs:
//
//  1. INVITE: generate a code locally and share it with a remote admin.
//     The remote admin pastes it into THEIR UI (flow 2), and the
//     handshake completes server-to-server.
//
//  2. ACCEPT: paste a code received from another admin, plus their
//     server's URL. The UI probes the URL, displays the remote's
//     fingerprint, the admin compares it out-of-band with the inviting
//     admin, then clicks Accept to finalise the handshake.
//
//  3. MANAGE: list paired peers and revoke them.
//
// Fingerprint comparison is the trust anchor for federation. The UI
// renders both the hex form (for paste/copy) and 4 short words (for
// voice readout). The "Confirma" checkbox guards the Accept button so
// a misclick can't pair a server before the admin verified.

export default function FederationAdmin() {
  const { t } = useTranslation();
  const identity = useServerIdentity();
  const peers = usePeers();

  return (
    <div className="flex flex-col gap-8">
      <header>
        <h2 className="text-xl font-semibold text-text-primary">
          {t("admin.federation.title")}
        </h2>
        <p className="mt-1 text-sm text-text-muted">
          {t("admin.federation.subtitle")}
        </p>
      </header>

      <section>
        <h3 className="mb-3 text-sm font-semibold uppercase tracking-wide text-text-muted">
          {t("admin.federation.identity.heading")}
        </h3>
        {identity.isLoading ? (
          <Spinner />
        ) : identity.error ? (
          <ErrorBanner message={String(identity.error)} />
        ) : identity.data ? (
          <IdentityCard info={identity.data} />
        ) : null}
      </section>

      <InviteSection />

      <AcceptSection />

      <section>
        <h3 className="mb-3 text-sm font-semibold uppercase tracking-wide text-text-muted">
          {t("admin.federation.peers.heading")}
        </h3>
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
