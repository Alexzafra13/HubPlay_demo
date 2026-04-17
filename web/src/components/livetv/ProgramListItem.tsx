import { useTranslation } from "react-i18next";
import type { EPGProgram } from "@/api/types";
import { categoryMeta } from "./categoryHelpers";
import { formatTime, getProgramProgress } from "./epgHelpers";

interface ProgramListItemProps {
  program: EPGProgram;
  /** Current wall-clock time (ms). Parent keeps a ticking value so the
   *  highlight transitions seamlessly when the programme ends. */
  now: number;
  /** Fallback category when the programme itself has no category field. */
  fallbackCategory?: string | null;
  onClick: () => void;
}

/**
 * Vertical row representing a single programme inside a channel's daily
 * schedule. Three visual states:
 *   — past (dimmed)
 *   — airing now (category-tinted accent + live pulse)
 *   — upcoming (default)
 *
 * Thumbnails come from `icon_url` when the EPG feed provides them, with a
 * coloured fallback built from the category palette.
 */
export function ProgramListItem({
  program,
  now,
  fallbackCategory = null,
  onClick,
}: ProgramListItemProps) {
  const { t } = useTranslation();
  const start = new Date(program.start_time).getTime();
  const end = new Date(program.end_time).getTime();
  const airing = start <= now && end > now;
  const past = end <= now;
  const progress = airing ? getProgramProgress(program) : 0;
  const meta = categoryMeta(program.category ?? fallbackCategory ?? "");
  const durationMin = Math.max(1, Math.round((end - start) / 60_000));

  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={`${program.title} ${formatTime(program.start_time)} — ${formatTime(program.end_time)}`}
      className={[
        "group relative flex w-full items-start gap-3 rounded-xl px-3 py-2.5 text-left transition-all",
        airing
          ? "bg-white/[0.06] ring-1 ring-white/10 hover:bg-white/[0.09]"
          : past
            ? "opacity-55 hover:opacity-80 hover:bg-white/[0.03]"
            : "hover:bg-white/[0.05]",
      ].join(" ")}
    >
      {/* ── Time column ────────────────────────────────────────── */}
      <div className="flex w-14 shrink-0 flex-col items-end pt-0.5 text-right tabular-nums">
        <span
          className={[
            "text-sm font-bold leading-none",
            airing ? "text-accent-light" : "text-text-primary",
          ].join(" ")}
        >
          {formatTime(program.start_time)}
        </span>
        <span className="mt-1 text-[10px] text-text-muted">
          {durationMin} min
        </span>
      </div>

      {/* ── Thumbnail ─────────────────────────────────────────── */}
      <div
        className={[
          "relative flex h-14 w-20 shrink-0 items-center justify-center overflow-hidden rounded-lg",
          meta.tint,
        ].join(" ")}
      >
        {program.icon_url ? (
          <img
            src={program.icon_url}
            alt=""
            loading="lazy"
            className="h-full w-full object-cover"
            onError={(e) => {
              // Hide broken images — the tinted backdrop + icon remains.
              (e.currentTarget as HTMLImageElement).style.display = "none";
            }}
          />
        ) : (
          <span
            aria-hidden="true"
            className="text-2xl drop-shadow-[0_2px_6px_rgba(0,0,0,0.4)]"
          >
            {meta.icon}
          </span>
        )}
        {airing && (
          <span className="absolute left-1 top-1 flex items-center gap-0.5 rounded bg-live/90 px-1 py-0.5 text-[9px] font-bold uppercase tracking-wider text-white">
            <span className="h-1 w-1 animate-pulse rounded-full bg-white" />
            {t("liveTV.live")}
          </span>
        )}
      </div>

      {/* ── Text column ───────────────────────────────────────── */}
      <div className="min-w-0 flex-1">
        <h4
          className={[
            "truncate text-sm font-semibold leading-tight",
            airing ? "text-text-primary" : "text-text-secondary",
          ].join(" ")}
        >
          {program.title}
        </h4>
        {program.description && (
          <p className="mt-0.5 line-clamp-2 text-xs text-text-muted">
            {program.description}
          </p>
        )}
        {program.category && (
          <div className="mt-1 flex items-center gap-1 text-[10px] text-text-muted">
            <span aria-hidden="true">{meta.icon}</span>
            <span className="truncate">{program.category}</span>
          </div>
        )}
        {airing && (
          <div
            className="mt-1.5 h-1 overflow-hidden rounded-full bg-white/5"
            role="progressbar"
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={Math.round(progress)}
            aria-label={t("liveTV.programProgress")}
          >
            <div
              className="h-full rounded-full bg-gradient-to-r from-accent-light to-accent transition-all duration-1000"
              style={{ width: `${progress}%` }}
            />
          </div>
        )}
      </div>
    </button>
  );
}
