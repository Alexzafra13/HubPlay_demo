import { Fragment, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  useAcceptInvite,
  useCreatePeerShare,
  useDeletePeerShare,
  useGenerateInvite,
  useListInvites,
  usePeers,
  usePeerShares,
  useProbePeer,
  useRevokePeer,
  useServerIdentity,
} from "@/api/hooks/federation";
import { useLibraries } from "@/api/hooks/media";
import { Badge, Spinner } from "@/components/common";
import { Button } from "@/components/common/Button";
import type {
  FederationLibraryShare,
  FederationPeer,
  FederationServerInfo,
  Library,
} from "@/api/types";

// FederationAdmin — admin tab for HubPlay-to-HubPlay peering.
//
// Three flows the admin needs:
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

// ─── My identity ───────────────────────────────────────────────────────

function IdentityCard({ info }: { info: FederationServerInfo }) {
  const { t } = useTranslation();
  return (
    <div className="rounded-lg border border-border bg-bg-elevated p-5">
      <div className="grid gap-4 sm:grid-cols-2">
        <div>
          <Label>{t("admin.federation.identity.name")}</Label>
          <Value>{info.name}</Value>
        </div>
        <div>
          <Label>{t("admin.federation.identity.serverUuid")}</Label>
          <Value mono>{info.server_uuid}</Value>
        </div>
        <div className="sm:col-span-2">
          <Label>{t("admin.federation.identity.fingerprint")}</Label>
          <div className="mt-1 flex items-center gap-2">
            <code className="rounded bg-bg-base px-3 py-2 text-base font-mono tracking-wider text-accent">
              {info.pubkey_fingerprint}
            </code>
            <CopyButton text={info.pubkey_fingerprint} />
          </div>
          <p className="mt-2 text-xs text-text-muted">
            {t("admin.federation.identity.fingerprintHint")}
          </p>
        </div>
        <div className="sm:col-span-2">
          <Label>{t("admin.federation.identity.words")}</Label>
          <div className="mt-1 flex flex-wrap gap-2">
            {info.pubkey_words.map((word) => (
              <span
                key={word}
                className="rounded bg-bg-base px-2 py-1 font-mono text-sm text-text-primary"
              >
                {word}
              </span>
            ))}
          </div>
          <p className="mt-2 text-xs text-text-muted">
            {t("admin.federation.identity.wordsHint")}
          </p>
        </div>
      </div>
    </div>
  );
}

// ─── Invite generation ─────────────────────────────────────────────────

function InviteSection() {
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

// ─── Accept (pair with another server) ─────────────────────────────────

function AcceptSection() {
  const { t } = useTranslation();
  const probe = useProbePeer();
  const accept = useAcceptInvite();

  const [baseURL, setBaseURL] = useState("");
  const [code, setCode] = useState("");
  const [confirmed, setConfirmed] = useState(false);

  const probedInfo = probe.data;

  const handleProbe = () => {
    if (!baseURL.trim()) return;
    setConfirmed(false);
    probe.mutate(baseURL.trim());
  };

  const handleAccept = () => {
    if (!baseURL.trim() || !code.trim() || !confirmed) return;
    accept.mutate(
      { baseURL: baseURL.trim(), code: code.trim() },
      {
        onSuccess: () => {
          // Clear the form on success — the peer is now in the table.
          setBaseURL("");
          setCode("");
          setConfirmed(false);
          probe.reset();
        },
      },
    );
  };

  return (
    <section>
      <h3 className="mb-3 text-sm font-semibold uppercase tracking-wide text-text-muted">
        {t("admin.federation.accept.heading")}
      </h3>
      <div className="rounded-lg border border-border bg-bg-elevated p-5">
        <p className="mb-4 text-sm text-text-muted">
          {t("admin.federation.accept.description")}
        </p>

        <div className="flex flex-col gap-3">
          <FieldInput
            label={t("admin.federation.accept.urlLabel")}
            placeholder="https://hubplay.tu-amigo.example.com"
            value={baseURL}
            onChange={setBaseURL}
          />

          <Button
            variant="secondary"
            onClick={handleProbe}
            disabled={probe.isPending || !baseURL.trim()}
          >
            {probe.isPending
              ? t("admin.federation.accept.probing")
              : t("admin.federation.accept.probe")}
          </Button>

          {probe.error && (
            <ErrorBanner message={String(probe.error)} />
          )}

          {probedInfo && (
            <div className="rounded border border-accent/40 bg-accent/5 p-4">
              <p className="text-sm font-semibold text-text-primary">
                {t("admin.federation.accept.foundServer", {
                  name: probedInfo.name,
                })}
              </p>
              <div className="mt-3 grid gap-3 sm:grid-cols-2">
                <div>
                  <Label>{t("admin.federation.identity.fingerprint")}</Label>
                  <Value mono>{probedInfo.pubkey_fingerprint}</Value>
                </div>
                <div>
                  <Label>{t("admin.federation.identity.serverUuid")}</Label>
                  <Value mono>{probedInfo.server_uuid}</Value>
                </div>
                <div className="sm:col-span-2">
                  <Label>{t("admin.federation.identity.words")}</Label>
                  <div className="mt-1 flex flex-wrap gap-2">
                    {probedInfo.pubkey_words.map((word) => (
                      <span
                        key={word}
                        className="rounded bg-bg-base px-2 py-1 font-mono text-sm text-text-primary"
                      >
                        {word}
                      </span>
                    ))}
                  </div>
                </div>
              </div>

              <div className="mt-4 flex flex-col gap-3">
                <FieldInput
                  label={t("admin.federation.accept.codeLabel")}
                  placeholder="hp-invite-XXXXXXXXXXXXXXXX"
                  value={code}
                  onChange={setCode}
                />

                <label className="flex cursor-pointer items-start gap-2 text-sm text-text-primary">
                  <input
                    type="checkbox"
                    className="mt-0.5"
                    checked={confirmed}
                    onChange={(e) => setConfirmed(e.target.checked)}
                  />
                  <span>{t("admin.federation.accept.confirm")}</span>
                </label>

                <Button
                  variant="primary"
                  onClick={handleAccept}
                  disabled={
                    !confirmed ||
                    !code.trim() ||
                    accept.isPending
                  }
                >
                  {accept.isPending
                    ? t("admin.federation.accept.pairing")
                    : t("admin.federation.accept.pair")}
                </Button>

                {accept.error && (
                  <ErrorBanner message={String(accept.error)} />
                )}
              </div>
            </div>
          )}
        </div>
      </div>
    </section>
  );
}

// ─── Peers table ──────────────────────────────────────────────────────

function PeersTable({ peers }: { peers: FederationPeer[] }) {
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

// ─── Small reusable bits ───────────────────────────────────────────────

function Label({ children }: { children: React.ReactNode }) {
  return <p className="text-xs uppercase tracking-wide text-text-muted">{children}</p>;
}

function Value({ children, mono = false }: { children: React.ReactNode; mono?: boolean }) {
  return (
    <p className={`mt-1 break-all text-sm text-text-primary ${mono ? "font-mono" : ""}`}>
      {children}
    </p>
  );
}

function FieldInput({
  label,
  placeholder,
  value,
  onChange,
}: {
  label: string;
  placeholder?: string;
  value: string;
  onChange: (v: string) => void;
}) {
  return (
    <label className="flex flex-col gap-1">
      <span className="text-xs uppercase tracking-wide text-text-muted">{label}</span>
      <input
        type="text"
        className="rounded border border-border bg-bg-base px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
        placeholder={placeholder}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
    </label>
  );
}

function CopyButton({ text }: { text: string }) {
  const { t } = useTranslation();
  const [copied, setCopied] = useState(false);

  const handleClick = async () => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // Some browsers gate clipboard on insecure context (HTTP). Fall
      // back to a no-op — the user can still read the field manually.
    }
  };

  return (
    <Button variant="secondary" size="sm" onClick={handleClick}>
      {copied ? t("admin.federation.copied") : t("admin.federation.copy")}
    </Button>
  );
}

function ErrorBanner({ message, className = "" }: { message: string; className?: string }) {
  return (
    <p className={`rounded border border-danger/40 bg-danger/5 p-3 text-sm text-danger ${className}`}>
      {message}
    </p>
  );
}
