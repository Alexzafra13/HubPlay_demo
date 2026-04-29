import { Link, useParams } from "react-router";
import { useTranslation } from "react-i18next";
import { useMyPeers, usePeerLibraries } from "@/api/hooks/federation";
import { Spinner } from "@/components/common";

// PeerLibrariesPage — /peers/:peerId
//
// Shows the libraries a specific peer has shared with us. Each card
// links into /peers/:peerId/libraries/:libId where the catalog
// browse lives.
export default function PeerLibrariesPage() {
  const { t } = useTranslation();
  const { peerId = "" } = useParams();
  const peers = useMyPeers();
  const libraries = usePeerLibraries(peerId);

  const peer = peers.data?.find((p) => p.id === peerId);

  if (libraries.isLoading || peers.isLoading) {
    return <Spinner />;
  }
  if (libraries.error) {
    return (
      <div className="p-6">
        <p className="rounded border border-danger/40 bg-danger/5 p-3 text-sm text-danger">
          {t("peers.unreachable", { error: String(libraries.error) })}
        </p>
        <Link to="/peers" className="mt-4 inline-block text-sm text-accent hover:underline">
          ← {t("peers.backToList")}
        </Link>
      </div>
    );
  }

  const libs = libraries.data ?? [];

  return (
    <div className="p-6 sm:p-10">
      <Link to="/peers" className="text-sm text-accent hover:underline">
        ← {t("peers.backToList")}
      </Link>
      <h1 className="mt-2 text-2xl font-bold text-text-primary">
        {peer?.name ?? t("peers.unknownPeer")}
      </h1>
      <p className="mb-6 text-sm text-text-muted">
        {t("peers.librariesSubtitle")}
      </p>

      {libs.length === 0 ? (
        <p className="rounded-lg border border-dashed border-border bg-bg-elevated p-6 text-center text-sm text-text-muted">
          {t("peers.noShares")}
        </p>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {libs.map((lib) => (
            <Link
              key={lib.id}
              to={`/peers/${peerId}/libraries/${lib.id}`}
              className="block rounded-lg border border-border bg-bg-elevated p-5 transition-colors hover:border-accent"
            >
              <h2 className="text-lg font-semibold text-text-primary">
                {lib.name}
              </h2>
              <p className="mt-1 text-xs uppercase tracking-wide text-text-muted">
                {lib.content_type}
              </p>
              <div className="mt-3 flex flex-wrap gap-1">
                {lib.scopes.can_play && (
                  <span className="rounded bg-bg-base px-2 py-0.5 text-xs text-text-muted">
                    {t("peers.scope.play")}
                  </span>
                )}
                {lib.scopes.can_download && (
                  <span className="rounded bg-bg-base px-2 py-0.5 text-xs text-text-muted">
                    {t("peers.scope.download")}
                  </span>
                )}
                {lib.scopes.can_livetv && (
                  <span className="rounded bg-bg-base px-2 py-0.5 text-xs text-text-muted">
                    {t("peers.scope.livetv")}
                  </span>
                )}
              </div>
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}
