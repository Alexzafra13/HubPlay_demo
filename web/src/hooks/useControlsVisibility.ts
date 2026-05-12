import { useCallback, useRef, useState } from "react";

const CONTROLS_HIDE_DELAY = 3000;
const MOUSE_LEAVE_DELAY = 800;

interface UseControlsVisibilityReturn {
  controlsVisible: boolean;
  showControls: () => void;
  /** Immediate hide. Used by the mobile tap-toggle pattern so a tap on
   *  the video while controls are visible dismisses them without
   *  pausing playback. */
  hideControls: () => void;
  handleMouseMove: () => void;
  handleMouseLeave: () => void;
  keepControlsVisible: () => void;
}

export function useControlsVisibility(
  isPlaying: boolean,
): UseControlsVisibilityReturn {
  const [controlsVisible, setControlsVisible] = useState(true);
  const hideTimerRef = useRef<ReturnType<typeof setTimeout>>(0 as never);

  const showControls = useCallback(() => {
    setControlsVisible(true);
    clearTimeout(hideTimerRef.current);
    hideTimerRef.current = setTimeout(() => {
      if (isPlaying) {
        setControlsVisible(false);
      }
    }, CONTROLS_HIDE_DELAY);
  }, [isPlaying]);

  const handleMouseMove = useCallback(() => {
    showControls();
  }, [showControls]);

  const handleMouseLeave = useCallback(() => {
    if (isPlaying) {
      clearTimeout(hideTimerRef.current);
      hideTimerRef.current = setTimeout(() => {
        setControlsVisible(false);
      }, MOUSE_LEAVE_DELAY);
    }
  }, [isPlaying]);

  const keepControlsVisible = useCallback(() => {
    setControlsVisible(true);
    clearTimeout(hideTimerRef.current);
  }, []);

  const hideControls = useCallback(() => {
    clearTimeout(hideTimerRef.current);
    setControlsVisible(false);
  }, []);

  return {
    controlsVisible,
    showControls,
    hideControls,
    handleMouseMove,
    handleMouseLeave,
    keepControlsVisible,
  };
}
