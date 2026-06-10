import { useReducer, useCallback, useEffect } from "react";

interface OverlayState {
  upNextActive: boolean;
  showHelp: boolean;
}

type OverlayAction =
  | { type: "SHOW_UP_NEXT" }
  | { type: "CONFIRM_UP_NEXT" }
  | { type: "CANCEL_UP_NEXT" }
  | { type: "TOGGLE_HELP" }
  | { type: "CLOSE_HELP" }
  | { type: "RESET_ITEM" };

const initialState: OverlayState = {
  upNextActive: false,
  showHelp: false,
};

function overlayReducer(state: OverlayState, action: OverlayAction): OverlayState {
  switch (action.type) {
    case "SHOW_UP_NEXT":
      return { ...state, upNextActive: true, showHelp: false };
    case "CONFIRM_UP_NEXT":
    case "CANCEL_UP_NEXT":
      return { ...state, upNextActive: false };
    case "TOGGLE_HELP":
      return { ...state, showHelp: !state.showHelp };
    case "CLOSE_HELP":
      return { ...state, showHelp: false };
    case "RESET_ITEM":
      return initialState;
    default:
      return state;
  }
}

interface UsePlayerOverlaysOptions {
  itemId: string;
  hasNextUp: boolean;
  onEndedCallback?: () => void;
}

export function usePlayerOverlays({ itemId, hasNextUp, onEndedCallback }: UsePlayerOverlaysOptions) {
  const [state, dispatch] = useReducer(overlayReducer, initialState);

  useEffect(() => {
    dispatch({ type: "RESET_ITEM" });
  }, [itemId]);

  const handleEnded = useCallback(() => {
    if (hasNextUp && onEndedCallback) {
      dispatch({ type: "SHOW_UP_NEXT" });
    } else {
      onEndedCallback?.();
    }
  }, [hasNextUp, onEndedCallback]);

  const handleUpNextConfirm = useCallback(() => {
    dispatch({ type: "CONFIRM_UP_NEXT" });
    onEndedCallback?.();
  }, [onEndedCallback]);

  const handleUpNextCancel = useCallback(() => {
    dispatch({ type: "CANCEL_UP_NEXT" });
  }, []);

  const toggleHelp = useCallback(() => {
    dispatch({ type: "TOGGLE_HELP" });
  }, []);

  const closeHelp = useCallback(() => {
    dispatch({ type: "CLOSE_HELP" });
  }, []);

  return {
    ...state,
    handleEnded,
    handleUpNextConfirm,
    handleUpNextCancel,
    toggleHelp,
    closeHelp,
  };
}
