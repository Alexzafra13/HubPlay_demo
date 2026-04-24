import { useTranslation } from "react-i18next";
import type { Channel, EPGProgram } from "@/api/types";
import { Button, Modal } from "@/components/common";
import { useNowTick } from "@/hooks/useNowTick";
import { capitalize, formatTime } from "./epgHelpers";

interface ProgramDetailModalProps {
  /**
   * Open state lives in the parent so the same modal instance can
   * fade content as the user clicks through different programmes.
   * Setting `program` to null with `isOpen` still true would render
   * an empty dialog — the modal short-circuits in that case.
   */
  isOpen: boolean;
  onClose: () => void;
  program: EPGProgram | null;
  channel: Channel | null;
  /**
   * Up-next entries on the same channel — already filtered + sliced
   * by the parent. Showing 0–3 keeps the modal short on mobile and
   * gives the viewer enough lookahead to decide whether to wait or
   * zap. The parent computes this via the existing schedule cache.
   */
  upNext: EPGProgram[];
  /**
   * Fired when the user clicks "Ver canal ahora". The parent is
   * responsible for closing the modal AND opening the player —
   * keeping both in one callback avoids a flicker between the two
   * transitions.
   */
  onWatch: () => void;
}

/**
 * ProgramDetailModal — programme detail card opened from EPGGrid.
 *
 * Shows everything the EPG row could only hint at (sinopsis, full
 * time range, duration, category, what's coming up next on the same
 * channel) and offers a single primary action: jump to the channel.
 * No edit / record / remind affordances yet — those are post-MVP.
 *
 * Accessibility: the underlying Modal already handles role="dialog",
 * Escape, body scroll lock and backdrop click. Title goes via the
 * Modal title prop so screen readers announce the programme name.
 */
export function ProgramDetailModal({
  isOpen,
  onClose,
  program,
  channel,
  upNext,
  onWatch,
}: ProgramDetailModalProps) {
  const { t } = useTranslation();
  // Per-minute tick is enough — live/past only flips at programme
  // boundaries. Using useNowTick keeps render pure (no Date.now()
  // call directly) which the Compiler-purity lint requires.
  const now = useNowTick(60_000);
  if (!program || !channel) return null;

  const start = new Date(program.start_time).getTime();
  const end = new Date(program.end_time).getTime();
  const isLive = start <= now && end > now;
  const isPast = end <= now;
  const durationMin = Math.max(1, Math.round((end - start) / 60000));

  return (
    <Modal isOpen={isOpen} onClose={onClose} title={program.title} size="lg">
      <div className="space-y-4">
        {/* Channel + time strip */}
        <div className="flex flex-wrap items-center gap-2 text-sm text-text-secondary">
          <span className="font-medium text-text-primary">{channel.name}</span>
          <span className="text-text-muted">·</span>
          <span className="tabular-nums">
            {formatTime(program.start_time)} – {formatTime(program.end_time)}
          </span>
          <span className="text-text-muted">·</span>
          <span className="tabular-nums">{durationMin} min</span>
          {program.category ? (
            <>
              <span className="text-text-muted">·</span>
              <span className="rounded bg-bg-elevated px-1.5 py-0.5 text-[11px] uppercase tracking-wider text-text-secondary">
                {capitalize(program.category)}
              </span>
            </>
          ) : null}
          {isLive ? (
            <span className="rounded bg-error/15 px-1.5 py-0.5 text-[11px] font-bold uppercase text-error">
              {t("liveTV.live", { defaultValue: "EN VIVO" })}
            </span>
          ) : null}
        </div>

        {/* Description */}
        {program.description ? (
          <p className="whitespace-pre-line text-sm leading-relaxed text-text-primary">
            {program.description}
          </p>
        ) : (
          <p className="text-sm italic text-text-muted">
            {t("liveTV.noDescription", {
              defaultValue: "Sin sinopsis disponible.",
            })}
          </p>
        )}

        {/* Up-next list */}
        {upNext.length > 0 ? (
          <div className="space-y-2 border-t border-border pt-4">
            <h3 className="text-xs font-semibold uppercase tracking-wider text-text-secondary">
              {t("liveTV.upNext", { defaultValue: "A continuación" })}
            </h3>
            <ul className="space-y-2">
              {upNext.map((p) => (
                <li
                  key={p.id || p.start_time}
                  className="flex items-baseline gap-3 text-sm"
                >
                  <span className="w-12 shrink-0 tabular-nums text-text-muted">
                    {formatTime(p.start_time)}
                  </span>
                  <span className="min-w-0 truncate text-text-primary">
                    {p.title}
                  </span>
                </li>
              ))}
            </ul>
          </div>
        ) : null}

        {/* Actions */}
        <div className="flex justify-end gap-2 border-t border-border pt-4">
          <Button variant="ghost" onClick={onClose}>
            {t("common.close", { defaultValue: "Cerrar" })}
          </Button>
          <Button variant="primary" onClick={onWatch} disabled={isPast}>
            {isPast
              ? t("liveTV.programEnded", {
                  defaultValue: "Ya terminó",
                })
              : t("liveTV.watchChannelNow", {
                  defaultValue: "Ver canal ahora",
                })}
          </Button>
        </div>
      </div>
    </Modal>
  );
}
