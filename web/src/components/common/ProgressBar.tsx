import type { FC } from "react";

type ProgressBarSize = "sm" | "md";

interface ProgressBarProps {
  value: number;
  size?: ProgressBarSize;
  className?: string;
  color?: string;
}

const sizeStyles: Record<ProgressBarSize, string> = {
  sm: "h-1",
  md: "h-2",
};

const ProgressBar: FC<ProgressBarProps> = ({
  value,
  size = "md",
  className = "",
  color,
}) => {
  const clamped = Math.min(100, Math.max(0, value));

  return (
    <div
      className={`w-full rounded-full bg-bg-elevated overflow-hidden ${sizeStyles[size]} ${className}`}
      role="progressbar"
      aria-valuenow={clamped}
      aria-valuemin={0}
      aria-valuemax={100}
    >
      <div
        className={`h-full rounded-full transition-all duration-300 ease-out ${color ?? "bg-accent"}`}
        style={{ width: `${clamped}%` }}
      />
    </div>
  );
};

export { ProgressBar };
export type { ProgressBarProps, ProgressBarSize };
