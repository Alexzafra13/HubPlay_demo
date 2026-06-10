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

// Modo del segundo contador: duración total o tiempo restante.
// Persistido — quien piensa en "cuánto me queda" lo hace siempre.
type TimeMode = "total" | "remaining";
const TIME_MODE_KEY = "hubplay.player.timeMode";

function readTimeMode(): TimeMode {
  try {
    return window.localStorage.getItem(TIME_MODE_KEY) === "remaining"
      ? "remaining"
      : "total";
  } catch {
    return "total";
  }
}

const TimeDisplay: FC<TimeDisplayProps> = ({ currentTime, duration }) => {
  const { i18n, t } = useTranslation();
  const [mode, setMode] = useState<TimeMode>(readTimeMode);

  const toggleMode = () => {
    setMode((m) => {
      const next: TimeMode = m === "total" ? "remaining" : "total";
      try {
        window.localStorage.setItem(TIME_MODE_KEY, next);
      } catch {
        // Storage lleno/privado: el toggle sigue funcionando en sesión.
      }
      return next;
    });
  };

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
      {/* Tocar el contador alterna total ↔ restante (patrón de los
          players nativos de iOS/macOS). El segundo término se re-monta
          con `key` para un fade corto en el cambio. */}
      <button
        type="button"
        onClick={toggleMode}
        aria-label={t("playerControls.timeModeToggle", {
          defaultValue: "Alternar duración total / tiempo restante",
        })}
        className="cursor-pointer rounded-[--radius-sm] px-1 -mx-1 text-xs text-white/90 tabular-nums transition-colors hover:bg-white/10"
      >
        {formatTime(currentTime)}
        <span className="text-white/50"> / </span>
        <span key={mode} className="inline-block animate-[fade-in_160ms_ease-out]">
          {mode === "remaining" ? `−${formatTime(remaining)}` : formatTime(duration)}
        </span>
      </button>
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
