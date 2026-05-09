import type { FC, ReactNode } from "react";

interface EmptyStateProps {
  icon?: ReactNode;
  title: string;
  description?: string;
  action?: ReactNode;
  /** Wraps the contents in a dashed-border card (rounded-lg
   *  border-dashed). Use for inline empty states sitting inside
   *  another section's flow; without it, the component renders
   *  bare and is meant for full-page empty states. */
  bordered?: boolean;
  /** Tighter vertical rhythm (`py-8` instead of `py-16`) for
   *  contexts where the empty state is one of several panels on
   *  the page rather than the page itself. Pairs naturally with
   *  `bordered`. */
  compact?: boolean;
}

const EmptyState: FC<EmptyStateProps> = ({
  icon,
  title,
  description,
  action,
  bordered = false,
  compact = false,
}) => {
  const wrapper = [
    "flex flex-col items-center justify-center px-4 text-center",
    compact ? "py-8" : "py-16",
    bordered
      ? "rounded-lg border border-dashed border-border bg-bg-elevated"
      : "",
  ]
    .filter(Boolean)
    .join(" ");

  return (
    <div className={wrapper}>
      {icon && (
        <div
          className={[
            "text-text-muted",
            compact ? "mb-2 [&>svg]:h-8 [&>svg]:w-8" : "mb-4 [&>svg]:h-12 [&>svg]:w-12",
          ].join(" ")}
        >
          {icon}
        </div>
      )}

      <h3
        className={[
          compact ? "text-sm font-medium" : "text-lg font-semibold",
          "text-text-secondary",
        ].join(" ")}
      >
        {title}
      </h3>

      {description && (
        <p className="mt-1.5 max-w-sm text-sm text-text-muted">{description}</p>
      )}

      {action && <div className={compact ? "mt-4" : "mt-6"}>{action}</div>}
    </div>
  );
};

export { EmptyState };
export type { EmptyStateProps };
