import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

/** Signal the caller picked to populate the hero. */
export type HeroMode = "favorites" | "live-now" | "newest" | "off";

export interface HeroModeOption {
  mode: HeroMode;
  label: string;
  hint?: string;
  disabled?: boolean;
}

interface HeroSettingsProps {
  mode: HeroMode;
  modeOptions: HeroModeOption[];
  onModeChange: (mode: HeroMode) => void;
}

/**
 * HeroSettings — page-level "view" control that lives in the LiveTV
 * topbar, not inside the hero. Putting it here solves three UX traps
 * the previous in-hero gear had:
 *
 *   - "Off" became a dead end: hiding the hero hid the only control
 *     that could bring it back. With the gear in the topbar it's
 *     always reachable, independent of whether the hero renders.
 *   - The popover was clipped by the hero's aspect ratio on smaller
 *     viewports (the menu extended ~250 px down, the hero was ~290 px,
 *     so the bottom of the menu bled into the rails below). A topbar
 *     anchor has the whole page below it to flow into.
 *   - Visual tax: the hero is a poster. Painting configuration chrome
 *     on top of an auto-playing stream fought for attention. A small
 *     "Vista" button is quieter.
 */
export function HeroSettings({ mode, modeOptions, onModeChange }: HeroSettingsProps) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);

  // Outside-click / Escape dismiss.
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    const onClick = (e: MouseEvent) => {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    window.addEventListener("keydown", onKey);
    window.addEventListener("mousedown", onClick);
    return () => {
      window.removeEventListener("keydown", onKey);
      window.removeEventListener("mousedown", onClick);
    };
  }, [open]);

  const activeLabel =
    modeOptions.find((o) => o.mode === mode)?.label ??
    t("liveTV.hero.customise", { defaultValue: "Personalizar destacado" });

  return (
    <div ref={rootRef} className="relative">
      <button
        type="button"
        aria-label={t("liveTV.hero.customise", {
          defaultValue: "Personalizar destacado",
        })}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
        className="flex items-center gap-1.5 rounded-full border border-tv-line bg-tv-bg-1 px-3 py-1.5 text-xs font-medium text-tv-fg-1 transition hover:border-tv-line-strong hover:text-tv-fg-0 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-tv-accent"
      >
        <GearIcon />
        <span className="hidden sm:inline">{activeLabel}</span>
      </button>
      {open ? (
        <div
          role="menu"
          className="absolute right-0 top-[calc(100%+0.5rem)] z-30 w-72 rounded-tv-md border border-tv-line bg-tv-bg-1 py-1 shadow-tv-lg"
        >
          <div className="border-b border-tv-line px-3 py-2">
            <div className="text-xs font-semibold text-tv-fg-0">
              {t("liveTV.hero.customiseTitle", {
                defaultValue: "Qué ver destacado",
              })}
            </div>
            <p className="mt-0.5 text-[11px] text-tv-fg-3">
              {t("liveTV.hero.customiseHint", {
                defaultValue:
                  "Se guarda en tu cuenta para todos tus dispositivos.",
              })}
            </p>
          </div>
          {modeOptions.map((opt) => {
            const selected = opt.mode === mode;
            return (
              <button
                key={opt.mode}
                role="menuitemradio"
                aria-checked={selected}
                disabled={opt.disabled}
                onClick={() => {
                  onModeChange(opt.mode);
                  setOpen(false);
                }}
                className={[
                  "flex w-full items-start gap-2 px-3 py-2 text-left transition",
                  selected
                    ? "bg-tv-accent/10 text-tv-fg-0"
                    : "text-tv-fg-1 hover:bg-tv-bg-2 hover:text-tv-fg-0",
                  opt.disabled && "cursor-not-allowed opacity-40",
                ]
                  .filter(Boolean)
                  .join(" ")}
              >
                <span
                  className={[
                    "mt-0.5 inline-block h-4 w-4 shrink-0 rounded-full border-2",
                    selected
                      ? "border-tv-accent bg-tv-accent"
                      : "border-tv-line",
                  ].join(" ")}
                  aria-hidden="true"
                />
                <span className="min-w-0 flex-1">
                  <span className="block text-sm font-medium">
                    {opt.label}
                  </span>
                  {opt.hint ? (
                    <span className="block text-[11px] text-tv-fg-3">
                      {opt.hint}
                    </span>
                  ) : null}
                </span>
              </button>
            );
          })}
        </div>
      ) : null}
    </div>
  );
}

function GearIcon() {
  return (
    <svg
      width="14"
      height="14"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <circle cx="12" cy="12" r="3" />
      <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
    </svg>
  );
}
