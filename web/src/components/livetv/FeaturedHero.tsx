import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import type { Channel, EPGProgram } from "@/api/types";
import { ChannelLogo } from "./ChannelLogo";
import { categoryMeta, parseCategory } from "./categoryHelpers";
import { formatTime, getProgramProgress } from "./epgHelpers";

export interface FeaturedSlide {
  channel: Channel;
  /** Current programme on the channel. Optional — when the EPG feed is
   *  empty the hero still renders with just the channel branding. */
  program?: EPGProgram | null;
}

interface FeaturedHeroProps {
  slides: FeaturedSlide[];
  onWatch: (channel: Channel) => void;
  /** Dwell time per slide, in ms. Default 7 s mirrors the pacing of
   *  Netflix / Movistar+ featured carousels. */
  intervalMs?: number;
}

/**
 * Rotating landing banner that previews a handful of channels. When the
 * EPG feed provides a "now airing" programme, we render the synopsis and
 * progress bar; otherwise we fall back to channel branding only so the
 * hero still feels alive even if the iptv-org feed ships no EPG.
 */
export function FeaturedHero({
  slides,
  onWatch,
  intervalMs = 7000,
}: FeaturedHeroProps) {
  const { t } = useTranslation();
  const [index, setIndex] = useState(0);
  const [paused, setPaused] = useState(false);
  const [, setNow] = useState(() => Date.now());

  // Clamp the index so a shrinking slide set can't leave us pointing past
  // the end (e.g. a programme just ended, dropping it out of the set).
  const safeIndex = slides.length > 0 ? index % slides.length : 0;

  useEffect(() => {
    if (slides.length <= 1 || paused) return;
    const id = window.setInterval(() => {
      setIndex((i) => (i + 1) % slides.length);
    }, intervalMs);
    return () => window.clearInterval(id);
  }, [slides.length, paused, intervalMs]);

  // Keep the progress bar current so the hero feels live; we only care
  // about re-rendering on each tick, not about the ms value itself.
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 30_000);
    return () => window.clearInterval(id);
  }, []);

  const active = slides[safeIndex];
  const meta = useMemo(() => {
    if (!active) return null;
    const parsed = parseCategory(active.channel.group);
    return {
      parsed,
      cat: categoryMeta(active.program?.category ?? parsed.primary),
    };
  }, [active]);

  if (!active || !meta) return null;
  const progress = active.program ? getProgramProgress(active.program) : 0;
  const hasProgram = !!active.program;

  return (
    <div
      className={[
        "relative overflow-hidden rounded-3xl border border-white/10 min-h-[220px] md:min-h-[260px]",
        meta.cat.tint,
      ].join(" ")}
      onMouseEnter={() => setPaused(true)}
      onMouseLeave={() => setPaused(false)}
      onFocus={() => setPaused(true)}
      onBlur={() => setPaused(false)}
      role="region"
      aria-roledescription="carousel"
      aria-label={t("liveTV.featured")}
    >
      <div
        className="pointer-events-none absolute inset-0 opacity-30"
        style={{
          background:
            "radial-gradient(ellipse at 80% 30%, rgba(255,255,255,0.20), transparent 55%), radial-gradient(ellipse at 20% 80%, rgba(0,0,0,0.40), transparent 55%)",
        }}
        aria-hidden="true"
      />

      <div className="relative flex flex-col gap-4 p-5 md:flex-row md:items-center md:gap-6 md:p-7">
        {/* Channel logo slab */}
        <div className="flex shrink-0 items-center gap-3 md:gap-4">
          <div className="flex h-24 w-24 shrink-0 items-center justify-center rounded-2xl bg-black/35 shadow-lg backdrop-blur-sm md:h-28 md:w-28">
            <ChannelLogo
              logoUrl={active.channel.logo_url}
              number={active.channel.number}
              name={active.channel.name}
              sizeClassName="w-16 h-16 md:w-20 md:h-20"
              fallbackTextClassName="text-2xl md:text-3xl font-bold"
            />
          </div>
        </div>

        {/* Text block */}
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2 text-[11px] font-semibold uppercase tracking-wider text-white/80">
            {hasProgram ? (
              <span className="flex items-center gap-1 rounded-md bg-live/90 px-1.5 py-0.5 text-white shadow-sm">
                <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-white" />
                {t("liveTV.live")}
              </span>
            ) : (
              <span className="rounded-md border border-white/20 bg-black/30 px-1.5 py-0.5 text-white/80">
                {t("liveTV.featured")}
              </span>
            )}
            <span className="tabular-nums">CH.{active.channel.number}</span>
            <span aria-hidden="true">·</span>
            <span className="truncate">{active.channel.name}</span>
            <span aria-hidden="true">·</span>
            <span className="inline-flex items-center gap-1">
              <span aria-hidden="true">{meta.cat.icon}</span>
              <span className="truncate">{meta.parsed.primary}</span>
            </span>
          </div>

          <h2 className="mt-2 line-clamp-2 text-xl font-bold text-text-primary md:text-2xl">
            {active.program?.title ?? active.channel.name}
          </h2>

          {hasProgram ? (
            <>
              <p className="mt-1 text-xs tabular-nums text-text-secondary md:text-sm">
                {formatTime(active.program!.start_time)} —{" "}
                {formatTime(active.program!.end_time)}
              </p>

              {active.program!.description && (
                <p className="mt-2 line-clamp-2 text-sm text-text-secondary md:text-base">
                  {active.program!.description}
                </p>
              )}

              <div className="mt-3 flex items-center gap-2">
                <div
                  className="h-1 max-w-xs flex-1 overflow-hidden rounded-full bg-white/10"
                  role="progressbar"
                  aria-valuemin={0}
                  aria-valuemax={100}
                  aria-valuenow={Math.round(progress)}
                >
                  <div
                    className="h-full rounded-full bg-gradient-to-r from-accent-light to-accent transition-all duration-1000"
                    style={{ width: `${progress}%` }}
                  />
                </div>
                <span className="text-[10px] tabular-nums text-text-muted">
                  {Math.round(progress)}%
                </span>
              </div>
            </>
          ) : (
            <p className="mt-2 line-clamp-2 text-sm text-text-secondary md:text-base">
              {t("liveTV.featuredFallbackDescription")}
            </p>
          )}

          <div className="mt-4 flex items-center gap-2">
            <button
              type="button"
              onClick={() => onWatch(active.channel)}
              className="inline-flex items-center gap-1.5 rounded-lg bg-accent px-4 py-2 text-sm font-semibold text-white shadow-md shadow-accent/20 transition-colors hover:bg-accent-hover"
            >
              <svg
                width="14"
                height="14"
                viewBox="0 0 24 24"
                fill="currentColor"
                aria-hidden="true"
              >
                <path d="M8 5v14l11-7z" />
              </svg>
              {t("liveTV.watchChannel")}
            </button>
          </div>
        </div>
      </div>

      {/* Slide dots */}
      {slides.length > 1 && (
        <div className="relative mx-auto flex items-center justify-center gap-1.5 pb-3">
          {slides.map((s, i) => (
            <button
              key={s.channel.id}
              type="button"
              onClick={() => setIndex(i)}
              aria-label={`${t("liveTV.goToSlide")} ${i + 1}`}
              aria-current={i === safeIndex}
              className={[
                "h-1.5 rounded-full transition-all",
                i === safeIndex
                  ? "w-6 bg-white/90"
                  : "w-1.5 bg-white/30 hover:bg-white/50",
              ].join(" ")}
            />
          ))}
        </div>
      )}
    </div>
  );
}
