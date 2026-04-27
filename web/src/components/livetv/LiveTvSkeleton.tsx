/**
 * LiveTvSkeleton — placeholder paint for the LiveTV page while channels
 * + EPG are loading. Mirrors the final layout (hero block → chip bar →
 * a couple of rails) so the user perceives progressive load instead of
 * a centred Spinner that hides the page chrome until everything is
 * ready and snaps in all at once.
 *
 * Uses the global `.animate-shimmer` utility (defined in
 * styles/globals.css) — a 90° gradient that sweeps across the box via
 * a 1.5 s background-position animation. The gradient bakes its own
 * colours from the bg-card / bg-elevated tokens so the shimmer stays
 * subtle on the dark theme. Reduced-motion users get the static
 * gradient stripe (no animation) via the global `motion-reduce`
 * variant on the parent container.
 */
function ShimmerBox({ className = "" }: { className?: string }) {
  return (
    <div
      aria-hidden="true"
      className={["rounded-tv-md animate-shimmer", className].join(" ")}
    />
  );
}

function ChannelCardSkeleton() {
  return (
    <div className="flex w-[260px] shrink-0 flex-col gap-2">
      <ShimmerBox className="aspect-[16/9] w-full" />
      <ShimmerBox className="h-3 w-3/4" />
      <ShimmerBox className="h-2.5 w-1/2" />
    </div>
  );
}

function RailSkeleton() {
  return (
    <div className="flex flex-col gap-3">
      <ShimmerBox className="h-4 w-40" />
      <div className="flex gap-3 overflow-hidden">
        {Array.from({ length: 6 }).map((_, i) => (
          <ChannelCardSkeleton key={i} />
        ))}
      </div>
    </div>
  );
}

export function LiveTvSkeleton() {
  return (
    <section
      data-theme="tv"
      data-accent="lime"
      aria-busy="true"
      aria-live="polite"
      className="-mx-4 flex flex-col gap-6 px-4 pb-10 pt-3 md:-mx-6 md:px-6 motion-reduce:[&_.animate-shimmer]:animate-none"
    >
      {/* Hero placeholder — same aspect ratio as the real card so
          there's no layout shift on swap. */}
      <ShimmerBox className="aspect-[21/9] w-full max-h-[420px] md:aspect-[32/9]" />

      {/* Chip strip placeholder. */}
      <div className="flex gap-2 overflow-hidden">
        {Array.from({ length: 8 }).map((_, i) => (
          <ShimmerBox key={i} className="h-7 w-20 rounded-full" />
        ))}
      </div>

      {/* Two rails — enough to fill the fold without claiming a
          precise channel count. */}
      <RailSkeleton />
      <RailSkeleton />
    </section>
  );
}
