import type { FC } from "react";

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

const TimeDisplay: FC<TimeDisplayProps> = ({ currentTime, duration }) => {
  return (
    <span className="text-xs text-white/90 tabular-nums whitespace-nowrap select-none">
      {formatTime(currentTime)}
      <span className="text-white/50"> / </span>
      {formatTime(duration)}
    </span>
  );
};

export { TimeDisplay };
export type { TimeDisplayProps };
