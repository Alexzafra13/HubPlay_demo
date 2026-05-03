// useModalStack — single source of truth for which dialogs are
// currently mounted on top of the page.
//
// Why this exists:
//   The ad-hoc per-modal pattern (each <Modal> writing to
//   `document.body.style.overflow` from its own useEffect) breaks down
//   the moment a second modal opens on top of the first. Cleanup
//   ordering becomes load-bearing — close the inner modal and its
//   cleanup wipes the outer modal's scroll lock, leaving the user
//   with a scrollable background while the outer dialog is still on
//   screen. We hit a worse variant of the same bug ("inner modal
//   leaks past its parent and swallows clicks until reload"), and
//   defensive flags in each call site only kicked the can.
//
// Design:
//   - The store holds an LIFO stack of opaque IDs (one per mounted
//     Modal). Order matters: the last entry is the dialog the user
//     can interact with.
//   - The Modal component pushes its ID on open and pops it on close
//     / unmount. Body scroll lock derives from `stack.length > 0`,
//     not from any individual modal's lifecycle, so two stacked
//     modals can't fight over it.
//   - `topId` lets a modal know whether it's the active one — used
//     by Escape and focus-trap handlers so a background modal
//     doesn't compete with the foreground one.
//   - Stays deliberately tiny. Cascade-close (when a parent closes,
//     drop everything that was on top of it) is handled at the call
//     site by structural fixes (wizard steps in the same modal, not
//     modal-in-modal) — putting it here would require coupling the
//     store to React's tree, which is the wrong shape.

import { create } from "zustand";

interface ModalStackState {
  stack: string[];
  push: (id: string) => void;
  remove: (id: string) => void;
}

export const useModalStack = create<ModalStackState>((set) => ({
  stack: [],
  push: (id) =>
    set((s) => (s.stack.includes(id) ? s : { stack: [...s.stack, id] })),
  remove: (id) =>
    set((s) => {
      const next = s.stack.filter((x) => x !== id);
      return next.length === s.stack.length ? s : { stack: next };
    }),
}));

/** Convenience selectors so call sites don't have to know the shape. */
export const modalStackSelectors = {
  count: (s: ModalStackState) => s.stack.length,
  topId: (s: ModalStackState) =>
    s.stack.length === 0 ? null : s.stack[s.stack.length - 1],
};
