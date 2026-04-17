import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import type { Channel, EPGProgram } from "@/api/types";
import { ChannelLogo } from "./ChannelLogo";
import { categoryMeta, parseCategory } from "./categoryHelpers";
import { formatTime } from "./epgHelpers";

interface ProgramDetailPopoverProps {
  program: EPGProgram;
  channel: Channel;
  /** Current wall-clock time (ms). Passed from the parent so the popover
   *  stays pure during render (React Compiler friendly). */
  now: number;
  onClose: () => void;
  onWatch: () => void;
}

/**
 * Modal dialog describing a single EPG programme. Opens from the EPG grid
 * and from the channel-detail vertical schedule. Extracted from EPGGrid so
 * both call sites reuse the same UI.
 */
export function ProgramDetailPopover({
  program,
  channel,
  now,
  onClose,
  onWatch,
}: ProgramDetailPopoverProps) {
  const { t } = useTranslation();
  const chCategory = parseCategory(channel.group);
  const progMeta = categoryMeta(program.category ?? chCategory.primary);
  const start = new Date(program.start_time).getTime();
  const end = new Date(program.end_time).getTime();
  const airing = start <= now && end > now;

  // Close on Escape — standard dialog affordance.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") onClose();
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <div
      className="fixed inset-0 z-50 flex items-end justify-center bg-black/70 p-4 backdrop-blur-sm md:items-center"
      role="dialog"
      aria-modal="true"
      aria-labelledby="program-detail-title"
      onClick={onClose}
    >
      <div
        className="w-full max-w-xl overflow-hidden rounded-2xl border border-white/10 bg-bg-card shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div
          className={[
            "relative flex items-start gap-3 border-b border-white/10 p-5",
            progMeta.tint,
          ].join(" ")}
        >
          <div className="flex h-14 w-14 shrink-0 items-center justify-center rounded-xl bg-black/20 backdrop-blur-sm">
            <ChannelLogo
              logoUrl={channel.logo_url}
              number={channel.number}
              name={channel.name}
              sizeClassName="w-11 h-11"
              fallbackTextClassName="text-base font-bold"
            />
          </div>
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2 text-[11px] font-semibold uppercase tracking-wider text-white/70">
              <span>CH.{channel.number}</span>
              <span aria-hidden="true">·</span>
              <span className="truncate">{channel.name}</span>
              {airing && (
                <span className="flex items-center gap-1 rounded-md bg-live/90 px-1.5 py-0.5 text-[10px] text-white shadow-sm">
                  <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-white" />
                  {t("liveTV.live")}
                </span>
              )}
            </div>
            <h3
              id="program-detail-title"
              className="mt-1 text-lg font-bold text-text-primary md:text-xl"
            >
              {program.title}
            </h3>
            <div className="mt-1 flex items-center gap-2 text-xs text-text-secondary">
              <span className="tabular-nums">
                {formatTime(program.start_time)} — {formatTime(program.end_time)}
              </span>
              {program.category && (
                <>
                  <span aria-hidden="true">·</span>
                  <span className="inline-flex items-center gap-1">
                    <span aria-hidden="true">{progMeta.icon}</span>
                    {program.category}
                  </span>
                </>
              )}
            </div>
          </div>
          <button
            type="button"
            onClick={onClose}
            aria-label={t("common.close")}
            className="shrink-0 rounded-lg p-1.5 text-white/70 transition-colors hover:bg-black/30 hover:text-white"
          >
            <svg
              width="18"
              height="18"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
              aria-hidden="true"
            >
              <path d="M18 6L6 18M6 6l12 12" />
            </svg>
          </button>
        </div>

        <div className="p-5">
          {program.description ? (
            <p className="text-sm leading-relaxed text-text-secondary">
              {program.description}
            </p>
          ) : (
            <p className="text-sm italic text-text-muted">
              {t("liveTV.noDescription")}
            </p>
          )}

          <div className="mt-5 flex items-center justify-end gap-2">
            <button
              type="button"
              onClick={onClose}
              className="rounded-lg border border-white/10 px-4 py-2 text-sm font-medium text-text-secondary transition-colors hover:bg-white/5 hover:text-text-primary"
            >
              {t("common.close")}
            </button>
            <button
              type="button"
              onClick={onWatch}
              className="inline-flex items-center gap-1.5 rounded-lg bg-accent px-4 py-2 text-sm font-semibold text-white shadow-sm shadow-accent/20 transition-colors hover:bg-accent-hover"
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
    </div>
  );
}
