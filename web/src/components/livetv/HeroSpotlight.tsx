import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { Channel, EPGProgram } from "@/api/types";
import { ChannelLogo } from "./ChannelLogo";
import { StreamPreview } from "./StreamPreview";
import { formatTime, getProgramProgress } from "./epgHelpers";

export interface HeroSpotlightItem {
  channel: Channel;
  nowPlaying?: EPGProgram | null;
}

/** Signal the caller chose to populate the hero. Displayed on the gear
 * menu so the viewer can swap it without leaving Discover. */
export type HeroMode = "favorites" | "live-now" | "newest" | "off";

export interface HeroModeOption {
  mode: HeroMode;
  label: string;
  hint?: string;
  disabled?: boolean;
}

interface HeroSpotlightProps {
  /**
   * Items to feature. The first is shown on mount; if there are more,
   * the hero cycles through them every ~12 s and exposes carousel
   * dots to scrub manually.
   */
  items: HeroSpotlightItem[];
  /** Label above the hero title — e.g. "Tu favorito" or "Destacado". */
  label: string;
  /** Current hero mode — drives the gear menu's highlight. */
  mode: HeroMode;
  /** Options the gear menu offers, in display order. */
  modeOptions: HeroModeOption[];
  onModeChange: (mode: HeroMode) => void;
  onOpen?: (channel: Channel) => void;
}

/** How long each hero slide stays visible before auto-advancing. */
const ROTATE_MS = 12_000;

/**
 * HeroSpotlight — the top-of-Discover focal point.
 *
 * Design intent (opinionated):
 *  - One slide at a time. A mosaic of five unrelated tiles (the old
 *    HeroMosaic) was visually loud and communicated nothing — the
 *    viewer couldn't tell why those particular five were featured.
 *    One slide with a clear label answers the "why is this here"
 *    question immediately: it's yours, you marked it.
 *  - Auto-preview. The first slide starts a muted HLS preview on
 *    mount so landing on Discover feels alive, not a static poster
 *    wall. No hover, no click needed.
 *  - Carousel dots. Respects the viewer's agency — they can jump
 *    across slides without waiting for the rotation timer.
 *  - Label at the top (not the bottom). Screen-reader and sighted
 *    users both learn what they're looking at before parsing the
 *    visual.
 */
export function HeroSpotlight({
  items,
  label,
  mode,
  modeOptions,
  onModeChange,
  onOpen,
}: HeroSpotlightProps) {
  const { t } = useTranslation();
  const [rawIdx, setIdx] = useState(0);
  const [gearOpen, setGearOpen] = useState(false);

  // Clamp the index to the current items length at render time (rather
  // than through a setState-in-effect, which the lint rightly objects
  // to). Handles the "user unfavorited the slide currently on screen"
  // case transparently.
  const idx = items.length === 0 ? 0 : rawIdx % items.length;

  // Close gear menu on Escape / outside click. Kept local to the
  // component so the parent doesn't have to orchestrate popover state.
  useEffect(() => {
    if (!gearOpen) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setGearOpen(false);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [gearOpen]);

  // Auto-rotation. Pauses when items has only one slide (nothing to
  // rotate to) so we don't pointlessly re-render.
  useEffect(() => {
    if (items.length < 2) return;
    const timer = window.setInterval(() => {
      setIdx((i) => (i + 1) % items.length);
    }, ROTATE_MS);
    return () => window.clearInterval(timer);
  }, [items.length]);

  if (mode === "off") return null;

  // Even when `items` is empty we still want the gear available so the
  // user can pick a different signal — otherwise "my favorites is empty
  // so the hero disappears forever" becomes a dead end. Render a slim
  // empty-state strip with just the gear.
  if (items.length === 0) {
    return (
      <EmptyHero
        label={label}
        mode={mode}
        modeOptions={modeOptions}
        onModeChange={onModeChange}
        gearOpen={gearOpen}
        setGearOpen={setGearOpen}
        t={t}
      />
    );
  }
  const current = items[idx];
  const { channel, nowPlaying } = current;
  const progress = nowPlaying ? getProgramProgress(nowPlaying) : 0;

  // Backdrop tuned brighter than a ChannelCard — this is the hero, it
  // deserves a stronger presence — but still anchored on the neutral
  // base so the spotlight doesn't clash with the rails below.
  const bg = `radial-gradient(circle at 20% 10%, ${channel.logo_bg}66 0%, transparent 60%), linear-gradient(180deg, var(--tv-bg-2) 0%, var(--tv-bg-0) 100%)`;

  return (
    <section
      aria-label={label}
      className="relative"
    >
      {/* Click target covers the whole hero. Using a <button> as the
          primary click surface keeps screen-reader semantics; the gear
          and carousel dots sit OUTSIDE it (as siblings with higher
          z-index) because nested interactive elements inside a button
          are invalid HTML. */}
      <button
        type="button"
        onClick={() => onOpen?.(channel)}
        className="group relative block aspect-[21/9] w-full overflow-hidden rounded-tv-lg border border-tv-line text-left transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-tv-accent md:aspect-[24/9]"
        aria-label={
          nowPlaying
            ? `${channel.name} — ${nowPlaying.title}`
            : channel.name
        }
      >
        <div
          className="pointer-events-none absolute inset-0"
          style={{ background: bg }}
        />

        {/* Auto-preview. Keyed on channel.id so switching slides
            dismounts the old HLS instance and mounts a fresh one for
            the new channel — no state leakage, no stale stream. */}
        <StreamPreview
          key={channel.id}
          streamUrl={channel.stream_url}
          className="absolute inset-0 h-full w-full object-cover opacity-80"
        />

        {/* Dark vignette so the caption stays readable over any video
            content. Always on, even when preview fails. */}
        <div
          className="pointer-events-none absolute inset-0 bg-gradient-to-t from-black/80 via-black/30 to-black/20"
          aria-hidden="true"
        />

        {/* Top meta row: label ("Tu favorito") + LIVE pill + country. */}
        <div className="pointer-events-none absolute left-5 right-14 top-5 flex items-center gap-2">
          <span className="rounded-tv-xs bg-tv-accent/90 px-2 py-0.5 text-[11px] font-bold uppercase tracking-wider text-tv-accent-ink">
            {label}
          </span>
          {nowPlaying && (
            <span className="flex items-center gap-1 rounded-tv-xs bg-tv-live/90 px-2 py-0.5 text-[11px] font-bold uppercase tracking-wider text-white">
              <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-white" />
              Live
            </span>
          )}
          {channel.country && (
            <span className="rounded-tv-xs bg-black/40 px-2 py-0.5 font-mono text-[11px] font-semibold uppercase tracking-wider text-tv-fg-1 backdrop-blur">
              {channel.country}
            </span>
          )}
        </div>

        {/* Bottom caption. */}
        <div className="absolute inset-x-5 bottom-5 flex flex-col gap-3">
          <div className="flex items-end gap-3">
            <ChannelLogo
              logoUrl={channel.logo_url}
              initials={channel.logo_initials}
              bg={channel.logo_bg}
              fg={channel.logo_fg}
              name={channel.name}
              className="h-14 w-14 rounded-tv-md ring-2 ring-white/10 shadow-lg"
              textClassName="text-base font-bold"
            />
            <div className="min-w-0 flex-1">
              <div className="font-mono text-[11px] uppercase tracking-widest text-tv-fg-2">
                CH {channel.number}
              </div>
              <div className="truncate text-xl font-semibold text-tv-fg-0 md:text-2xl">
                {channel.name}
              </div>
            </div>
          </div>

          {nowPlaying ? (
            <>
              <div className="line-clamp-2 max-w-3xl text-sm text-tv-fg-1 md:text-base">
                <span className="mr-1.5 uppercase tracking-wider text-tv-fg-3">
                  Ahora
                </span>
                {nowPlaying.title}
              </div>
              <div className="flex max-w-xl items-center gap-2">
                <div className="h-1 flex-1 overflow-hidden rounded-full bg-white/10">
                  <div
                    className="h-full rounded-full bg-tv-accent transition-[width] duration-1000"
                    style={{ width: `${progress}%` }}
                  />
                </div>
                <span className="font-mono text-[11px] tabular-nums text-tv-fg-2">
                  {formatTime(nowPlaying.end_time)}
                </span>
              </div>
            </>
          ) : null}
        </div>
      </button>

      {/* Gear — personalisation menu. Sibling of the click-target button
          (not child) because nested interactive elements inside a
          button are invalid HTML AND would bubble clicks into onOpen. */}
      <div className="pointer-events-none absolute right-4 top-4 z-10">
        <div className="pointer-events-auto">
          <GearMenu
            mode={mode}
            modeOptions={modeOptions}
            onModeChange={onModeChange}
            open={gearOpen}
            setOpen={setGearOpen}
            t={t}
          />
        </div>
      </div>

      {/* Carousel dots — only render when there's more than one slide.
          Kept outside the <button> so clicks on a dot don't bubble
          into the hero's onOpen. */}
      {items.length > 1 ? (
        <div
          className="mt-3 flex items-center justify-center gap-2"
          role="tablist"
          aria-label={t("liveTV.hero.dots", {
            defaultValue: "Elegir destacado",
          })}
        >
          {items.map((item, i) => (
            <button
              key={item.channel.id}
              type="button"
              role="tab"
              aria-selected={i === idx}
              aria-label={t("liveTV.hero.goTo", {
                defaultValue: "Ir a {{name}}",
                name: item.channel.name,
              })}
              onClick={() => setIdx(i)}
              className={[
                "h-1.5 rounded-full transition-all",
                i === idx
                  ? "w-8 bg-tv-accent"
                  : "w-2 bg-tv-line hover:bg-tv-line-strong",
              ].join(" ")}
            />
          ))}
        </div>
      ) : null}
    </section>
  );
}

// ───────────────────────────────────────────────────────────────────
// GearMenu
// ───────────────────────────────────────────────────────────────────
//
// Hero personalisation dropdown. Deliberately minimal — a round
// translucent button that pops a small menu with radio-style
// selections. Lives absolute-positioned over the hero and closes on
// selection, Escape, or outside click.

interface GearMenuProps {
  mode: HeroMode;
  modeOptions: HeroModeOption[];
  onModeChange: (mode: HeroMode) => void;
  open: boolean;
  setOpen: (open: boolean) => void;
  t: ReturnType<typeof useTranslation>["t"];
}

function GearMenu({
  mode,
  modeOptions,
  onModeChange,
  open,
  setOpen,
  t,
}: GearMenuProps) {
  // Outside-click dismiss: attach on open, detach on close.
  useEffect(() => {
    if (!open) return;
    const onClick = (e: MouseEvent) => {
      const target = e.target as HTMLElement;
      if (!target.closest("[data-hero-gear]")) {
        setOpen(false);
      }
    };
    window.addEventListener("mousedown", onClick);
    return () => window.removeEventListener("mousedown", onClick);
  }, [open, setOpen]);

  return (
    <div data-hero-gear className="relative">
      <button
        type="button"
        aria-label={t("liveTV.hero.customise", {
          defaultValue: "Personalizar destacado",
        })}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
        className="flex h-9 w-9 items-center justify-center rounded-full bg-black/50 text-tv-fg-0 backdrop-blur transition hover:bg-black/70 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-tv-accent"
      >
        <GearIcon />
      </button>
      {open ? (
        <div
          role="menu"
          className="absolute right-0 top-11 w-64 rounded-tv-md border border-tv-line bg-tv-bg-1 py-1 shadow-tv-lg"
        >
          <div className="border-b border-tv-line px-3 py-2">
            <div className="text-xs font-semibold text-tv-fg-0">
              {t("liveTV.hero.customiseTitle", {
                defaultValue: "Qué ver destacado",
              })}
            </div>
            <p className="mt-0.5 text-[11px] text-tv-fg-3">
              {t("liveTV.hero.customiseHint", {
                defaultValue: "Se guarda en tu cuenta para todos tus dispositivos.",
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
                  <span className="block text-sm font-medium">{opt.label}</span>
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
      width="16"
      height="16"
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

// ───────────────────────────────────────────────────────────────────
// EmptyHero — shown when the selected signal produced no items.
// Keeps the gear available so the user can pick a different signal.
// ───────────────────────────────────────────────────────────────────

interface EmptyHeroProps {
  label: string;
  mode: HeroMode;
  modeOptions: HeroModeOption[];
  onModeChange: (mode: HeroMode) => void;
  gearOpen: boolean;
  setGearOpen: (open: boolean) => void;
  t: ReturnType<typeof useTranslation>["t"];
}

function EmptyHero({
  label,
  mode,
  modeOptions,
  onModeChange,
  gearOpen,
  setGearOpen,
  t,
}: EmptyHeroProps) {
  return (
    <section
      aria-label={label}
      className="relative flex items-center justify-between gap-4 rounded-tv-lg border border-dashed border-tv-line bg-tv-bg-1 px-5 py-4"
    >
      <div>
        <div className="text-[11px] font-bold uppercase tracking-wider text-tv-fg-3">
          {label}
        </div>
        <p className="mt-1 text-sm text-tv-fg-2">
          {t("liveTV.hero.emptyHint", {
            defaultValue:
              "Nada para destacar ahora. Cambia la fuente desde la rueda para ver otra cosa aquí arriba.",
          })}
        </p>
      </div>
      <GearMenu
        mode={mode}
        modeOptions={modeOptions}
        onModeChange={onModeChange}
        open={gearOpen}
        setOpen={setGearOpen}
        t={t}
      />
    </section>
  );
}
