import { useTranslation } from "react-i18next";
import { usePreflightM3U } from "@/api/hooks";
import type { PreflightResult, PreflightStatus } from "@/api/types";

interface Props {
  m3uURL: string;
  tlsInsecure: boolean;
  /** Disable the button while the URL is empty / clearly invalid. */
  disabled?: boolean;
}

/**
 * "Test connection" button for the library Add/Edit modals. Calls
 * /iptv/preflight and renders the verdict inline so the operator
 * gets a verdict in ~12 s instead of clicking Save and watching a
 * silent spinner for up to 5 min.
 *
 * The verdict is owned by the mutation state so a re-test (e.g.
 * after toggling tls_insecure) replaces it cleanly.
 */
export function PreflightButton({ m3uURL, tlsInsecure, disabled }: Props) {
  const { t } = useTranslation();
  const preflight = usePreflightM3U();

  const result = preflight.data;
  const tone = result ? toneForStatus(result.status) : "neutral";

  return (
    <div className="space-y-2">
      <button
        type="button"
        disabled={disabled || !m3uURL.trim() || preflight.isPending}
        onClick={() =>
          preflight.mutate({ m3u_url: m3uURL.trim(), tls_insecure: tlsInsecure })
        }
        className="px-3 py-1.5 text-xs rounded border border-border bg-bg-1 hover:border-text-muted disabled:opacity-50 disabled:cursor-not-allowed"
      >
        {preflight.isPending
          ? t("admin.libraries.preflightTesting", {
              defaultValue: "Probando conexión…",
            })
          : t("admin.libraries.preflightTest", {
              defaultValue: "Probar conexión",
            })}
      </button>

      {preflight.error && (
        <p className="text-xs text-error">
          {t("admin.libraries.preflightError", {
            defaultValue: "No se pudo lanzar la prueba",
          })}
          : {preflight.error.message}
        </p>
      )}

      {result && <PreflightVerdict result={result} tone={tone} />}
    </div>
  );
}

type Tone = "ok" | "warn" | "error" | "neutral";

function toneForStatus(s: PreflightStatus): Tone {
  switch (s) {
    case "ok":
      return "ok";
    case "slow":
    case "tls":
      return "warn";
    case "empty":
    case "html":
    case "auth":
    case "not_found":
    case "dns":
    case "connect":
    case "invalid_url":
    case "unknown":
      return "error";
    default:
      return "neutral";
  }
}

function PreflightVerdict({
  result,
  tone,
}: {
  result: PreflightResult;
  tone: Tone;
}) {
  const colour =
    tone === "ok"
      ? "border-success/40 bg-success/5 text-text"
      : tone === "warn"
        ? "border-warning/40 bg-warning/5 text-text"
        : "border-error/40 bg-error/5 text-text";
  const icon = tone === "ok" ? "✓" : tone === "warn" ? "⚠" : "✕";

  return (
    <div className={`rounded border p-2.5 text-xs ${colour}`}>
      <div className="flex items-start gap-2">
        <span className="font-bold">{icon}</span>
        <div className="flex-1 space-y-1">
          <p>{result.message}</p>
          <p className="text-[10px] text-text-muted">
            {result.http_status ? `HTTP ${result.http_status} · ` : ""}
            {result.content_length
              ? `${formatBytes(result.content_length)} · `
              : ""}
            {result.elapsed_ms} ms
          </p>
          {result.body_hint && (
            <pre className="overflow-x-auto whitespace-pre-wrap break-all text-[10px] text-text-muted bg-bg-1 rounded p-1.5">
              {result.body_hint}
            </pre>
          )}
        </div>
      </div>
    </div>
  );
}

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`;
  return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`;
}
