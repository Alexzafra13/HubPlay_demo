import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import QRCode from "qrcode";
import { useStartDeviceCode, usePollDeviceCode } from "@/api/hooks/deviceAuth";
import { Button } from "@/components/common";
import { BrandWordmark } from "@/components/layout/BrandWordmark";

// PairThisDevice — public /pair route. Renders on a TV / console
// browser when the operator doesn't want to type a password on a
// remote control. Flow:
//
//   1. Start a device-code flow via POST /auth/device/start.
//   2. Show the user_code (ABCD-EFGH, big) and a QR encoding
//      verification_uri_complete (the /link URL with the code
//      pre-filled).
//   3. Open an EventSource against /auth/device/events?device_code=…
//      — no polling. The server pushes "approved" the moment the
//      operator confirms on their phone.
//   4. On "approved" call /poll exactly once; the response sets
//      HTTP-only cookies so the next navigation is logged in.
//   5. Redirect into the app (Home / WhoIsWatching / change-password
//      are picked up by the existing post-login routing).
//
// On error or expiry the operator can request a fresh code. The
// EventSource is closed on unmount and on each new start to keep
// connection count predictable on TVs (older Tizen/webOS budgets).
export default function PairThisDevice() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const start = useStartDeviceCode();
  const poll = usePollDeviceCode();

  const deviceName = useMemo(() => detectDeviceName(), []);
  const [pair, setPair] = useState<{
    deviceCode: string;
    userCode: string;
    verificationURL: string;
    verificationURIComplete: string;
    expiresAt: number;
  } | null>(null);
  const [status, setStatus] = useState<
    "idle" | "waiting" | "approved" | "expired" | "error"
  >("idle");
  const [errorMsg, setErrorMsg] = useState<string | null>(null);
  const [qrSvg, setQrSvg] = useState<string | null>(null);
  const sourceRef = useRef<EventSource | null>(null);

  const beginFlow = useCallback(() => {
    setErrorMsg(null);
    setStatus("idle");
    start.mutate(deviceName, {
      onSuccess: (data) => {
        setPair({
          deviceCode: data.device_code,
          userCode: data.user_code,
          verificationURL: data.verification_url,
          verificationURIComplete: data.verification_uri_complete,
          expiresAt: Date.now() + data.expires_in * 1000,
        });
        setStatus("waiting");
      },
      onError: (err) => {
        setStatus("error");
        setErrorMsg(err.message);
      },
    });
  }, [start, deviceName]);

  // Kick off the flow once on mount. Tracked via useState so React 19's
  // ref-access-during-render rule stays satisfied; the guarded setState
  // pattern is the canonical replacement for an empty-deps useEffect
  // mount initialiser. StrictMode double-invokes still trip a single
  // beginFlow because the second invocation finds started === true.
  const [started, setStarted] = useState(false);
  if (!started) {
    setStarted(true);
    beginFlow();
  }

  // Reset the QR immediately when `pair` changes — render-time guarded
  // so the stale svg doesn't flash while the new one renders. The
  // async render of the new svg still lives inside the effect below.
  const [lastPair, setLastPair] = useState(pair);
  if (pair !== lastPair) {
    setLastPair(pair);
    setQrSvg(null);
  }

  // Render the QR whenever the pair URL changes. SVG mode keeps the
  // bundle light and avoids canvas issues on older smart-TV browsers.
  useEffect(() => {
    if (!pair) return;
    let cancelled = false;
    QRCode.toString(pair.verificationURIComplete, {
      type: "svg",
      margin: 1,
      errorCorrectionLevel: "M",
      color: { dark: "#0F172A", light: "#F8FAFC" },
    })
      .then((svg) => {
        if (!cancelled) setQrSvg(svg);
      })
      .catch(() => {
        if (!cancelled) setQrSvg(null);
      });
    return () => {
      cancelled = true;
    };
  }, [pair]);

  // Subscribe to the SSE stream. Closes on unmount / re-start. The
  // server emits a synthetic terminal event when the row is already
  // approved/consumed/expired at connect time, so reconnects after a
  // network blip still resolve cleanly.
  useEffect(() => {
    if (!pair) return;
    const url = `/api/v1/auth/device/events?device_code=${encodeURIComponent(
      pair.deviceCode,
    )}`;
    const src = new EventSource(url);
    sourceRef.current = src;

    src.addEventListener("approved", () => {
      setStatus("approved");
      // One poll consumes the code + sets the auth cookies. After
      // that the browser is logged in for every subsequent /api/v1.
      poll.mutate(pair.deviceCode, {
        onSuccess: () => {
          navigate("/", { replace: true });
        },
        onError: (err) => {
          setStatus("error");
          setErrorMsg(err.message);
        },
      });
      src.close();
    });
    src.addEventListener("expired", () => {
      setStatus("expired");
      src.close();
    });
    src.addEventListener("consumed", () => {
      setStatus("expired");
      src.close();
    });
    src.onerror = () => {
      // EventSource auto-reconnects on transient errors, so don't
      // flip to "error" here — only the explicit terminal events do.
    };
    return () => {
      src.close();
      sourceRef.current = null;
    };
    // poll/navigate son identidades estables (tanstack-query mutate +
    // react-router navigate); incluirlas en las deps no re-corre el
    // effect en la práctica y satisface el linter del compiler.
  }, [pair, poll, navigate]);

  return (
    <main className="mx-auto flex min-h-screen max-w-2xl flex-col items-center gap-6 p-6 sm:p-10">
      <BrandWordmark />
      <header className="text-center">
        <h1 className="text-2xl font-bold text-text-primary sm:text-3xl">
          {t("pair.title", { defaultValue: "Vincular este dispositivo" })}
        </h1>
        <p className="mt-2 text-sm text-text-muted">
          {t("pair.subtitle", {
            defaultValue:
              "Escanea el código QR con el móvil donde ya tienes sesión, o entra a /link en otro dispositivo e introduce este código.",
          })}
        </p>
      </header>

      {status === "error" ? (
        <div
          role="alert"
          className="w-full rounded-lg border border-red-500/40 bg-red-500/10 p-4 text-sm text-text-primary"
        >
          {errorMsg ??
            t("pair.errorGeneric", {
              defaultValue: "No pudimos preparar el vínculo. Inténtalo de nuevo.",
            })}
          <div className="mt-3">
            <Button onClick={beginFlow}>
              {t("pair.retry", { defaultValue: "Reintentar" })}
            </Button>
          </div>
        </div>
      ) : null}

      {status === "expired" ? (
        <div className="w-full rounded-lg border border-amber-500/40 bg-amber-500/10 p-4 text-sm text-text-primary">
          {t("pair.expired", {
            defaultValue:
              "El código caducó o se aprobó desde otro dispositivo. Genera uno nuevo si quieres seguir vinculando.",
          })}
          <div className="mt-3">
            <Button onClick={beginFlow}>
              {t("pair.regenerate", { defaultValue: "Generar nuevo código" })}
            </Button>
          </div>
        </div>
      ) : null}

      {pair && (status === "waiting" || status === "approved") ? (
        <section className="flex w-full flex-col items-center gap-6 rounded-2xl border border-border bg-bg-card p-6 sm:flex-row sm:items-stretch sm:gap-8">
          <div
            className="flex h-56 w-56 items-center justify-center rounded-xl bg-white p-3"
            aria-label={t("pair.qrAlt", {
              defaultValue: "Código QR para vincular este dispositivo",
            })}
          >
            {qrSvg ? (
              <div
                dangerouslySetInnerHTML={{ __html: qrSvg }}
                className="h-full w-full"
              />
            ) : (
              <div className="text-xs text-text-muted">
                {t("pair.qrLoading", { defaultValue: "Generando código…" })}
              </div>
            )}
          </div>
          <div className="flex flex-1 flex-col justify-center gap-3 text-center sm:text-left">
            <p className="text-xs font-medium uppercase tracking-wider text-text-muted">
              {t("pair.codeLabel", { defaultValue: "O escribe este código" })}
            </p>
            <p className="font-mono text-4xl font-bold tracking-[0.25em] text-text-primary">
              {pair.userCode}
            </p>
            <p className="text-xs text-text-muted">
              {t("pair.verificationHint", {
                defaultValue:
                  "Entra a {{url}} en otro dispositivo donde ya tengas sesión.",
                url: pair.verificationURL.replace(/^https?:\/\//, ""),
              })}
            </p>
            <p
              className="text-xs text-text-muted"
              aria-live="polite"
            >
              {status === "approved"
                ? t("pair.statusApproved", {
                    defaultValue: "Aprobado. Iniciando sesión…",
                  })
                : t("pair.statusWaiting", {
                    defaultValue: "Esperando aprobación…",
                  })}
            </p>
          </div>
        </section>
      ) : null}

      <footer className="text-xs text-text-muted">
        {t("pair.footer", {
          defaultValue:
            "El código expira a los 10 minutos. Cuando lo apruebes, este dispositivo iniciará sesión automáticamente.",
        })}
      </footer>
    </main>
  );
}

// detectDeviceName picks a friendly label for the session row the
// server creates on poll-success. Falls back to the platform string
// so a household with several TVs can still tell sessions apart in
// the Settings → Tus dispositivos panel.
function detectDeviceName(): string {
  if (typeof navigator === "undefined") return "Navegador";
  const ua = navigator.userAgent || "";
  if (/Tizen/i.test(ua)) return "Samsung TV";
  if (/Web0S|webOS|LG/i.test(ua)) return "LG TV";
  if (/AndroidTV|GoogleTV/i.test(ua)) return "Android TV";
  if (/PlayStation/i.test(ua)) return "PlayStation";
  if (/Xbox/i.test(ua)) return "Xbox";
  if (/iPad|Android/i.test(ua)) return "Tablet";
  if (/iPhone/i.test(ua)) return "iPhone";
  return "Navegador";
}
