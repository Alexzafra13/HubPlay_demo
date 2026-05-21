import type { FC } from "react";

interface Props {
  /** Cuando es `true`, el overlay se desvanece (opacity 0). */
  firstFrameReady: boolean;
  backdropUrl?: string;
  logoUrl?: string;
  title?: string;
}

/**
 * Cubre el `<video>` (todavía negro) con el arte del item hasta que
 * el primer frame pinta — cierra el "gap negro" de 2-5 s mientras
 * ffmpeg produce el primer segmento. Mismo patrón que Plex/Jellyfin.
 *
 * Layout:
 * - Barra superior 0.5 px con highlight deslizante (telegrafía
 *   "preparando" sin spinner Win95).
 * - Backdrop full-bleed escalado a `cover`. Fallback a negro sólido si
 *   no hay URL (items federados / scan-in-progress).
 * - Gradiente oscuro (vignette) para que el logo/título lean limpios
 *   sobre artworks brillantes (Avengers, Doctor Strange).
 * - Logo PNG si hay, si no `<h1>` con el título. Esquina inferior-
 *   izquierda; mismo treatment que la página de detail de la que
 *   viene el usuario.
 *
 * `pointer-events-none` cuando ya está fundido a fuera para no
 * interceptar clicks sobre los controles o el video.
 */
export const BackdropLoadingOverlay: FC<Props> = ({
  firstFrameReady,
  backdropUrl,
  logoUrl,
  title,
}) => {
  return (
    <div
      className={[
        "absolute inset-0 transition-opacity duration-500 ease-out",
        firstFrameReady ? "opacity-0 pointer-events-none" : "opacity-100",
      ].join(" ")}
      aria-hidden={firstFrameReady}
    >
      <div className="absolute top-0 left-0 right-0 h-0.5 bg-white/10 overflow-hidden">
        <div
          className="h-full w-1/4 bg-white/70"
          style={{ animation: "loading-slide 900ms ease-in-out infinite" }}
        />
      </div>

      <div
        className="absolute inset-0 bg-black"
        style={
          backdropUrl
            ? {
                backgroundImage: `url(${backdropUrl})`,
                backgroundSize: "cover",
                backgroundPosition: "center",
              }
            : undefined
        }
      />
      <div className="absolute inset-0 bg-gradient-to-t from-black via-black/40 to-black/30" />

      <div className="absolute left-6 right-6 bottom-12 sm:left-12 sm:bottom-20 max-w-[60%]">
        {logoUrl ? (
          <img
            src={logoUrl}
            alt={title ?? ""}
            className="max-h-24 sm:max-h-32 w-auto object-contain drop-shadow-[0_4px_12px_rgba(0,0,0,0.7)]"
          />
        ) : title ? (
          <h1 className="text-3xl sm:text-5xl font-semibold text-white drop-shadow-[0_4px_12px_rgba(0,0,0,0.8)]">
            {title}
          </h1>
        ) : null}
      </div>
    </div>
  );
};
