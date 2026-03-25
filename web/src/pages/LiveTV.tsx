import { useState, useMemo } from "react";
import { useChannels, useLibraries, usePublicCountries, useImportPublicIPTV } from "@/api/hooks";
import type { Channel, PublicCountry } from "@/api/types";
import { Spinner, EmptyState } from "@/components/common";

export default function LiveTV() {
  const { data: libraries, isLoading: librariesLoading } = useLibraries();
  const liveTvLibrary = useMemo(
    () => libraries?.find((l) => l.content_type === "livetv"),
    [libraries],
  );

  const { data: channels, isLoading: channelsLoading } = useChannels(liveTvLibrary?.id);
  const [activeChannel, setActiveChannel] = useState<Channel | null>(null);
  const [search, setSearch] = useState("");

  const grouped = useMemo(() => {
    if (!channels) return new Map<string, Channel[]>();
    const filtered = search
      ? channels.filter(
          (ch) =>
            ch.name.toLowerCase().includes(search.toLowerCase()) ||
            (ch.group ?? "").toLowerCase().includes(search.toLowerCase()),
        )
      : channels;
    const map = new Map<string, Channel[]>();
    for (const ch of filtered) {
      const group = ch.group ?? "Uncategorized";
      const list = map.get(group) ?? [];
      list.push(ch);
      map.set(group, list);
    }
    return map;
  }, [channels, search]);

  const isLoading = librariesLoading || channelsLoading;

  if (isLoading) {
    return (
      <div className="flex min-h-[60vh] items-center justify-center">
        <Spinner size="lg" />
      </div>
    );
  }

  // No livetv library or no channels — show country selector
  if (!liveTvLibrary || !channels || channels.length === 0) {
    return <CountrySelector hasLibrary={!!liveTvLibrary} libraryId={liveTvLibrary?.id} />;
  }

  return (
    <div className="flex flex-col gap-6 px-6 py-8 sm:px-10">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <h1 className="text-2xl font-bold text-text-primary sm:text-3xl">
          Live TV
        </h1>
        <input
          type="text"
          placeholder="Search channels..."
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="w-full max-w-xs rounded-[--radius-md] border border-border bg-bg-card px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
        />
      </div>

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

      {grouped.size === 0 && search && (
        <div className="py-12 text-center text-text-muted">
          No channels match "{search}"
        </div>
      )}
    </div>
  );
}

function CountrySelector({ hasLibrary, libraryId }: { hasLibrary: boolean; libraryId?: string }) {
  const { data: countries, isLoading } = usePublicCountries();
  const importMutation = useImportPublicIPTV();
  const [selectedCountry, setSelectedCountry] = useState<PublicCountry | null>(null);
  const [countrySearch, setCountrySearch] = useState("");

  const filtered = useMemo(() => {
    if (!countries) return [];
    if (!countrySearch) return countries;
    return countries.filter((c) =>
      c.name.toLowerCase().includes(countrySearch.toLowerCase()),
    );
  }, [countries, countrySearch]);

  const handleImport = () => {
    if (!selectedCountry) return;
    importMutation.mutate(
      { country: selectedCountry.code },
      {
        onSuccess: () => {
          // Channels will load after library is created and M3U is refreshed.
          // The page will re-render with the new library.
          window.location.reload();
        },
      },
    );
  };

  return (
    <div className="flex min-h-[60vh] items-center justify-center px-6">
      <div className="w-full max-w-lg">
        <div className="mb-8 text-center">
          <svg
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth={1.5}
            className="mx-auto mb-4 h-16 w-16 text-text-muted"
          >
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              d="M6 20h12M6 4h12M4 8h16v8H4z"
            />
          </svg>
          <h2 className="text-2xl font-bold text-text-primary">
            {hasLibrary ? "No channels loaded yet" : "Set up Live TV"}
          </h2>
          <p className="mt-2 text-text-muted">
            Choose a country to import free public IPTV channels.
            Channels are provided by the iptv-org community project.
          </p>
        </div>

        <div className="rounded-[--radius-lg] border border-border bg-bg-card p-6">
          <input
            type="text"
            placeholder="Search countries..."
            value={countrySearch}
            onChange={(e) => setCountrySearch(e.target.value)}
            className="mb-4 w-full rounded-[--radius-md] border border-border bg-bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
          />

          {isLoading ? (
            <div className="flex justify-center py-8">
              <Spinner size="md" />
            </div>
          ) : (
            <div className="grid max-h-64 grid-cols-2 gap-2 overflow-y-auto sm:grid-cols-3">
              {filtered.map((country) => (
                <button
                  key={country.code}
                  type="button"
                  onClick={() => setSelectedCountry(country)}
                  className={[
                    "rounded-[--radius-md] border px-3 py-2 text-left text-sm transition-colors",
                    selectedCountry?.code === country.code
                      ? "border-accent bg-accent/10 text-text-primary"
                      : "border-border bg-bg-elevated text-text-secondary hover:bg-bg-card",
                  ].join(" ")}
                >
                  <span className="mr-1.5 text-xs">{country.flag}</span>
                  {country.name}
                </button>
              ))}
            </div>
          )}

          {selectedCountry && (
            <div className="mt-4 flex items-center justify-between">
              <span className="text-sm text-text-secondary">
                Selected: <strong>{selectedCountry.name}</strong>
              </span>
              <button
                type="button"
                onClick={handleImport}
                disabled={importMutation.isPending}
                className="rounded-[--radius-md] bg-accent px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-accent/90 disabled:opacity-50"
              >
                {importMutation.isPending ? "Importing..." : "Import channels"}
              </button>
            </div>
          )}

          {importMutation.isError && (
            <p className="mt-3 text-sm text-red-400">
              Failed to import channels. Please try again.
            </p>
          )}
        </div>
      </div>
    </div>
  );
}
