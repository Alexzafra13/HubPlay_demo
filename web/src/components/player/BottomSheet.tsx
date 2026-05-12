import { useEffect, useRef } from "react";
import type { FC, ReactNode } from "react";
import { useTranslation } from "react-i18next";

interface BottomSheetProps {
  open: boolean;
  title: string;
  onClose: () => void;
  children: ReactNode;
}

// BottomSheet is the mobile-first picker surface for audio, subs and
// settings. It anchors to the bottom of the player overlay, fills the
// width, and caps its own height at 75vh with internal scroll so a
// long track list on a short landscape phone doesn't crowd the seek
// bar. Closes on backdrop click, on Escape, and on the drag-handle
// being interacted with (touch + click). Body scroll is locked while
// open so a stray vertical swipe on the backdrop doesn't pull the
// page underneath the fullscreen player.
const BottomSheet: FC<BottomSheetProps> = ({ open, title, onClose, children }) => {
  const { t } = useTranslation();
  const dialogRef = useRef<HTMLDivElement | null>(null);

  // Escape-to-close. Bound at the window so the sheet doesn't need
  // focus to be reachable — the user may have just tapped a button
  // and the focus is on a now-hidden element.
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        onClose();
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [open, onClose]);

  // Body-scroll lock while open. The player itself is already
  // fullscreen so the page underneath rarely matters, but on iOS
  // Safari the rubber-band scroll can still pull the body chrome
  // during a long sheet scroll. Saving + restoring the original
  // overflow keeps the page intact when the user closes the sheet.
  useEffect(() => {
    if (!open) return;
    const prev = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.body.style.overflow = prev;
    };
  }, [open]);

  if (!open) return null;

  return (
    <div
      className="absolute inset-0 z-50 flex items-end justify-center"
      role="dialog"
      aria-modal="true"
      aria-label={title}
      onClick={(e) => {
        // Backdrop tap closes; sheet body taps are stopped below.
        if (e.target === e.currentTarget) onClose();
      }}
    >
      {/* Backdrop — slightly translucent so the player chrome behind
          stays visible but darkens enough to focus attention. */}
      <div className="absolute inset-0 bg-black/60 backdrop-blur-sm" aria-hidden="true" />

      {/* Sheet body. max-w-md so on tablet/landscape it doesn't
          stretch the full width and look like a heavy modal. */}
      <div
        ref={dialogRef}
        className="relative w-full max-w-md mx-auto rounded-t-2xl border-t border-x border-border bg-bg-card/95 backdrop-blur-md shadow-2xl text-text-primary max-h-[75vh] flex flex-col animate-[sheet-up_180ms_ease-out]"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Drag handle visual — non-functional today (no swipe-down
            gesture), but it telegraphs "this is a sheet you can
            dismiss" the way the rest of the mobile world does. */}
        <button
          type="button"
          onClick={onClose}
          aria-label={t("playerControls.sheet.close")}
          className="flex justify-center pt-3 pb-1 cursor-pointer"
        >
          <span className="block h-1 w-10 rounded-full bg-white/30" />
        </button>

        <div className="flex items-center justify-between px-4 pb-2">
          <h3 className="text-sm font-semibold uppercase tracking-wider text-text-muted">
            {title}
          </h3>
          <button
            type="button"
            onClick={onClose}
            aria-label={t("playerControls.sheet.close")}
            className="p-2 -mr-2 rounded-[--radius-sm] text-text-muted hover:text-text-primary hover:bg-white/10 transition-colors cursor-pointer"
          >
            <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}>
              <path d="M6 6l12 12M18 6L6 18" strokeLinecap="round" />
            </svg>
          </button>
        </div>

        <div className="overflow-y-auto px-2 pb-4">{children}</div>
      </div>
    </div>
  );
};

interface SheetRowProps {
  selected: boolean;
  label: string;
  /** Optional secondary text below the label (e.g. "AAC 5.1"). */
  sublabel?: string;
  onClick: () => void;
  /** Optional icon node rendered on the leading edge. */
  leading?: ReactNode;
}

// A single tappable row inside a sheet. 48px tall (44px hit + 4px
// padding) to meet WCAG / Apple HIG touch-target guidance. The
// selected state shows a check on the trailing edge — clearer than a
// background tint on mobile where contrast varies wildly across
// devices.
const SheetRow: FC<SheetRowProps> = ({ selected, label, sublabel, onClick, leading }) => (
  <button
    type="button"
    onClick={onClick}
    className={[
      "w-full flex items-center gap-3 px-3 py-3 rounded-[--radius-md] text-left transition-colors cursor-pointer",
      selected
        ? "bg-accent/15 text-accent"
        : "text-text-primary hover:bg-white/5",
    ].join(" ")}
    aria-pressed={selected}
  >
    {leading && <span className="shrink-0 text-text-muted">{leading}</span>}
    <span className="flex-1 min-w-0">
      <span className="block text-sm font-medium truncate">{label}</span>
      {sublabel && (
        <span className="block text-xs text-text-muted truncate">{sublabel}</span>
      )}
    </span>
    {selected && (
      <svg className="h-4 w-4 shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2.5}>
        <path d="M5 12l5 5L20 7" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
    )}
  </button>
);

interface SheetSectionProps {
  title?: string;
  children: ReactNode;
}

const SheetSection: FC<SheetSectionProps> = ({ title, children }) => (
  <section className="py-1">
    {title && (
      <div className="px-3 pt-2 pb-1 text-[11px] font-semibold uppercase tracking-wider text-text-muted">
        {title}
      </div>
    )}
    <div className="flex flex-col gap-0.5">{children}</div>
  </section>
);

export { BottomSheet, SheetRow, SheetSection };
