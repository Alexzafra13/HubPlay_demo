import { Link } from "react-router";
import { useTranslation } from "react-i18next";
import { useMyPeers } from "@/api/hooks/federation";
import { Spinner } from "@/components/common";

// PeersPage — top-level "Servidores conectados" route.
//
// Shows every paired federation peer. Empty state guides the admin
// toward /admin/federation. The user clicks a peer to drill into
// their shared libraries.
export default function PeersPage() {
  const { t } = useTranslation();
  const peers = useMyPeers();

  if (peers.isLoading) {
    return <Spinner />;
  }
  if (peers.error) {
    return (
      <div className="p-6">
        <p className="rounded border border-danger/40 bg-danger/5 p-3 text-sm text-danger">
          {String(peers.error)}
        </p>
      </div>
    );
  }

  const data = peers.data ?? [];

  if (data.length === 0) {
    return (
      <div className="p-6 sm:p-10">
        <h1 className="mb-2 text-2xl font-bold text-text-primary">
          {t("peers.title")}
        </h1>
        <p className="text-sm text-text-muted">
          {t("peers.emptyHint")}
        </p>
      </div>
    );
  }

  return (
    <div className="p-6 sm:p-10">
      <h1 className="mb-1 text-2xl font-bold text-text-primary">
        {t("peers.title")}
      </h1>
      <p className="mb-6 text-sm text-text-muted">
        {t("peers.subtitle")}
      </p>
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {data.map((peer) => (
          <Link
            key={peer.id}
            to={`/peers/${peer.id}`}
            className="block rounded-lg border border-border bg-bg-elevated p-5 transition-colors hover:border-accent"
          >
            <div className="flex items-center justify-between">
              <h2 className="text-lg font-semibold text-text-primary">
                {peer.name}
              </h2>
              <span className="rounded bg-accent/10 px-2 py-0.5 text-xs font-medium text-accent">
                {t("peers.statusPaired")}
              </span>
            </div>
            <p className="mt-2 break-all text-xs text-text-muted">
              {peer.base_url}
            </p>
            <p className="mt-2 font-mono text-xs text-text-muted">
              {peer.fingerprint}
            </p>
          </Link>
        ))}
      </div>
    </div>
  );
}
