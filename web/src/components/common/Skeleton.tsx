import type { FC, CSSProperties } from "react";

type SkeletonVariant = "text" | "circular" | "rectangular";

interface SkeletonProps {
  className?: string;
  variant?: SkeletonVariant;
  width?: string | number;
  height?: string | number;
}

const variantStyles: Record<SkeletonVariant, string> = {
  text: "rounded-[--radius-sm] h-4",
  circular: "rounded-full",
  rectangular: "rounded-[--radius-md]",
};

const Skeleton: FC<SkeletonProps> = ({
  className = "",
  variant = "text",
  width,
  height,
}) => {
  const style: CSSProperties = {};
  if (width != null) style.width = typeof width === "number" ? `${width}px` : width;
  if (height != null) style.height = typeof height === "number" ? `${height}px` : height;
  if (variant === "circular" && width != null && height == null) {
    style.height = style.width;
  }

  return (
    <div
      className={`animate-shimmer ${variantStyles[variant]} ${className}`}
      style={style}
      aria-hidden="true"
    />
  );
};

export { Skeleton };
export type { SkeletonProps, SkeletonVariant };
