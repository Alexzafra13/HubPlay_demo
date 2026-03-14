import { Link } from "react-router";
import { useContinueWatching, useLatestItems, useNextUp } from "@/api/hooks";
import type { MediaItem } from "@/api/types";
import { useAuthStore } from "@/store/auth";
import { Skeleton } from "@/components/common";
import { PosterCard, EpisodeCard } from "@/components/media";

function ScrollRow({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex gap-4 overflow-x-auto pb-2 scrollbar-thin scrollbar-track-transparent scrollbar-thumb-border">
      {children}
    </div>
  );
}

function SkeletonRow() {
  return (
    <div className="flex gap-4">
      {Array.from({ length: 6 }, (_, i) => (
        <div key={i} className="w-[150px] shrink-0">
          <Skeleton
            variant="rectangular"
            className="aspect-[2/3] w-full rounded-[--radius-lg]"
          />
          <Skeleton variant="text" width="80%" className="mt-2" />
        </div>
      ))}
    </div>
  );
}

function EpisodeSkeletonRow() {
  return (
    <div className="flex gap-4">
      {Array.from({ length: 4 }, (_, i) => (
        <div key={i} className="w-[280px] shrink-0">
          <Skeleton
            variant="rectangular"
            className="aspect-video w-full rounded-[--radius-lg]"
          />
          <Skeleton variant="text" width="70%" className="mt-2" />
          <Skeleton variant="text" width="40%" className="mt-1" />
        </div>
      ))}
    </div>
  );
}

interface SectionProps {
  title: string;
  linkTo?: string;
  children: React.ReactNode;
}

function Section({ title, linkTo, children }: SectionProps) {
  return (
    <section className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <h2 className="text-xl font-semibold text-text-primary">{title}</h2>
        {linkTo && (
          <Link
            to={linkTo}
            className="text-sm text-accent hover:text-accent-hover transition-colors"
          >
            See All
          </Link>
        )}
      </div>
      {children}
    </section>
  );
}

export default function Home() {
  const user = useAuthStore((s) => s.user);
  const continueWatching = useContinueWatching();
  const latestItems = useLatestItems();
  const nextUp = useNextUp();

  const continueItems = continueWatching.data ?? [];
  const latestList = latestItems.data ?? [];
  const nextUpList = nextUp.data ?? [];

  return (
    <div className="flex flex-col gap-10 px-6 py-8 sm:px-10">
      <h1 className="text-2xl font-bold text-text-primary sm:text-3xl">
        Welcome back, {user?.display_name ?? user?.username ?? "User"}
      </h1>

      {/* Continue Watching */}
      {(continueWatching.isLoading || continueItems.length > 0) && (
        <Section title="Continue Watching">
          {continueWatching.isLoading ? (
            <SkeletonRow />
          ) : (
            <ScrollRow>
              {continueItems.map((item: MediaItem) => (
                <div key={item.id} className="w-[150px] shrink-0">
                  <PosterCard item={item} progress={50} />
                </div>
              ))}
            </ScrollRow>
          )}
        </Section>
      )}

      {/* Recently Added */}
      {(latestItems.isLoading || latestList.length > 0) && (
        <Section title="Recently Added">
          {latestItems.isLoading ? (
            <SkeletonRow />
          ) : (
            <ScrollRow>
              {latestList.map((item: MediaItem) => (
                <div key={item.id} className="w-[150px] shrink-0">
                  <PosterCard item={item} />
                </div>
              ))}
            </ScrollRow>
          )}
        </Section>
      )}

      {/* Next Up */}
      {(nextUp.isLoading || nextUpList.length > 0) && (
        <Section title="Next Up" linkTo="/series">
          {nextUp.isLoading ? (
            <EpisodeSkeletonRow />
          ) : (
            <ScrollRow>
              {nextUpList.map((item: MediaItem) => (
                <div key={item.id} className="w-[280px] shrink-0">
                  <EpisodeCard item={item} />
                </div>
              ))}
            </ScrollRow>
          )}
        </Section>
      )}
    </div>
  );
}
