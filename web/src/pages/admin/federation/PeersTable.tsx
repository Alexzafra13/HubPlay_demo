import { Fragment, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  useCreatePeerShare,
  useDeletePeerShare,
  usePeerShares,
  useRevokePeer,
} from "@/api/hooks/federation";
import { useLibraries } from "@/api/hooks/media";
import { Badge, Spinner } from "@/components/common";
import { Button } from "@/components/common/Button";
import type {
  FederationLibraryShare,
  FederationPeer,
  Library,
} from "@/api/types";
import { ErrorBanner } from "./_shared";

// PeersTable lists every paired (and revoked) peer with status,
// fingerprint, base URL, and per-peer actions: expand to manage
// shares, or revoke. Sub-pieces (SharesPanel, StatusBadge) live in
// the same file because they're tightly coupled to the row layout
// — splitting them further would just create cross-file plumbing
// for no readability win.

export function PeersTable({ peers }: { peers: FederationPeer[] }) {
  const { t } = useTranslation();
  const revoke = useRevokePeer();
  const [expanded, setExpanded] = useState<Set<string>>(new Set());

  if (peers.length === 0) {
    return (
      <p className="rounded-lg border border-dashed border-border bg-bg-elevated p-6 text-center text-sm text-text-muted">
        {t("admin.federation.peers.empty")}
      </p>
    );
  }

  const handleRevoke = (peer: FederationPeer) => {
    if (
      !window.confirm(
        t("admin.federation.peers.revokeConfirm", { name: peer.name }),
      )
    ) {
      return;
    }
    revoke.mutate(peer.id);
  };

  const toggle = (peerId: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(peerId)) {
        next.delete(peerId);
      } else {
        next.add(peerId);
      }
      return next;
    });
  };

  return (
    <div className="overflow-x-auto rounded-lg border border-border">
      <table className="w-full text-sm">
        <thead className="bg-bg-base">
          <tr className="text-left">
            <th className="px-4 py-2 font-semibold text-text-muted">
              {t("admin.federation.peers.col.name")}
            </th>
            <th className="px-4 py-2 font-semibold text-text-muted">
              {t("admin.federation.peers.col.status")}
            </th>
            <th className="px-4 py-2 font-semibold text-text-muted">
              {t("admin.federation.peers.col.fingerprint")}
            </th>
            <th className="px-4 py-2 font-semibold text-text-muted">
              {t("admin.federation.peers.col.url")}
            </th>
            <th className="px-4 py-2"></th>
          </tr>
        </thead>
        <tbody>
          {peers.map((peer) => {
            const isPaired = peer.status === "paired";
            const isExpanded = expanded.has(peer.id);
            return (
              <Fragment key={peer.id}>
                <tr className="border-t border-border">
                  <td className="px-4 py-3 font-medium text-text-primary">
                    {peer.name}
                  </td>
                  <td className="px-4 py-3">
                    <StatusBadge status={peer.status} />
                  </td>
                  <td className="px-4 py-3 font-mono text-xs text-text-muted">
                    {peer.fingerprint}
                  </td>
                  <td className="px-4 py-3 break-all text-xs text-text-muted">
                    {peer.base_url}
                  </td>
                  <td className="px-4 py-3 text-right">
                    <div className="flex flex-wrap justify-end gap-2">
                      {isPaired && (
                        <Button
                          variant="secondary"
                          size="sm"
                          onClick={() => toggle(peer.id)}
                        >
                          {isExpanded
                            ? t("admin.federation.peers.collapseShares")
                            : t("admin.federation.peers.manageShares")}
                        </Button>
                      )}
                      {peer.status !== "revoked" && (
                        <Button
                          variant="danger"
                          size="sm"
                          onClick={() => handleRevoke(peer)}
                          disabled={revoke.isPending}
                        >
                          {t("admin.federation.peers.revoke")}
                        </Button>
                      )}
                    </div>
                  </td>
                </tr>
                {isPaired && isExpanded && (
                  <tr className="border-t border-border bg-bg-base">
                    <td colSpan={5} className="px-4 py-4">
                      <SharesPanel peer={peer} />
                    </td>
                  </tr>
                )}
              </Fragment>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

// SharesPanel — per-peer expansion that lists every local library
// with toggles for "shared" + scope (play / download / livetv).
// browse defaults to true when shared. Auto-saves on each change
// (idempotent UPSERT on the backend).
function SharesPanel({ peer }: { peer: FederationPeer }) {
  const { t } = useTranslation();
  const libraries = useLibraries();
  const shares = usePeerShares(peer.id, true);
  const create = useCreatePeerShare(peer.id);
  const remove = useDeletePeerShare(peer.id);

  // Index of share rows by library_id for O(1) lookup.
  const sharesByLibrary = useMemo(() => {
    const m = new Map<string, FederationLibraryShare>();
    for (const s of shares.data ?? []) {
      m.set(s.library_id, s);
    }
    return m;
  }, [shares.data]);

  if (libraries.isLoading || shares.isLoading) {
    return <Spinner />;
  }
  if (libraries.error) {
    return <ErrorBanner message={String(libraries.error)} />;
  }
  if (shares.error) {
    return <ErrorBanner message={String(shares.error)} />;
  }

  const handleShareToggle = (lib: Library) => {
    const existing = sharesByLibrary.get(lib.id);
    if (existing) {
      // Currently shared → unshare.
      remove.mutate(existing.id);
    } else {
      // Not shared → create with default scopes.
      create.mutate({
        libraryID: lib.id,
        canBrowse: true,
        canPlay: true,
        canDownload: false,
        canLiveTV: false,
      });
    }
  };

  const handleScopeToggle = (
    share: FederationLibraryShare,
    scope: "can_play" | "can_download" | "can_livetv",
  ) => {
    create.mutate({
      libraryID: share.library_id,
      canBrowse: share.can_browse,
      canPlay: scope === "can_play" ? !share.can_play : share.can_play,
      canDownload: scope === "can_download" ? !share.can_download : share.can_download,
      canLiveTV: scope === "can_livetv" ? !share.can_livetv : share.can_livetv,
    });
  };

  const libs = libraries.data ?? [];
  if (libs.length === 0) {
    return (
      <p className="text-sm text-text-muted">
        {t("admin.federation.shares.noLibraries")}
      </p>
    );
  }

  return (
    <div className="flex flex-col gap-3">
      <h4 className="text-sm font-semibold text-text-primary">
        {t("admin.federation.shares.heading", { name: peer.name })}
      </h4>
      <p className="text-xs text-text-muted">
        {t("admin.federation.shares.hint")}
      </p>
      <div className="overflow-x-auto rounded border border-border bg-bg-elevated">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border text-left text-xs text-text-muted">
              <th className="px-3 py-2 font-semibold">
                {t("admin.federation.shares.col.library")}
              </th>
              <th className="px-3 py-2 font-semibold">
                {t("admin.federation.shares.col.shared")}
              </th>
              <th className="px-3 py-2 font-semibold">
                {t("admin.federation.shares.col.play")}
              </th>
              <th className="px-3 py-2 font-semibold">
                {t("admin.federation.shares.col.download")}
              </th>
              <th className="px-3 py-2 font-semibold">
                {t("admin.federation.shares.col.livetv")}
              </th>
            </tr>
          </thead>
          <tbody>
            {libs.map((lib) => {
              const share = sharesByLibrary.get(lib.id);
              const isShared = Boolean(share);
              return (
                <tr key={lib.id} className="border-t border-border">
                  <td className="px-3 py-2">
                    <div className="font-medium text-text-primary">{lib.name}</div>
                    <div className="text-xs text-text-muted">{lib.content_type}</div>
                  </td>
                  <td className="px-3 py-2">
                    <input
                      type="checkbox"
                      checked={isShared}
                      onChange={() => handleShareToggle(lib)}
                      disabled={create.isPending || remove.isPending}
                    />
                  </td>
                  <td className="px-3 py-2">
                    <input
                      type="checkbox"
                      checked={share?.can_play ?? false}
                      onChange={() => share && handleScopeToggle(share, "can_play")}
                      disabled={!isShared || create.isPending}
                    />
                  </td>
                  <td className="px-3 py-2">
                    <input
                      type="checkbox"
                      checked={share?.can_download ?? false}
                      onChange={() => share && handleScopeToggle(share, "can_download")}
                      disabled={!isShared || create.isPending}
                    />
                  </td>
                  <td className="px-3 py-2">
                    <input
                      type="checkbox"
                      checked={share?.can_livetv ?? false}
                      onChange={() => share && handleScopeToggle(share, "can_livetv")}
                      disabled={!isShared || create.isPending}
                    />
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      {(create.error || remove.error) && (
        <ErrorBanner message={String(create.error || remove.error)} />
      )}
    </div>
  );
}

function StatusBadge({ status }: { status: FederationPeer["status"] }) {
  const { t } = useTranslation();
  const variant: "success" | "warning" | "default" =
    status === "paired" ? "success" : status === "pending" ? "warning" : "default";
  return <Badge variant={variant}>{t(`admin.federation.peers.status.${status}`)}</Badge>;
}
