import { useState } from "react";
import { useTranslation } from "react-i18next";
import { CheckCircle2, Fingerprint, Search, Volume2 } from "lucide-react";
import { useAcceptInvite, useProbePeer } from "@/api/hooks/federation";
import { Button } from "@/components/common/Button";
import { ErrorBanner, FieldInput, Label, Value } from "./_shared";

// AcceptSection — inbound side of the handshake. Renders bare (no
// <section>/h3) because the parent Tab already states the mode.
//
// The flow is staged: paste URL → Probe → display remote identity
// for verification → paste code → confirm → Pair. We render every
// stage as a visually distinct block (background tint shifts when
// the remote is verified) so the admin always knows where they are.

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
    <div className="flex flex-col gap-4">
      <p className="text-sm leading-relaxed text-text-muted">
        {t("admin.federation.accept.description")}
      </p>

      {/* Stage 1 — URL + Probe. Always visible. */}
      <div className="flex flex-col gap-3">
        <FieldInput
          label={t("admin.federation.accept.urlLabel")}
          placeholder="https://hubplay.tu-amigo.example.com"
          value={baseURL}
          onChange={setBaseURL}
        />

        <Button
          variant={probedInfo ? "secondary" : "primary"}
          onClick={handleProbe}
          disabled={probe.isPending || !baseURL.trim()}
        >
          <Search className="-ml-1 mr-1.5 h-4 w-4" />
          {probe.isPending
            ? t("admin.federation.accept.probing")
            : t("admin.federation.accept.probe")}
        </Button>

        {probe.error && <ErrorBanner message={String(probe.error)} />}
      </div>

      {/* Stage 2 — verification. Only after a successful probe.
          The accent border + tinted bg makes the "this is the
          remote you're about to trust" moment unmissable. */}
      {probedInfo && (
        <div className="rounded-md border border-accent/40 bg-accent/5 p-4">
          <p className="flex items-center gap-2 text-sm font-semibold text-text-primary">
            <CheckCircle2 className="h-4 w-4 text-accent" />
            {t("admin.federation.accept.foundServer", {
              name: probedInfo.name,
            })}
          </p>

          <div className="mt-4 flex flex-col gap-4">
            <div>
              <Label>
                <span className="inline-flex items-center gap-1.5">
                  <Fingerprint className="h-3 w-3" />
                  {t("admin.federation.identity.fingerprint")}
                </span>
              </Label>
              <div className="mt-1 rounded-md border border-border bg-bg-base px-3 py-2">
                <code className="block break-all text-center font-mono text-base tracking-[0.15em] text-accent">
                  {probedInfo.pubkey_fingerprint}
                </code>
              </div>
            </div>

            <div>
              <Label>
                <span className="inline-flex items-center gap-1.5">
                  <Volume2 className="h-3 w-3" />
                  {t("admin.federation.identity.words")}
                </span>
              </Label>
              <div className="mt-1 flex flex-wrap gap-2">
                {probedInfo.pubkey_words.map((word) => (
                  <span
                    key={word}
                    className="rounded-md border border-border bg-bg-base px-3 py-1.5 font-mono text-sm font-semibold text-text-primary"
                  >
                    {word}
                  </span>
                ))}
              </div>
            </div>

            <div>
              <Label>{t("admin.federation.identity.serverUuid")}</Label>
              <Value mono>{probedInfo.server_uuid}</Value>
            </div>
          </div>

          {/* Stage 3 — code + confirm + pair. Inside the verification
              card so the admin doesn't visually disconnect "the thing
              I just saw" from "the action I'm about to take". */}
          <div className="mt-5 border-t border-accent/20 pt-4">
            <div className="flex flex-col gap-3">
              <FieldInput
                label={t("admin.federation.accept.codeLabel")}
                placeholder="hp-invite-XXXXXXXXXXXXXXXX"
                value={code}
                onChange={setCode}
              />

              <label className="flex cursor-pointer items-start gap-2 rounded-md border border-border bg-bg-base px-3 py-2.5 text-sm text-text-primary">
                <input
                  type="checkbox"
                  className="mt-0.5"
                  checked={confirmed}
                  onChange={(e) => setConfirmed(e.target.checked)}
                />
                <span className="leading-relaxed">
                  {t("admin.federation.accept.confirm")}
                </span>
              </label>

              <Button
                variant="primary"
                onClick={handleAccept}
                disabled={!confirmed || !code.trim() || accept.isPending}
              >
                {accept.isPending
                  ? t("admin.federation.accept.pairing")
                  : t("admin.federation.accept.pair")}
              </Button>

              {accept.error && <ErrorBanner message={String(accept.error)} />}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
