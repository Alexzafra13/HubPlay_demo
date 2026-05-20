import { useState } from "react";
import { Check, Copy } from "lucide-react";

export interface CopyToClipboardButtonProps {
  /** Texto a copiar al portapapeles. */
  value: string;
  /** Label accesible — aria-label + title del botón. */
  label: string;
  /** Tamaño del icono en px. Default 12 (para usar dentro de filas KV
   *  finas). 14-16 cuadra mejor cuando el botón está suelto. */
  iconSize?: number;
  className?: string;
}

/**
 * CopyToClipboardButton — botón compacto con icono que copia un string
 * al portapapeles y muestra un check verde 2s tras el click. Pensado
 * para valores que el operador quiere compartir (URL del LAN mDNS,
 * tokens, fingerprints).
 *
 * Tolerante a entornos sin clipboard API (HTTP plano, browsers
 * antiguos): el click es no-op silencioso, sin error popup. El value
 * sigue siendo seleccionable con teclado / ratón para fallback manual.
 */
export function CopyToClipboardButton({
  value,
  label,
  iconSize = 12,
  className = "",
}: CopyToClipboardButtonProps) {
  const [copied, setCopied] = useState(false);

  async function onClick() {
    try {
      if (!navigator.clipboard) return;
      await navigator.clipboard.writeText(value);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 2000);
    } catch {
      // Permission denied / insecure context — el operador todavía
      // puede copiar manualmente seleccionando el texto.
    }
  }

  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      title={label}
      data-testid="copy-to-clipboard"
      className={[
        "inline-flex flex-none items-center justify-center rounded p-1",
        "text-text-muted hover:text-text-primary hover:bg-bg-hover transition-colors",
        className,
      ].join(" ")}
    >
      {copied ? (
        <Check size={iconSize} className="text-green-500" aria-hidden />
      ) : (
        <Copy size={iconSize} aria-hidden />
      )}
    </button>
  );
}
