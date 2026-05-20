import { useEffect, useId, useRef } from "react";
import { createPortal } from "react-dom";
import type { FC, ReactNode } from "react";
import { useModalStack, modalStackSelectors } from "@/store/modalStack";

type ModalSize = "sm" | "md" | "lg";

interface ModalProps {
  isOpen: boolean;
  onClose: () => void;
  title?: string;
  children: ReactNode;
  size?: ModalSize;
}

const sizeStyles: Record<ModalSize, string> = {
  sm: "max-w-sm",
  md: "max-w-lg",
  lg: "max-w-2xl",
};

// Selectors that the WAI-ARIA practices guide considers reasonable to
// move focus into. Anything with tabindex="-1" is excluded — those
// nodes are programmatically focusable but not part of the Tab cycle.
const FOCUSABLE_SELECTOR = [
  "button:not([disabled])",
  "[href]",
  "input:not([disabled])",
  "select:not([disabled])",
  "textarea:not([disabled])",
  "[tabindex]:not([tabindex='-1'])",
].join(",");

function focusableWithin(root: HTMLElement): HTMLElement[] {
  return Array.from(
    root.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR),
  ).filter((el) => !el.hasAttribute("aria-hidden"));
}

const Modal: FC<ModalProps> = ({
  isOpen,
  onClose,
  title,
  children,
  size = "md",
}) => {
  const dialogRef = useRef<HTMLDivElement>(null);

  // Stable, opaque id for this Modal's lifetime. Used to register
  // with the global stack so scroll lock + focus + escape behave
  // correctly when modals are stacked.
  const id = useId();
  const push = useModalStack((s) => s.push);
  const remove = useModalStack((s) => s.remove);
  const stackCount = useModalStack(modalStackSelectors.count);
  const topId = useModalStack(modalStackSelectors.topId);
  const isTop = topId === id;

  // Register / unregister this modal in the global stack. Pushing on
  // every open and popping on close OR unmount means a parent that
  // unmounts mid-flight (route change, reload, error boundary) takes
  // its entry with it — no orphan portal can survive.
  //
  // The cleanup also handles the "last modal vanishes" case for body
  // scroll: when this is the only modal up and it unmounts, the
  // sibling useEffect below has no live subscriber left to clear the
  // lock, so we do it inline here. Reading the stack snapshot via
  // getState() rather than a closure keeps the value fresh after the
  // remove() that just ran.
  useEffect(() => {
    if (!isOpen) return;
    push(id);
    return () => {
      remove(id);
      if (useModalStack.getState().stack.length === 0) {
        document.body.style.overflow = "";
      }
    };
  }, [isOpen, id, push, remove]);

  // Body scroll lock is derived from the stack count, NOT from this
  // modal's open flag. Two siblings opening together both contribute
  // to the count; closing one drops the count by 1 but leaves the
  // lock in place while the other is still up. The previous per-modal
  // useEffect cleanup wiped the lock the moment the inner modal
  // closed, even when an outer one was still open — that's the bug
  // this whole refactor exists to kill.
  useEffect(() => {
    if (stackCount === 0) {
      document.body.style.overflow = "";
      return;
    }
    document.body.style.overflow = "hidden";
    // Defensive: route-level unmount while still open. The first
    // useEffect's cleanup handles the close path, but if the parent
    // tree disappears before we reach close, reset the lock here too.
    return () => {
      if (useModalStack.getState().stack.length === 0) {
        document.body.style.overflow = "";
      }
    };
  }, [stackCount]);

  // onClose se guarda en un ref para que el listener de keydown no se
  // re-suscriba cada render del padre (la identidad de onClose rota
  // habitualmente al venir de un useState/useCallback con deps).
  const onCloseRef = useRef(onClose);
  useEffect(() => {
    onCloseRef.current = onClose;
  }, [onClose]);

  // Only the top modal in the stack listens for Escape and Tab. A
  // background modal that grabbed Escape would close itself instead
  // of the dialog the user is actually looking at.
  useEffect(() => {
    if (!isOpen || !isTop) return;
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        onCloseRef.current();
        return;
      }
      // Focus trap: keep Tab + Shift+Tab cycling inside the dialog so
      // keyboard users can't accidentally land on the page underneath
      // (which is aria-hidden but still has its DOM intact). Without
      // this, Tab from the last button escapes the modal entirely.
      if (e.key !== "Tab") return;
      const dialog = dialogRef.current;
      if (!dialog) return;
      const focusables = focusableWithin(dialog);
      if (focusables.length === 0) {
        // Nothing to tab to — pin focus on the dialog itself.
        e.preventDefault();
        dialog.focus();
        return;
      }
      const first = focusables[0];
      const last = focusables[focusables.length - 1];
      const active = document.activeElement as HTMLElement | null;
      if (e.shiftKey) {
        if (active === first || !dialog.contains(active)) {
          e.preventDefault();
          last.focus();
        }
      } else {
        if (active === last) {
          e.preventDefault();
          first.focus();
        }
      }
    };
    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [isOpen, isTop]);

  // On open: remember the element that had focus, then move focus into
  // the dialog. On close: restore focus to the trigger so the keyboard
  // user lands back where they came from. The cleanup runs even if
  // isOpen flips false mid-mount.
  useEffect(() => {
    if (!isOpen) return;
    const previousActive = document.activeElement as HTMLElement | null;
    // Defer the focus call by a tick so the dialog DOM is mounted and
    // the children have settled — focusing too early lands on the
    // dialog wrapper instead of the first interactive element.
    const t = window.setTimeout(() => {
      const dialog = dialogRef.current;
      if (!dialog) return;
      const focusables = focusableWithin(dialog);
      (focusables[0] ?? dialog).focus();
    }, 0);
    return () => {
      window.clearTimeout(t);
      previousActive?.focus?.();
    };
  }, [isOpen]);

  if (!isOpen) return null;

  return createPortal(
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      {/* Backdrop */}
      <div
        className="absolute inset-0 bg-black/60 backdrop-blur-sm animate-fade-in"
        onClick={onClose}
        aria-hidden="true"
      />

      {/* Dialog */}
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-label={title}
        // tabIndex=-1 so the dialog itself can receive focus as a
        // last-resort fallback when no interactive children exist.
        tabIndex={-1}
        className={[
          "relative w-full rounded-[--radius-lg] outline-none",
          "bg-bg-card border border-border shadow-2xl",
          "animate-fade-in",
          sizeStyles[size],
        ].join(" ")}
      >
        {/* Header */}
        {title && (
          <div className="flex items-center justify-between px-6 py-4 border-b border-border">
            <h2 className="text-lg font-semibold text-text-primary">{title}</h2>
            <button
              onClick={onClose}
              className="p-1 rounded-[--radius-sm] text-text-muted hover:text-text-primary hover:bg-bg-elevated transition-colors cursor-pointer"
              aria-label="Close"
            >
              <svg
                className="size-5"
                viewBox="0 0 20 20"
                fill="currentColor"
              >
                <path d="M6.28 5.22a.75.75 0 00-1.06 1.06L8.94 10l-3.72 3.72a.75.75 0 101.06 1.06L10 11.06l3.72 3.72a.75.75 0 101.06-1.06L11.06 10l3.72-3.72a.75.75 0 00-1.06-1.06L10 8.94 6.28 5.22z" />
              </svg>
            </button>
          </div>
        )}

        {/* Body */}
        <div className="px-6 py-4">{children}</div>
      </div>
    </div>,
    document.body,
  );
};

export { Modal };
export type { ModalProps, ModalSize };
