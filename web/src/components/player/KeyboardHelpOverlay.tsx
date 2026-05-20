import { useTranslation } from "react-i18next";

// KeyboardHelpOverlay — floats over the player when the user hits `?`,
// listing every shortcut the player binds. Pure documentation surface
// — no side effects beyond rendering. Backdrop click + Escape (handled
// by the caller via the existing usePlayerKeyboard close path) dismiss.
//
// The list is kept in this file (rather than derived from the
// usePlayerKeyboard hook) because a hand-written grouping reads
// better than what the hook's switch statement can generate. We
// trade a tiny amount of duplication for legible copy.

interface KeyboardHelpOverlayProps {
  onClose: () => void;
}

interface ShortcutGroup {
  title: string;
  rows: { keys: string[]; label: string }[];
}

export function KeyboardHelpOverlay({ onClose }: KeyboardHelpOverlayProps) {
  const { t } = useTranslation();

  const groups: ShortcutGroup[] = [
    {
      title: t("player.shortcuts.playback", { defaultValue: "Reproducción" }),
      rows: [
        {
          keys: ["Espacio", "K"],
          label: t("player.shortcuts.togglePlay", {
            defaultValue: "Play / Pausa",
          }),
        },
        {
          keys: ["←", "J"],
          label: t("player.shortcuts.seekBack", {
            defaultValue: "Retroceder 10 s",
          }),
        },
        {
          keys: ["→", "L"],
          label: t("player.shortcuts.seekForward", {
            defaultValue: "Avanzar 10 s",
          }),
        },
        {
          keys: ["0", "…", "9"],
          label: t("player.shortcuts.seekPercent", {
            defaultValue: "Saltar al 0 % – 90 % de la duración",
          }),
        },
      ],
    },
    {
      title: t("player.shortcuts.audio", { defaultValue: "Audio" }),
      rows: [
        {
          keys: ["↑"],
          label: t("player.shortcuts.volumeUp", {
            defaultValue: "Subir volumen",
          }),
        },
        {
          keys: ["↓"],
          label: t("player.shortcuts.volumeDown", {
            defaultValue: "Bajar volumen",
          }),
        },
        {
          keys: ["M"],
          label: t("player.shortcuts.mute", {
            defaultValue: "Silenciar",
          }),
        },
      ],
    },
    {
      title: t("player.shortcuts.view", { defaultValue: "Vista" }),
      rows: [
        {
          keys: ["F"],
          label: t("player.shortcuts.fullscreen", {
            defaultValue: "Pantalla completa",
          }),
        },
        {
          keys: ["P"],
          label: t("player.shortcuts.pip", {
            defaultValue: "Picture-in-Picture",
          }),
        },
        {
          keys: ["?"],
          label: t("player.shortcuts.help", {
            defaultValue: "Mostrar / ocultar esta ayuda",
          }),
        },
        {
          keys: ["Esc"],
          label: t("player.shortcuts.exit", {
            defaultValue: "Salir de pantalla completa o cerrar el reproductor",
          }),
        },
      ],
    },
  ];

  return (
    <div
      role="dialog"
      aria-modal="true"
      className="absolute inset-0 z-50 flex items-center justify-center bg-black/70 backdrop-blur-sm"
      onClick={(e) => {
        e.stopPropagation();
        onClose();
      }}
      onKeyDown={(e) => {
        if (e.key === "Escape") {
          e.stopPropagation();
          onClose();
        }
      }}
    >
      <div
        role="presentation"
        className="max-h-[80vh] w-full max-w-lg overflow-y-auto rounded-xl border border-white/10 bg-bg-card/95 p-6 shadow-2xl"
        onClick={(e) => e.stopPropagation()}
        onKeyDown={(e) => e.stopPropagation()}
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold text-text-primary">
            {t("player.shortcuts.title", {
              defaultValue: "Atajos de teclado",
            })}
          </h2>
          <button
            type="button"
            onClick={onClose}
            className="rounded-md px-2 py-1 text-xs text-text-muted transition-colors hover:bg-bg-hover hover:text-text-primary"
            aria-label={t("common.close", { defaultValue: "Cerrar" })}
          >
            ✕
          </button>
        </div>

        <div className="flex flex-col gap-5">
          {groups.map((g) => (
            <div key={g.title}>
              <h3 className="mb-2 text-xs font-semibold uppercase tracking-wider text-text-muted">
                {g.title}
              </h3>
              <ul className="flex flex-col gap-1.5">
                {g.rows.map((row) => (
                  <li
                    key={row.label}
                    className="flex items-center justify-between gap-3 text-sm"
                  >
                    <span className="text-text-secondary">{row.label}</span>
                    <span className="flex flex-wrap items-center gap-1">
                      {row.keys.map((k, i) => (
                        // El label de la fila + la tecla + posición
                        // mantiene la key única aunque haya teclas
                        // repetidas en la misma combinación.
                        <span
                          key={`${row.label}-${k}-${i}`}
                          className={
                            k === "…"
                              ? "text-text-muted"
                              : "min-w-[24px] rounded-md border border-border bg-bg-elevated px-2 py-0.5 text-center font-mono text-xs text-text-primary shadow-sm"
                          }
                        >
                          {k}
                        </span>
                      ))}
                    </span>
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>

        <p className="mt-5 text-center text-[11px] text-text-muted">
          {t("player.shortcuts.dismissHint", {
            defaultValue: "Pulsa ? otra vez o haz clic fuera para cerrar.",
          })}
        </p>
      </div>
    </div>
  );
}
