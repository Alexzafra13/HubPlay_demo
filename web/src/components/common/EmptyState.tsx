import type { FC, ReactNode } from "react";

interface EmptyStateProps {
  icon?: ReactNode;
  title: string;
  description?: string;
  action?: ReactNode;
}

const EmptyState: FC<EmptyStateProps> = ({
  icon,
  title,
  description,
  action,
}) => {
  return (
    <div className="flex flex-col items-center justify-center py-16 px-4 text-center">
      {icon && (
        <div className="mb-4 text-text-muted [&>svg]:h-12 [&>svg]:w-12">
          {icon}
        </div>
      )}

      <h3 className="text-lg font-semibold text-text-secondary">{title}</h3>

      {description && (
        <p className="mt-1.5 max-w-sm text-sm text-text-muted">{description}</p>
      )}

      {action && <div className="mt-6">{action}</div>}
    </div>
  );
};

export { EmptyState };
export type { EmptyStateProps };
