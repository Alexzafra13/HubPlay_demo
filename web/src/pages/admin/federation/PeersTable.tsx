import { Fragment, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  ChevronDown,
  ChevronRight,
  Globe,
  Inbox,
  ShieldOff,
} from "lucide-react";
import {
  useCreatePeerShare,
  useDeletePeerShare,
  usePeerShares,
  useRevokePeer,
} from "@/api/hooks/federation";
import { useLibraries } from "@/api/hooks/media";
import { Spinner } from "@/components/common";
import { Button } from "@/components/common/Button";
import type {
  FederationLibraryShare,
  FederationPeer,
  Library,
} from "@/api/types";
import { avatarColorFor } from "@/utils/avatarColor";
import { ErrorBanner } from "./_shared";

// PeersTable lists every paired (and revoked) peer with status,
// fingerprint, base URL, and per-peer actions: expand to manage
// shares, or revoke. SharesPanel + StatusDot live in the same file
// because they're tightly coupled to the row layout — splitting
// them further would just create cross-file plumbing for no
// readability win.

export function PeersTable({ peers }: { peers: FederationPeer[] }) {
  const { t } = useTranslation();
  const revoke = useRevokePeer();
  const [expanded, setExpanded] = useState<Set<string>>(new Set());

  if (peers.length === 0) {
    return <EmptyPeers />;
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
    <div className="overflow-hidden rounded-lg border border-border">
      <ul className="divide-y divide-border">
        {peers.map((peer) => {
          const isPaired = peer.status === "paired";
          const isExpanded = expanded.has(peer.id);
          const palette = avatarColorFor(peer.server_uuid || peer.name);
          const initials = initialsFor(peer.name);
          const lastSeen = peerLastSeen(peer);

          return (
            <Fragment key={peer.id}>
              <li className="bg-bg-card hover:bg-bg-elevated transition-colors">
                <div className="flex flex-wrap items-center gap-3 px-4 py-3">
                  {/* Expand chevron — only on paired peers, since
                      that's the only state with shares to manage. */}
                  <button
                    type="button"
                    onClick={() => isPaired && toggle(peer.id)}
                    disabled={!isPaired}
                    aria-expanded={isExpanded}
                    aria-label={
                      isExpanded
                        ? t("admin.federation.peers.collapseShares")
                        : t("admin.federation.peers.manageShares")
                    }
                    className="inline-flex h-6 w-6 flex-none items-center justify-center rounded text-text-muted hover:bg-bg-base hover:text-text-primary disabled:cursor-default disabled:opacity-30 disabled:hover:bg-transparent transition-colors"
                  >
                    {isExpanded ? (
                      <ChevronDown className="h-4 w-4" />
                    ) : (
                      <ChevronRight className="h-4 w-4" />
                    )}
                  </button>

                  {/* Avatar bubble. Initials over a deterministic
                      palette colour so two different peers never
                      look identical at a glance. */}
                  <div
                    className="flex h-9 w-9 flex-none items-center justify-center rounded-full text-sm font-semibold text-white"
                    style={{ backgroundColor: palette.background }}
                    aria-hidden
                  >
                    {initials}
                  </div>

                  {/* Name + URL stack. Name is the primary read
                      target; URL is metadata, smaller and muted. */}
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <p className="truncate font-medium text-text-primary">
                        {peer.name}
                      </p>
                      <StatusDot status={peer.status} lastSeen={lastSeen} />
                    </div>
                    <div className="mt-0.5 flex flex-wrap items-center gap-2 text-xs text-text-muted">
                      <span className="inline-flex items-center gap-1">
                        <Globe className="h-3 w-3" />
                        <span className="truncate">{peer.base_url}</span>
                      </span>
                      {lastSeen && (
                        <span className="inline-flex items-center gap-1 before:content-['·'] before:opacity-50">
                          <span className="ml-1">{lastSeen.label}</span>
                        </span>
                      )}
                    </div>
                    <p className="mt-0.5 truncate font-mono text-[10px] text-text-muted/80">
                      {peer.fingerprint}
                    </p>
                  </div>

                  <div className="flex flex-none flex-wrap justify-end gap-2">
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
                </div>
                {isPaired && isExpanded && (
                  <div className="border-t border-border bg-bg-base px-4 py-4">
                    <SharesPanel peer={peer} />
                  </div>
                )}
              </li>
            </Fragment>
          );
        })}
      </ul>
    </div>
  );
}

// EmptyPeers — friendly empty state. The original was a one-line
// muted paragraph that read like an error; this one explains the
// happy path and points the admin at the right CTA above.
function EmptyPeers() {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col items-center gap-3 rounded-lg border border-dashed border-border bg-bg-elevated px-6 py-12 text-center">
      <div className="rounded-full bg-bg-base p-3 text-text-muted">
        <Inbox className="h-6 w-6" />
      </div>
      <div>
        <p className="text-sm font-medium text-text-primary">
          {t("admin.federation.peers.emptyTitle", {
            defaultValue: "Aún no has emparejado con nadie",
          })}
        </p>
        <p className="mt-1 max-w-md text-xs text-text-muted">
          {t("admin.federation.peers.emptyHint", {
            defaultValue:
              "Genera un invite y compártelo con otro admin, o pega aquí el invite que te hayan enviado. El handshake es directo entre servidores y revocable en cualquier momento.",
          })}
        </p>
      </div>
    </div>
  );
}

// StatusDot — combines pairing status with last-seen heartbeat into
// a single severity dot + short label. We don't have a real "online
// now" probe so we infer: paired + last_seen within 5 min + status
// 200 → online; paired but stale → drowsy; revoked → off; pending →
// neutral. Trade-off between accuracy and admin clarity.
function StatusDot({
  status,
  lastSeen,
}: {
  status: FederationPeer["status"];
  lastSeen: ReturnType<typeof peerLastSeen>;
}) {
  const { t } = useTranslation();
  if (status === "revoked") {
    return (
      <span className="inline-flex items-center gap-1.5 rounded-full bg-error/10 px-2 py-0.5 text-[10px] font-medium text-error">
        <ShieldOff className="h-3 w-3" />
        {t("admin.federation.peers.status.revoked")}
      </span>
    );
  }
  if (status === "pending") {
    return (
      <span className="inline-flex items-center gap-1.5 rounded-full bg-warning/10 px-2 py-0.5 text-[10px] font-medium text-warning">
        <span className="h-1.5 w-1.5 rounded-full bg-warning" />
        {t("admin.federation.peers.status.pending")}
      </span>
    );
  }
  // Paired. Layer "online vs last seen" on top.
  const online = lastSeen?.online ?? false;
  return (
    <span
      className={[
        "inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[10px] font-medium",
        online
          ? "bg-success/10 text-success"
          : "bg-bg-base text-text-secondary border border-border-subtle",
      ].join(" ")}
    >
      <span
        className={[
          "h-1.5 w-1.5 rounded-full",
          online ? "bg-success" : "bg-text-muted",
        ].join(" ")}
      />
      {online
        ? t("admin.federation.peers.status.online", {
            defaultValue: "Online",
          })
        : t("admin.federation.peers.status.paired")}
    </span>
  );
}

// peerLastSeen — derives a short relative-time label and an "online"
// flag from last_seen_at + last_seen_status_code. Online means we
// pinged the peer within the last 5 minutes and got a 2xx. Anything
// older or non-200 falls back to a relative timestamp.
function peerLastSeen(peer: FederationPeer): {
  label: string;
  online: boolean;
} | null {
  if (!peer.last_seen_at) return null;
  const ts = new Date(peer.last_seen_at).getTime();
  if (Number.isNaN(ts)) return null;
  const ageMs = Date.now() - ts;
  const ageMin = Math.floor(ageMs / 60_000);
  const ok =
    peer.last_seen_status_code !== undefined &&
    peer.last_seen_status_code >= 200 &&
    peer.last_seen_status_code < 300;
  const online = ok && ageMin < 5;

  let label: string;
  if (ageMin < 1) label = "ahora";
  else if (ageMin < 60) label = `hace ${ageMin} min`;
  else if (ageMin < 60 * 24) label = `hace ${Math.floor(ageMin / 60)} h`;
  else label = `hace ${Math.floor(ageMin / (60 * 24))} d`;

  return { label, online };
}

// initialsFor — first letter of up to two leading words. "HubPlay
// Server" → "HS"; "alex" → "A". We avoid a server-side `name`
// breakdown because peer names are free-form.
function initialsFor(name: string): string {
  const parts = name.trim().split(/\s+/).slice(0, 2);
  return parts
    .map((p) => p.charAt(0).toUpperCase())
    .join("")
    .slice(0, 2);
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
      remove.mutate(existing.id);
    } else {
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
      canDownload:
        scope === "can_download" ? !share.can_download : share.can_download,
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
      <div>
        <h4 className="text-sm font-semibold text-text-primary">
          {t("admin.federation.shares.heading", { name: peer.name })}
        </h4>
        <p className="mt-1 text-xs text-text-muted">
          {t("admin.federation.shares.hint")}
        </p>
      </div>
      <div className="overflow-x-auto rounded-md border border-border bg-bg-elevated">
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
                    <div className="font-medium text-text-primary">
                      {lib.name}
                    </div>
                    <div className="text-xs text-text-muted">
                      {lib.content_type}
                    </div>
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
                      onChange={() =>
                        share && handleScopeToggle(share, "can_play")
                      }
                      disabled={!isShared || create.isPending}
                    />
                  </td>
                  <td className="px-3 py-2">
                    <input
                      type="checkbox"
                      checked={share?.can_download ?? false}
                      onChange={() =>
                        share && handleScopeToggle(share, "can_download")
                      }
                      disabled={!isShared || create.isPending}
                    />
                  </td>
                  <td className="px-3 py-2">
                    <input
                      type="checkbox"
                      checked={share?.can_livetv ?? false}
                      onChange={() =>
                        share && handleScopeToggle(share, "can_livetv")
                      }
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
