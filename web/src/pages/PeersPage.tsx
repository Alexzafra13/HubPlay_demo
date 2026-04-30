import { Link } from "react-router";
import { useTranslation } from "react-i18next";
import { useAllPeerLibraries, useMyPeers } from "@/api/hooks/federation";
import { Spinner } from "@/components/common";
import type { FederationUnifiedLibrary } from "@/api/types";

// PeersPage — unified landing for "Servidores conectados".
//
// Design principle: a library shared with you should feel like a
// first-class library, just with a small peer attribution badge.
// Users don't think "go into peer X then into library Y" — they
// think "browse my friends' movies". One flat grid per content
// type does that.
//
// Layout:
//   - top: small connected-peers strip (status indicator)
//   - middle: grouped grid (Movies, Series, Live TV) of library
//     cards from all peers
//   - empty state: clear pointer to admin federation panel
export default function PeersPage() {
  const { t } = useTranslation();
  const peers = useMyPeers();
  const libs = useAllPeerLibraries();

  if (peers.isLoading || libs.isLoading) {
    return (
      <div className="p-6 sm:p-10">
        <Spinner />
      </div>
    );
  }

  const peerList = peers.data ?? [];
  const libList = libs.data ?? [];

  if (peerList.length === 0) {
    return (
      <div className="p-6 sm:p-10">
        <h1 className="mb-2 text-2xl font-bold text-text-primary">
          {t("peers.title")}
        </h1>
        <div className="rounded-lg border border-dashed border-border bg-bg-elevated p-8 text-center">
          <p className="text-sm text-text-muted">{t("peers.emptyHint")}</p>
        </div>
      </div>
    );
  }

  // Group libraries by content_type for category-based browsing.
  // Order: movies → shows → livetv → other (preserve insertion).
  const groups = groupByContentType(libList);

  return (
    <div className="p-6 sm:p-10">
      <header className="mb-6">
        <h1 className="text-2xl font-bold text-text-primary sm:text-3xl">
          {t("peers.title")}
        </h1>
        <p className="mt-1 text-sm text-text-muted">{t("peers.subtitle")}</p>
      </header>

      <PeersStrip peers={peerList} />

      {libList.length === 0 ? (
        <div className="mt-8 rounded-lg border border-dashed border-border bg-bg-elevated p-8 text-center">
          <p className="text-sm text-text-muted">{t("peers.allEmpty")}</p>
        </div>
      ) : (
        <div className="mt-8 flex flex-col gap-10">
          {groups.map(([contentType, libs]) => (
            <section key={contentType}>
              <h2 className="mb-4 text-lg font-semibold text-text-primary">
                {t(`peers.contentType.${contentType}`, {
                  defaultValue: contentType,
                })}
                <span className="ml-2 text-sm font-normal text-text-muted">
                  ({libs.length})
                </span>
              </h2>
              <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
                {libs.map((lib) => (
                  <LibraryCard
                    key={`${lib.peer_id}-${lib.library_id}`}
                    lib={lib}
                  />
                ))}
              </div>
            </section>
          ))}
        </div>
      )}
    </div>
  );
}

// PeersStrip — compact "who's connected" row at the top of the page.
// Lets the user see their network at a glance without expanding into
// per-peer navigation.
function PeersStrip({
  peers,
}: {
  peers: { id: string; name: string; fingerprint: string }[];
}) {
  const { t } = useTranslation();
  return (
    <div className="rounded-lg border border-border bg-bg-elevated p-4">
      <p className="mb-3 text-xs uppercase tracking-wide text-text-muted">
        {t("peers.peersStripHeading", { count: peers.length })}
      </p>
      <div className="flex flex-wrap gap-2">
        {peers.map((peer) => (
          <Link
            key={peer.id}
            to={`/peers/${peer.id}`}
            className="group flex items-center gap-2 rounded-full bg-bg-base px-3 py-1.5 text-sm text-text-primary transition-colors hover:bg-accent/10 hover:text-accent"
            title={peer.fingerprint}
          >
            <span
              className="h-2 w-2 rounded-full bg-emerald-500"
              aria-hidden
            />
            <span className="font-medium">{peer.name}</span>
          </Link>
        ))}
      </div>
    </div>
  );
}

// LibraryCard — the unit of the unified grid. Shows library name +
// peer attribution + content-type chip + scope badges. Click → drill
// into the catalog for this (peer, library) pair.
function LibraryCard({ lib }: { lib: FederationUnifiedLibrary }) {
  const { t } = useTranslation();
  return (
    <Link
      to={`/peers/${lib.peer_id}/libraries/${lib.library_id}`}
      className="group flex flex-col gap-3 rounded-lg border border-border bg-bg-elevated p-5 transition-colors hover:border-accent"
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <h3 className="truncate text-base font-semibold text-text-primary group-hover:text-accent">
            {lib.library_name}
          </h3>
          <p className="mt-0.5 text-xs text-text-muted">
            {t("peers.sharedBy", { name: lib.peer_name })}
          </p>
        </div>
        <span className="shrink-0 rounded bg-bg-base px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-text-muted">
          {lib.content_type}
        </span>
      </div>
      <div className="flex flex-wrap gap-1">
        {lib.can_play && (
          <ScopeChip label={t("peers.scope.play")} />
        )}
        {lib.can_download && (
          <ScopeChip label={t("peers.scope.download")} />
        )}
        {lib.can_livetv && (
          <ScopeChip label={t("peers.scope.livetv")} />
        )}
      </div>
    </Link>
  );
}

function ScopeChip({ label }: { label: string }) {
  return (
    <span className="rounded bg-accent/10 px-2 py-0.5 text-[10px] font-medium text-accent">
      {label}
    </span>
  );
}

// groupByContentType bins libraries by content_type and returns them
// in a deterministic order: movies → shows → livetv → anything else
// (in insertion order). Stable sort keys keep cards from jumping
// around between renders even when react-query refetches.
function groupByContentType(
  libs: FederationUnifiedLibrary[],
): [string, FederationUnifiedLibrary[]][] {
  const order = ["movies", "shows", "livetv"];
  const buckets = new Map<string, FederationUnifiedLibrary[]>();
  for (const lib of libs) {
    const key = lib.content_type || "other";
    if (!buckets.has(key)) {
      buckets.set(key, []);
    }
    buckets.get(key)!.push(lib);
  }
  const ordered: [string, FederationUnifiedLibrary[]][] = [];
  for (const k of order) {
    if (buckets.has(k)) {
      ordered.push([k, buckets.get(k)!]);
      buckets.delete(k);
    }
  }
  for (const [k, v] of buckets) {
    ordered.push([k, v]);
  }
  return ordered;
}
