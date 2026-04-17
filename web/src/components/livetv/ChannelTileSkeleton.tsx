/**
 * Skeleton tile used while the channel list is still loading. Shape mirrors
 * `ChannelCard` in its tile variant so the layout doesn't jump when real
 * data arrives.
 */
export function ChannelTileSkeleton() {
  return (
    <div
      className="flex flex-col overflow-hidden rounded-2xl border border-white/[0.06] bg-gradient-to-b from-white/[0.04] to-white/[0.01]"
      aria-hidden="true"
    >
      <div className="h-24 animate-pulse bg-white/[0.04] sm:h-28" />
      <div className="flex flex-col gap-2 p-3">
        <div className="h-3.5 w-3/4 animate-pulse rounded bg-white/[0.06]" />
        <div className="h-3 w-1/2 animate-pulse rounded bg-white/[0.04]" />
        <div className="mt-1 h-3 w-5/6 animate-pulse rounded bg-white/[0.03]" />
        <div className="h-1 w-full animate-pulse rounded-full bg-white/[0.05]" />
      </div>
    </div>
  );
}
