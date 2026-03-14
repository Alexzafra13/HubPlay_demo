import type { FC } from "react";
import type { MediaItem } from "@/api/types";
import { Skeleton } from "@/components/common/Skeleton";
import { EmptyState } from "@/components/common/EmptyState";
import { PosterCard } from "./PosterCard";

interface MediaGridProps {
  items: MediaItem[];
  loading: boolean;
  emptyMessage?: string;
}

const SKELETON_COUNT = 8;

const MediaGrid: FC<MediaGridProps> = ({
  items,
  loading,
  emptyMessage = "No items found",
}) => {
  if (loading) {
    return (
      <div className="grid grid-cols-[repeat(auto-fill,minmax(150px,1fr))] gap-4">
        {Array.from({ length: SKELETON_COUNT }, (_, i) => (
          <div key={i} className="flex flex-col gap-2">
            <Skeleton
              variant="rectangular"
              className="aspect-[2/3] w-full rounded-[--radius-lg]"
            />
            <Skeleton variant="text" width="80%" />
            <Skeleton variant="text" width="40%" />
          </div>
        ))}
      </div>
    );
  }

  if (items.length === 0) {
    return (
      <EmptyState
        title={emptyMessage}
        icon={
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.5}>
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              d="M7 4v16m10-16v16M3 8h4m10 0h4M3 12h18M3 16h4m10 0h4M4 20h16a1 1 0 001-1V5a1 1 0 00-1-1H4a1 1 0 00-1 1v14a1 1 0 001 1z"
            />
          </svg>
        }
      />
    );
  }

  return (
    <div className="grid grid-cols-[repeat(auto-fill,minmax(150px,1fr))] gap-4">
      {items.map((item) => (
        <PosterCard key={item.id} item={item} />
      ))}
    </div>
  );
};

export { MediaGrid };
export type { MediaGridProps };
