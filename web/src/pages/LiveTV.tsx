import { useState, useMemo } from "react";
import { useChannels } from "@/api/hooks";
import type { Channel } from "@/api/types";
import { Spinner, EmptyState } from "@/components/common";

export default function LiveTV() {
  const { data: channels, isLoading } = useChannels();
  const [activeChannel, setActiveChannel] = useState<Channel | null>(null);

  const grouped = useMemo(() => {
    if (!channels) return new Map<string, Channel[]>();
    const map = new Map<string, Channel[]>();
    for (const ch of channels) {
      const group = ch.group ?? "Uncategorized";
      const list = map.get(group) ?? [];
      list.push(ch);
      map.set(group, list);
    }
    return map;
  }, [channels]);

  if (isLoading) {
    return (
      <div className="flex min-h-[60vh] items-center justify-center">
        <Spinner size="lg" />
      </div>
    );
  }

  if (!channels || channels.length === 0) {
    return (
      <div className="flex min-h-[60vh] items-center justify-center">
        <EmptyState
          title="No channels available"
          description="Live TV channels will appear here once configured."
          icon={
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.5}>
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                d="M6 20h12M6 4h12M4 8h16v8H4z"
              />
            </svg>
          }
        />
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-6 px-6 py-8 sm:px-10">
      <h1 className="text-2xl font-bold text-text-primary sm:text-3xl">
        Live TV
      </h1>

      {/* Video player */}
      {activeChannel && (
        <div className="flex flex-col gap-2">
          <div className="aspect-video w-full max-w-4xl overflow-hidden rounded-[--radius-lg] bg-black">
            <video
              src={activeChannel.stream_url}
              controls
              autoPlay
              className="h-full w-full"
            >
              Your browser does not support video playback.
            </video>
          </div>
          <div className="flex items-center gap-3">
            <span className="text-sm font-medium text-text-primary">
              {activeChannel.name}
            </span>
            <button
              type="button"
              onClick={() => setActiveChannel(null)}
              className="text-xs text-text-muted hover:text-text-secondary transition-colors"
            >
              Close player
            </button>
          </div>
        </div>
      )}

      {/* Channel groups */}
      {Array.from(grouped.entries()).map(([group, groupChannels]) => (
        <section key={group}>
          <h2 className="mb-4 text-lg font-semibold text-text-primary">
            {group}
          </h2>
          <div className="grid grid-cols-[repeat(auto-fill,minmax(200px,1fr))] gap-3">
            {groupChannels.map((channel) => (
              <button
                key={channel.id}
                type="button"
                onClick={() => setActiveChannel(channel)}
                className={[
                  "flex items-center gap-3 rounded-[--radius-lg] border p-4 text-left transition-colors",
                  activeChannel?.id === channel.id
                    ? "border-accent bg-accent/10"
                    : "border-border bg-bg-card hover:bg-bg-elevated",
                ].join(" ")}
              >
                {channel.logo_url ? (
                  <img
                    src={channel.logo_url}
                    alt={channel.name}
                    className="h-10 w-10 shrink-0 rounded-[--radius-md] object-contain bg-white p-1"
                  />
                ) : (
                  <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-[--radius-md] bg-bg-elevated text-sm font-bold text-text-muted">
                    {channel.number}
                  </div>
                )}
                <div className="flex flex-col overflow-hidden">
                  <span className="truncate text-sm font-medium text-text-primary">
                    {channel.name}
                  </span>
                  <span className="text-xs text-text-muted">
                    Ch. {channel.number}
                  </span>
                </div>
              </button>
            ))}
          </div>
        </section>
      ))}
    </div>
  );
}
