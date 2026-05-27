import { useEffect } from "react";

/**
 * Sincroniza el estado externo de fullscreen con el del documento.
 * Cubre el caso en que el usuario sale del fullscreen por la
 * tecla ESC nativa (sin pasar por el botón de la UI) — sin este
 * listener el store mantenía `isFullscreen = true` y el icono del
 * toolbar quedaba desincronizado hasta la siguiente acción.
 */
export function useFullscreenSync(
  setFullscreen: (isFullscreen: boolean) => void,
): void {
  useEffect(() => {
    const onFullscreenChange = () => {
      setFullscreen(!!document.fullscreenElement);
    };

    document.addEventListener("fullscreenchange", onFullscreenChange);
    return () =>
      document.removeEventListener("fullscreenchange", onFullscreenChange);
  }, [setFullscreen]);
}
