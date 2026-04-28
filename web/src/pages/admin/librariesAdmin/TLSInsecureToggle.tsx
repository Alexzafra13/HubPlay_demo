import { useTranslation } from "react-i18next";

interface Props {
  value: boolean;
  onChange: (next: boolean) => void;
}

/**
 * "Skip TLS verification" toggle for an IPTV library's M3U / EPG
 * fetch. Off by default. When ON, the surrounding card paints a
 * yellow warning so the operator can see at a glance that this
 * library is talking to an unverified server.
 *
 * Scope is intentionally narrow: only M3U / EPG fetches honour the
 * flag. The stream proxy keeps strict TLS verification regardless,
 * because clients trust HubPlay to deliver verified bytes — opting
 * the proxy out would weaken the chain in a way no operator should
 * be able to do via a checkbox.
 */
export function TLSInsecureToggle({ value, onChange }: Props) {
  const { t } = useTranslation();
  return (
    <div
      className={`rounded border p-2.5 transition-colors ${
        value ? "border-warning/40 bg-warning/5" : "border-border bg-bg-1"
      }`}
    >
      <label className="flex items-start gap-2 cursor-pointer text-xs">
        <input
          type="checkbox"
          checked={value}
          onChange={(e) => onChange(e.target.checked)}
          className="mt-0.5"
        />
        <span className="flex-1">
          <span className="font-medium text-text">
            {t("admin.libraries.tlsInsecureLabel", {
              defaultValue: "Saltar verificación TLS (cert. inválido)",
            })}
          </span>
          <span className="block mt-1 text-text-muted">
            {value
              ? t("admin.libraries.tlsInsecureWarn", {
                  defaultValue:
                    "⚠ Las descargas de M3U/EPG aceptarán cualquier certificado, incluso caducados o auto-firmados. Sólo activar para providers de confianza con TLS roto. El proxy de streams mantiene verificación estricta.",
                })
              : t("admin.libraries.tlsInsecureHint", {
                  defaultValue:
                    "Activar sólo si el provider tiene el certificado caducado o auto-firmado y aceptas el riesgo MITM.",
                })}
          </span>
        </span>
      </label>
    </div>
  );
}
