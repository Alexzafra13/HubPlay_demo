// logoFailureCache — a process-wide memory of channel-logo URLs that
// have already failed to load, shared across every <img> that renders a
// channel logo (ChannelLogo, ChannelCard, rails…).
//
// Why it exists: the same channel shows up in several surfaces at once
// (hero spotlight + EPG grid row + mini player). Each renders its own
// <img src="/api/v1/channels/{id}/logo">. When that endpoint 404s (dead
// upstream tvg-logo), the browser HTTP cache dedupes repeat *network*
// hits — but only once the first response lands and only if the response
// is cacheable. This module closes the remaining gap on the client: once
// any instance has seen a URL fail, every later mount skips the <img>
// entirely and paints the initials avatar straight away. No flash, no
// speculative request.
//
// Keyed on the full logo URL (not channel id) so a re-import that hands a
// channel a brand-new working URL is not poisoned by the old failure.
// The set is intentionally unbounded: logo URLs are few (one per channel)
// and the memory cost is trivial next to the request churn it saves.

const failedLogoUrls = new Set<string>();

/** True when this exact logo URL has already failed to load this session. */
export function hasLogoFailed(url: string | null | undefined): boolean {
  return !!url && failedLogoUrls.has(url);
}

/** Remember that this logo URL failed so other instances skip the <img>. */
export function markLogoFailed(url: string | null | undefined): void {
  if (url) failedLogoUrls.add(url);
}

// Test-only escape hatch: the set is module-level state, so tests that
// assert fallback behaviour need a way to start from a clean slate.
export function __resetLogoFailureCache(): void {
  failedLogoUrls.clear();
}
