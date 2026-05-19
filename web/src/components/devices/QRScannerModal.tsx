import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Scanner, type IDetectedBarcode } from "@yudiel/react-qr-scanner";
import { X } from "lucide-react";

import { extractCode } from "./extractCode";

// QRScannerModal abre la cámara del móvil para escanear un QR
// generado por la TV en /pair. Cuando decodifica, intenta extraer
// el ?code=ABCD-EFGH de la URL y se lo pasa al caller; si el QR
// no es una URL válida o no trae code, muestra error y deja
// reintentar sin cerrar.
//
// Cargado con React.lazy desde LinkDevice para que el bundle base
// de /link no incluya la librería del scanner (~38 KB gzipped).
//
// Permisos: getUserMedia exige HTTPS en producción (Safari iOS
// rechaza HTTP excepto localhost). En desarrollo http://localhost
// también funciona.

interface Props {
  onCode: (code: string) => void;
  onClose: () => void;
}

export default function QRScannerModal({ onCode, onClose }: Props) {
  const { t } = useTranslation();
  const [error, setError] = useState<string | null>(null);
  const [paused, setPaused] = useState(false);

  // Bloquea el scroll del body mientras el modal está abierto —
  // típico de modales fullscreen en móvil, evita pull-to-refresh
  // accidental mientras se apunta a la TV.
  useEffect(() => {
    const prev = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.body.style.overflow = prev;
    };
  }, []);

  // ESC cierra (teclado del móvil casi nunca, pero útil en
  // navegadores desktop donde alguien quiere probar con la webcam).
  useEffect(() => {
    function handleKey(e: KeyboardEvent) {
      if (e.key === "Escape") onClose();
    }
    window.addEventListener("keydown", handleKey);
    return () => window.removeEventListener("keydown", handleKey);
  }, [onClose]);

  function handleScan(results: IDetectedBarcode[]) {
    if (paused) return;
    const raw = results[0]?.rawValue;
    if (!raw) return;
    const code = extractCode(raw);
    if (!code) {
      setError(
        t("link.scanner.invalidQR", {
          defaultValue:
            "El QR no contiene un código válido. Asegúrate de escanear el QR que muestra la TV.",
        }),
      );
      return;
    }
    // Pausa para no disparar múltiples veces mientras el caller
    // procesa el código (re-render de LinkDevice cierra el modal,
    // pero entre frames el scanner podría disparar otra vez).
    setPaused(true);
    setError(null);
    onCode(code);
  }

  function handleError(err: unknown) {
    // Errores típicos de getUserMedia: permiso denegado, sin cámara,
    // HTTP en producción (Safari). Damos un mensaje genérico — el
    // log del navegador tiene el detalle si hace falta debug.
    const name =
      err && typeof err === "object" && "name" in err
        ? String((err as { name: unknown }).name)
        : "";
    if (name === "NotAllowedError" || name === "PermissionDeniedError") {
      setError(
        t("link.scanner.permissionDenied", {
          defaultValue:
            "Necesitamos permiso para la cámara. Acéptalo en el navegador y vuelve a intentarlo.",
        }),
      );
      return;
    }
    if (name === "NotFoundError" || name === "DevicesNotFoundError") {
      setError(
        t("link.scanner.noCamera", {
          defaultValue: "No se ha encontrado ninguna cámara en este dispositivo.",
        }),
      );
      return;
    }
    setError(
      t("link.scanner.genericError", {
        defaultValue:
          "No se pudo iniciar la cámara. Asegúrate de estar en HTTPS y vuelve a intentarlo.",
      }),
    );
  }

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label={t("link.scanner.title", { defaultValue: "Escanear código QR" })}
      className="fixed inset-0 z-50 flex flex-col bg-bg-base"
    >
      <header className="flex items-center justify-between border-b border-border-subtle bg-bg-elevated px-4 py-3">
        <h2 className="text-base font-semibold text-text-primary">
          {t("link.scanner.title", { defaultValue: "Escanear código QR" })}
        </h2>
        <button
          type="button"
          onClick={onClose}
          aria-label={t("common.close", { defaultValue: "Cerrar" })}
          className="rounded-full p-2 text-text-muted transition-colors hover:bg-bg-base hover:text-text-primary"
        >
          <X className="h-5 w-5" />
        </button>
      </header>

      {/* Visor de cámara. flex-1 ocupa todo el alto disponible;
          la propia librería ajusta el video al contenedor. */}
      <div className="relative flex-1 overflow-hidden bg-black">
        <Scanner
          onScan={handleScan}
          onError={handleError}
          paused={paused}
          constraints={{ facingMode: "environment" }}
          styles={{
            container: { width: "100%", height: "100%" },
            video: { width: "100%", height: "100%", objectFit: "cover" },
          }}
        />
        {/* Overlay con hint y guía visual: marco central para
            que el usuario sepa dónde encuadrar el QR. */}
        <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center gap-4 p-6">
          <div className="h-56 w-56 rounded-2xl border-2 border-white/70 shadow-[0_0_0_9999px_rgba(0,0,0,0.45)]" />
          <p className="max-w-xs rounded-full bg-black/60 px-4 py-2 text-center text-sm text-white">
            {t("link.scanner.hint", {
              defaultValue: "Apunta a la pantalla de tu TV con el código QR.",
            })}
          </p>
        </div>
      </div>

      {error && (
        <div
          role="alert"
          className="border-t border-red-500/40 bg-red-500/10 p-4 text-sm text-text-primary"
        >
          {error}
          <button
            type="button"
            onClick={() => {
              setError(null);
              setPaused(false);
            }}
            className="ml-3 underline underline-offset-2 hover:no-underline"
          >
            {t("link.scanner.retry", { defaultValue: "Reintentar" })}
          </button>
        </div>
      )}
    </div>
  );
}

// extractCode + isLikelyCode viven en ./extractCode.ts — mantener
// utilidades fuera de este módulo es lo que pide la regla
// react-refresh/only-export-components (HMR sólo funciona bien
// cuando un archivo exporta sólo componentes).
