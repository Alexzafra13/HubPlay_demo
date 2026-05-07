import type { FC, ReactNode } from "react";

type BadgeVariant = "default" | "success" | "warning" | "error" | "live";

interface BadgeProps {
  variant?: BadgeVariant;
  children: ReactNode;
  className?: string;
}

// `default` is intentionally neutral so the brand accent stays
// reserved for primary CTAs (Button), identity (BrandWordmark, sidebar
// active indicator) and active interactive state. Decorative metadata
// chips (genres, content-type, content-rating, "default" markers)
// previously inherited the accent and competed with the real CTAs
// for attention; rendering them in a quiet bg-elevated/text-secondary
// pair lets the eye land on action surfaces first. Status variants
// (success/warning/error/live) keep their semantic colour.
const variantStyles: Record<BadgeVariant, string> = {
  default: "bg-bg-elevated text-text-secondary",
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
