// Sheet — side-anchored overlay for contextual actions (Edit, Filters,
// Details). Lives in the same useModalStack as Modal, so scroll lock,
// Escape, focus trap and "ghost overlay" prevention are all shared.
//
// Why a sheet (and not another Modal)?
//   Modals interrupt. They take the centre of the screen and demand
//   attention. They're right for confirmations, picks, decisions —
//   anything where the user can't usefully see the underlying page
//   while they choose. CRUD edits are a different shape: the user is
//   acting on a row in a list, the row should stay visible, and the
//   form on the right should feel like a contextual extension of
//   the list, not a separate dialog. That's a sheet.
//
// Mobile behaviour: on screens < sm the sheet expands to fullscreen
// (no half-overlay weirdness, no thumb-reachability problems).

import { useEffect, useId, useRef } from "react";
import { createPortal } from "react-dom";
import type { FC, ReactNode } from "react";
import { useModalStack, modalStackSelectors } from "@/store/modalStack";

type SheetSize = "sm" | "md" | "lg";

interface SheetProps {
  isOpen: boolean;
  onClose: () => void;
  title?: string;
  description?: string;
  children: ReactNode;
  size?: SheetSize;
  /**
   * Optional footer rendered as a sticky bar at the bottom of the
   * sheet. Use for primary actions (Save / Cancel) so they stay
   * visible while the body scrolls.
   */
  footer?: ReactNode;
}

const sizeStyles: Record<SheetSize, string> = {
  sm: "sm:max-w-sm",
  md: "sm:max-w-md",
  lg: "sm:max-w-lg",
};

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

const Sheet: FC<SheetProps> = ({
  isOpen,
  onClose,
  title,
  description,
  children,
  size = "md",
  footer,
}) => {
  const dialogRef = useRef<HTMLDivElement>(null);
  const id = useId();
  const push = useModalStack((s) => s.push);
  const remove = useModalStack((s) => s.remove);
  const stackCount = useModalStack(modalStackSelectors.count);
  const topId = useModalStack(modalStackSelectors.topId);
  const isTop = topId === id;

  // Same lifecycle contract as Modal: register on open, drop on
  // unmount, and clear the body lock inline if we were the last one
  // up (no live subscriber would react after unmount otherwise).
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

  useEffect(() => {
    if (stackCount === 0) {
      document.body.style.overflow = "";
      return;
    }
    document.body.style.overflow = "hidden";
    // Defensive cleanup: if the component unmounts while the stack
    // hasn't drained (route change while open), reset on the way out
    // so the body lock doesn't leak across navigations. Inline reset
    // in the first effect already handles the normal close path; this
    // catches the abrupt-unmount case.
    return () => {
      if (useModalStack.getState().stack.length === 0) {
        document.body.style.overflow = "";
      }
    };
  }, [stackCount]);

  // onClose via ref — el listener se monta una vez por (isOpen, isTop)
  // y no se re-suscribe cuando el padre cambia la identidad de la
  // callback. Igual que Modal.
  const onCloseRef = useRef(onClose);
  useEffect(() => {
    onCloseRef.current = onClose;
  }, [onClose]);

  useEffect(() => {
    if (!isOpen || !isTop) return;
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        onCloseRef.current();
        return;
      }
      if (e.key !== "Tab") return;
      const dialog = dialogRef.current;
      if (!dialog) return;
      const focusables = focusableWithin(dialog);
      if (focusables.length === 0) {
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
      } else if (active === last) {
        e.preventDefault();
        first.focus();
      }
    };
    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [isOpen, isTop]);

  useEffect(() => {
    if (!isOpen) return;
    const previousActive = document.activeElement as HTMLElement | null;
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
    <div className="fixed inset-0 z-50">
      {/* Backdrop — softer than the modal's; the user is meant to
          still feel anchored to the page underneath. */}
      <div
        className="absolute inset-0 bg-black/50 backdrop-blur-[2px] animate-fade-in"
        onClick={onClose}
        aria-hidden="true"
      />

      {/* Panel */}
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-label={title}
        tabIndex={-1}
        className={[
          // Mobile: full screen. Desktop: anchored right with width cap.
          "absolute inset-0 sm:left-auto sm:right-0 sm:h-full sm:w-full",
          "flex flex-col outline-none",
          "bg-bg-card sm:border-l border-border shadow-[0_0_40px_rgba(0,0,0,0.5)]",
          "animate-slide-in-right",
          sizeStyles[size],
        ].join(" ")}
      >
        {/* Header — slim, no chunky padding. The X is on the left
            because that's where a "back" intent reads from in a
            side-anchored panel; the user is moving away from this
            sheet, not closing a centred dialog. */}
        {(title || description) && (
          <header className="flex items-start gap-3 px-5 py-4 border-b border-border">
            <button
              type="button"
              onClick={onClose}
              className="mt-0.5 -ml-1.5 p-1.5 rounded-[--radius-sm] text-text-muted hover:text-text-primary hover:bg-bg-elevated transition-colors"
              aria-label="Close"
            >
              <svg
                className="size-4"
                viewBox="0 0 20 20"
                fill="none"
                stroke="currentColor"
                strokeWidth={1.5}
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  d="M12.5 15l-5-5 5-5"
                />
              </svg>
            </button>
            <div className="min-w-0 flex-1">
              {title && (
                <h2 className="text-[15px] font-semibold tracking-tight text-text-primary leading-tight truncate">
                  {title}
                </h2>
              )}
              {description && (
                <p className="mt-0.5 text-[12px] text-text-muted">
                  {description}
                </p>
              )}
            </div>
          </header>
        )}

        {/* Body — owns its scroll, footer stays pinned. */}
        <div className="flex-1 overflow-y-auto px-5 py-4">{children}</div>

        {/* Footer — sticky strip with primary actions. */}
        {footer && (
          <footer className="flex items-center justify-end gap-2 px-5 py-3 border-t border-border bg-bg-surface/60">
            {footer}
          </footer>
        )}
      </div>
    </div>,
    document.body,
  );
};

export { Sheet };
export type { SheetProps, SheetSize };
