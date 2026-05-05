import { useTranslation } from "react-i18next";
import type { FederationServerInfo } from "@/api/types";
import { CopyButton, Label, Value } from "./_shared";

// IdentityCard renders this server's federation identity panel:
// human name, server UUID, and the public-key fingerprint in both
// hex form (for copy/paste) and 4-word phonetic form (for voice
// readout when comparing fingerprints out-of-band before pairing).

export function IdentityCard({ info }: { info: FederationServerInfo }) {
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
