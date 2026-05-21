import type { FC } from "react";

interface Props {
  message: string;
  closeLabel: string;
  onClose: () => void;
}

/**
 * Overlay full-bleed con icono de error, mensaje y botón "Cerrar".
 * El `stopPropagation` del botón evita que el click llegue al
 * surface-tap del padre (que dispararía play/pause sobre un video
 * que en este punto probablemente ya está roto).
 */
export const ErrorOverlay: FC<Props> = ({ message, closeLabel, onClose }) => {
  return (
    <div className="absolute inset-0 flex items-center justify-center z-30 bg-black/80">
      <div className="flex flex-col items-center gap-4 max-w-md px-6 text-center">
        <svg
          className="size-12 text-error"
          viewBox="0 0 24 24"
          fill="currentColor"
        >
          <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm1 15h-2v-2h2v2zm0-4h-2V7h2v6z" />
        </svg>
        <p className="text-sm text-text-secondary">{message}</p>
        <button
          onClick={(e) => {
            e.stopPropagation();
            onClose();
          }}
          className="px-4 py-2 bg-white/10 hover:bg-white/20 rounded-[--radius-md] text-sm text-white transition-colors cursor-pointer"
        >
          {closeLabel}
        </button>
      </div>
    </div>
  );
};
