import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useAcceptInvite, useProbePeer } from "@/api/hooks/federation";
import { Button } from "@/components/common/Button";
import { ErrorBanner, FieldInput, Label, Value } from "./_shared";

// AcceptSection completes the inbound side of the handshake. The
// admin pastes the remote URL + the invite code they received, then
// hits Probe to fetch the remote's identity (name + fingerprint +
// 4-word phonetic). The "Confirma" checkbox guards the Pair button
// so a misclick can't pair before the admin verified the fingerprint
// out-of-band with the inviting admin (voice / text / paper).

export function AcceptSection() {
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
