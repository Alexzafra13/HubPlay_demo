import type { FC, ReactNode } from "react";

type BadgeVariant = "default" | "success" | "warning" | "error" | "live";

interface BadgeProps {
  variant?: BadgeVariant;
  children: ReactNode;
  className?: string;
}

const variantStyles: Record<BadgeVariant, string> = {
  default: "bg-accent-soft text-accent-light",
  success: "bg-success/10 text-success",
  warning: "bg-warning/10 text-warning",
  error: "bg-error/10 text-error",
  live: "bg-live/10 text-live",
};

const Badge: FC<BadgeProps> = ({
  variant = "default",
  children,
  className = "",
}) => {
  return (
    <span
      className={[
        "inline-flex items-center gap-1.5 px-2 py-0.5 text-xs font-medium",
        "rounded-[--radius-sm]",
        variantStyles[variant],
        className,
      ]
        .filter(Boolean)
        .join(" ")}
    >
      {variant === "live" && (
        <span className="relative flex h-2 w-2">
          <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-live opacity-75" />
          <span className="relative inline-flex h-2 w-2 rounded-full bg-live" />
        </span>
      )}
      {children}
    </span>
  );
};

export { Badge };
export type { BadgeProps, BadgeVariant };
