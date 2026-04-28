import { useEffect, useCallback, useRef } from "react";
import { createPortal } from "react-dom";
import type { FC, ReactNode } from "react";

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

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        onClose();
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
    },
    [onClose],
  );

  // Body scroll lock + key handler. Tied to isOpen so the rest of the
  // page is responsive again the moment the modal closes.
  useEffect(() => {
    if (!isOpen) return;
    document.addEventListener("keydown", handleKeyDown);
    document.body.style.overflow = "hidden";
    return () => {
      document.removeEventListener("keydown", handleKeyDown);
      document.body.style.overflow = "";
    };
  }, [isOpen, handleKeyDown]);

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
                className="h-5 w-5"
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
