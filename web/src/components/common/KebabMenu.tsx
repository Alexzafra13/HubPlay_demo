// KebabMenu — three-dots `⋮` button that opens a dropdown of
// actions. Built deliberately tiny: no Radix Popover dependency,
// no portal, no positioning math beyond `right-0 top-full`. The
// admin pages use this for "more actions" on row cards (mobile
// UsersAdmin), where the actions list is small (≤ 7) and the
// trigger is always near the right edge of its container.
//
// Behaviour:
//   - Click trigger toggles the menu.
//   - Click outside closes (mousedown listener on document).
//   - Escape closes (keydown listener while open).
//   - Item click fires action AND closes the menu.
//
// Items can flag themselves as `danger` (red text) or `disabled`
// (grey + non-clickable). A `hint` line renders under the label
// as a small explanation — useful when the action's effect isn't
// obvious from the label alone (e.g. "Eliminar — borra todo el
// historial").

import { useEffect, useRef, useState } from "react";
import { MoreVertical } from "lucide-react";

export interface KebabMenuItem {
  label: string;
  onClick: () => void;
  danger?: boolean;
  disabled?: boolean;
  hint?: string;
  /** Optional icon rendered to the left of the label. Pass a
   *  lucide-react component (without instantiation) so the menu
   *  can size it consistently. */
  icon?: React.ComponentType<{ className?: string }>;
  /** When true, the item is dropped from the rendered menu
   *  entirely. Useful for callsites computing conditional arrays
   *  inline — a single field is cleaner than `.filter(Boolean)`
   *  ceremony at every call. */
  hidden?: boolean;
}

interface KebabMenuProps {
  items: KebabMenuItem[];
  /** Aria label for the trigger button. Required because the
   *  visible icon alone doesn't carry meaning for screen readers. */
  ariaLabel: string;
}

export function KebabMenu({ items, ariaLabel }: KebabMenuProps) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    function onDocClick(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        setOpen(false);
      }
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") setOpen(false);
    }
    document.addEventListener("mousedown", onDocClick);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDocClick);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  // Filter out disabled items the caller passed in if it doesn't
  // matter to render them (e.g. `+ Perfil` only on parents). The
  // simpler API is to let the caller decide what to put in items;
  // disabled items still render so the row stays a stable
  // affordance ("the option exists but you can't use it here").
  const visible = items.filter((it) => !it.hidden);

  if (visible.length === 0) return null;

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-label={ariaLabel}
        aria-haspopup="menu"
        aria-expanded={open}
        className="inline-flex h-9 w-9 items-center justify-center rounded-full text-text-muted transition-colors hover:bg-bg-hover hover:text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent"
      >
        <MoreVertical className="h-4 w-4" />
      </button>

      {open && (
        <div
          role="menu"
          className="absolute right-0 top-full z-30 mt-1 min-w-[200px] overflow-hidden rounded-md border border-border bg-bg-card shadow-xl"
        >
          {visible.map((it, i) => {
            const Icon = it.icon;
            const baseClass = [
              "flex w-full items-center gap-2.5 px-3 py-2 text-left text-sm transition-colors",
              it.disabled
                ? "cursor-not-allowed text-text-muted/60"
                : it.danger
                  ? "text-error hover:bg-error/10"
                  : "text-text-primary hover:bg-bg-hover",
            ].join(" ");
            return (
              <button
                key={i}
                type="button"
                role="menuitem"
                disabled={it.disabled}
                onClick={() => {
                  if (it.disabled) return;
                  setOpen(false);
                  it.onClick();
                }}
                className={baseClass}
              >
                {Icon && (
                  <Icon className="h-3.5 w-3.5 shrink-0 opacity-70" />
                )}
                <span className="flex-1">
                  <span className="block">{it.label}</span>
                  {it.hint && (
                    <span className="block text-xs text-text-muted">
                      {it.hint}
                    </span>
                  )}
                </span>
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}

