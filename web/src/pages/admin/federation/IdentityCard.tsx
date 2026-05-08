import { useTranslation } from "react-i18next";
import { Fingerprint, Volume2 } from "lucide-react";
import type { FederationServerInfo } from "@/api/types";
import { CopyButton, Label } from "./_shared";

// IdentityCard renders this server's federation identity. The
// fingerprint is the trust anchor for the whole protocol, so the
// visual hierarchy makes that explicit:
//
//   - server name + UUID sit at the top as compact metadata
//   - the fingerprint dominates the card: large mono, accent
//     colour, dedicated copy button right next to it
//   - the 4-word phonetic confirmation gets its own block with a
//     speaker icon to nudge the admin toward voice readout
//
// Everything here is non-secret by design (the private key never
// leaves the server), so we're optimising for "easy to read aloud
// over a phone" rather than "hard to see at a glance".

export function IdentityCard({ info }: { info: FederationServerInfo }) {
  const { t } = useTranslation();
  return (
    <div className="rounded-lg border border-border bg-bg-elevated p-5">
      {/* Top strip: name + UUID. Compact so the fingerprint below
          stays visually dominant. */}
      <div className="flex flex-wrap items-start justify-between gap-4 border-b border-border-subtle pb-4">
        <div className="min-w-0">
          <Label>{t("admin.federation.identity.name")}</Label>
          <p className="mt-1 truncate text-base font-semibold text-text-primary">
            {info.name}
          </p>
        </div>
        <div className="min-w-0 text-right">
          <Label>{t("admin.federation.identity.serverUuid")}</Label>
          <p className="mt-1 truncate font-mono text-xs text-text-muted">
            {info.server_uuid}
          </p>
        </div>
      </div>

      {/* Hero: fingerprint. Large mono, accent ring, copy button
          inline. The visual centre of the card. */}
      <div className="pt-5">
        <div className="flex items-center justify-between gap-2">
          <Label>
            <span className="inline-flex items-center gap-1.5">
              <Fingerprint className="h-3 w-3" />
              {t("admin.federation.identity.fingerprint")}
            </span>
          </Label>
          <CopyButton text={info.pubkey_fingerprint} />
        </div>
        <div className="mt-2 rounded-md border border-accent/30 bg-bg-base px-4 py-3">
          <code className="block break-all text-center font-mono text-xl tracking-[0.15em] text-accent">
            {info.pubkey_fingerprint}
          </code>
        </div>
        <p className="mt-2 text-xs leading-relaxed text-text-muted">
          {t("admin.federation.identity.fingerprintHint")}
        </p>
      </div>

      {/* Phonetic words. Bigger chips than before so they read at
          arm's length when the admin is on a phone call. */}
      <div className="mt-5 border-t border-border-subtle pt-5">
        <Label>
          <span className="inline-flex items-center gap-1.5">
            <Volume2 className="h-3 w-3" />
            {t("admin.federation.identity.words")}
          </span>
        </Label>
        <div className="mt-2 flex flex-wrap gap-2">
          {info.pubkey_words.map((word) => (
            <span
              key={word}
              className="rounded-md border border-border bg-bg-base px-3 py-1.5 font-mono text-sm font-semibold tracking-wide text-text-primary"
            >
              {word}
            </span>
          ))}
        </div>
        <p className="mt-2 text-xs leading-relaxed text-text-muted">
          {t("admin.federation.identity.wordsHint")}
        </p>
      </div>
    </div>
  );
}

