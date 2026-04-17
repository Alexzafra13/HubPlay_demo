import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { Channel, EPGProgram } from "@/api/types";
import { ChannelLogo } from "./ChannelLogo";
import { categoryMeta, parseCategory } from "./categoryHelpers";
import { getNowPlaying } from "./epgHelpers";
import { ProgramListItem } from "./ProgramListItem";
import { ProgramDetailPopover } from "./ProgramDetailPopover";

interface ChannelDetailPanelProps {
  channel: Channel;
  programs: EPGProgram[] | undefined;
  isFavorite: boolean;
  onToggleFavorite: () => void;
}

/**
 * Right-hand pane in the WatchingView: shows the channel's full daily
 * schedule with a rich header (logo, category, favourite toggle) on top.
 * Inspired by the per-channel screen in Movistar+ / TDT Channels, where
 * the user can scroll upcoming programmes without leaving the stream.
 *
 * The schedule auto-scrolls the current programme into view so the
 * "what's on now" anchor is immediately visible on mount.
 */
export function ChannelDetailPanel({
  channel,
  programs,
  isFavorite,
  onToggleFavorite,
}: ChannelDetailPanelProps) {
  const { t } = useTranslation();
  const parsed = parseCategory(channel.group);
  const meta = categoryMeta(parsed.primary);
  const [now, setNow] = useState(() => Date.now());
  const [selected, setSelected] = useState<EPGProgram | null>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const activeItemRef = useRef<HTMLDivElement>(null);

  // Re-tick every 30s so "airing now" highlights stay accurate.
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 30_000);
    return () => window.clearInterval(id);
  }, []);

  // Sort chronologically — the backend generally returns sorted data but
  // we defensively sort so our "airing now" detection can't be fooled.
  const sortedPrograms = useMemo(() => {
    if (!programs || programs.length === 0) return [];
    return [...programs].sort(
      (a, b) =>
        new Date(a.start_time).getTime() - new Date(b.start_time).getTime(),
    );
  }, [programs]);

  const airingNow = useMemo(
    () => getNowPlaying(sortedPrograms),
    [sortedPrograms],
  );

  // Auto-scroll to the currently-airing programme on mount and when the
  // channel changes. Uses `block: "start"` so the live row anchors to the
  // top of the scroller rather than centring (which loses upcoming context).
  useEffect(() => {
    const el = activeItemRef.current;
    const scroller = listRef.current;
    if (!el || !scroller) return;
    const top = el.offsetTop - 8;
    scroller.scrollTo({ top, behavior: "smooth" });
  }, [channel.id, airingNow?.id]);

  const favoriteLabel = isFavorite
    ? t("liveTV.removeFavorite")
    : t("liveTV.addFavorite");

  return (
    <div className="flex h-full min-h-0 flex-col gap-3">
      {/* ── Header ─────────────────────────────────────────────── */}
      <div
        className={[
          "relative overflow-hidden rounded-2xl border border-white/10 p-4",
          meta.tint,
        ].join(" ")}
      >
        <div
          className="pointer-events-none absolute inset-0 opacity-60"
          style={{
            background:
              "radial-gradient(ellipse at top right, rgba(255,255,255,0.08), transparent 60%)",
          }}
          aria-hidden="true"
        />
        <div className="relative flex items-center gap-3">
          <div className="flex h-14 w-14 shrink-0 items-center justify-center rounded-xl bg-black/30 backdrop-blur-sm">
            <ChannelLogo
              logoUrl={channel.logo_url}
              number={channel.number}
              name={channel.name}
              sizeClassName="w-10 h-10"
              fallbackTextClassName="text-base font-bold"
            />
          </div>
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2 text-[11px] font-semibold uppercase tracking-wider text-white/70">
              <span>CH.{channel.number}</span>
              <span aria-hidden="true">·</span>
              <span className="inline-flex items-center gap-1">
                <span aria-hidden="true">{meta.icon}</span>
                <span className="truncate">{parsed.primary}</span>
              </span>
            </div>
            <h2 className="mt-1 truncate text-base font-bold text-text-primary md:text-lg">
              {channel.name}
            </h2>
          </div>
          <button
            type="button"
            onClick={onToggleFavorite}
            aria-label={favoriteLabel}
            aria-pressed={isFavorite}
            className={[
              "inline-flex shrink-0 items-center justify-center rounded-full p-2 transition-all",
              isFavorite
                ? "bg-rose-500/90 text-white shadow-md shadow-rose-500/30 hover:bg-rose-500"
                : "bg-black/40 text-white/80 backdrop-blur-sm hover:bg-black/60 hover:text-white",
            ].join(" ")}
          >
            <svg
              width="16"
              height="16"
              viewBox="0 0 24 24"
              fill={isFavorite ? "currentColor" : "none"}
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
              aria-hidden="true"
            >
              <path d="M20.84 4.61a5.5 5.5 0 0 0-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 0 0-7.78 7.78l1.06 1.06L12 21.23l7.78-7.78 1.06-1.06a5.5 5.5 0 0 0 0-7.78z" />
            </svg>
          </button>
        </div>
      </div>

      {/* ── Schedule heading ───────────────────────────────────── */}
      <div className="flex items-baseline justify-between px-1">
        <h3 className="text-sm font-bold uppercase tracking-wide text-text-secondary">
          {t("liveTV.schedule")}
        </h3>
        {sortedPrograms.length > 0 && (
          <span className="text-[11px] tabular-nums text-text-muted">
            {sortedPrograms.length} {t("liveTV.programsCount")}
          </span>
        )}
      </div>

      {/* ── Schedule list ──────────────────────────────────────── */}
      <div
        ref={listRef}
        className="scrollbar-hide flex-1 overflow-y-auto rounded-xl border border-white/5 bg-white/[0.02] p-2"
      >
        {sortedPrograms.length === 0 ? (
          <div className="flex h-full items-center justify-center px-4 py-12 text-center text-sm italic text-text-muted">
            {t("liveTV.noEPG")}
          </div>
        ) : (
          <div className="flex flex-col gap-1.5">
            {sortedPrograms.map((p) => {
              const isActive = airingNow?.id === p.id;
              return (
                <div key={p.id || p.start_time} ref={isActive ? activeItemRef : null}>
                  <ProgramListItem
                    program={p}
                    now={now}
                    fallbackCategory={parsed.primary}
                    onClick={() => setSelected(p)}
                  />
                </div>
              );
            })}
          </div>
        )}
      </div>

      {selected && (
        <ProgramDetailPopover
          program={selected}
          channel={channel}
          now={now}
          onClose={() => setSelected(null)}
          onWatch={() => setSelected(null)}
        />
      )}
    </div>
  );
}
