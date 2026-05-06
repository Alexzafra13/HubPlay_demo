import { useEffect, useState, type FC } from "react";
import { useTranslation } from "react-i18next";

interface TimeDisplayProps {
  currentTime: number;
  duration: number;
}

function formatTime(totalSeconds: number): string {
  const s = Math.max(0, Math.floor(totalSeconds));
  const hours = Math.floor(s / 3600);
  const minutes = Math.floor((s % 3600) / 60);
  const seconds = s % 60;

  const mm = String(minutes).padStart(2, "0");
  const ss = String(seconds).padStart(2, "0");

  if (hours > 0) {
    return `${hours}:${mm}:${ss}`;
  }
  return `${minutes}:${ss}`;
}

// Local-time HH:MM for the "ends at" badge. Browser locale picks
// 12h vs 24h automatically.
function formatClock(date: Date, locale: string): string {
  return date.toLocaleTimeString(locale, {
    hour: "2-digit",
    minute: "2-digit",
  });
}

const TimeDisplay: FC<TimeDisplayProps> = ({ currentTime, duration }) => {
  const { i18n, t } = useTranslation();

  // Current wall-clock — refreshed every 30s so the "ends at" badge
  // doesn't drift if the user pauses for a while. We don't tick per
  // second because the badge text only changes once a minute.
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 30_000);
    return () => window.clearInterval(id);
  }, []);

  const remaining = Math.max(0, duration - currentTime);
  const showEndsAt = duration > 0 && remaining > 60;
  const endsAtLabel = showEndsAt
    ? t("playerControls.endsAt", {
        defaultValue: "Termina a las {{time}}",
        time: formatClock(new Date(now + remaining * 1000), i18n.language),
      })
    : null;

  return (
    <span className="flex items-center gap-2 whitespace-nowrap select-none">
      <span className="text-xs text-white/90 tabular-nums">
        {formatTime(currentTime)}
        <span className="text-white/50"> / </span>
        {formatTime(duration)}
      </span>
      {endsAtLabel && (
        <span
          className="hidden sm:inline text-[11px] text-white/55 tabular-nums"
          title={endsAtLabel}
        >
          · {endsAtLabel}
        </span>
      )}
    </span>
  );
};

export { TimeDisplay };
export type { TimeDisplayProps };
