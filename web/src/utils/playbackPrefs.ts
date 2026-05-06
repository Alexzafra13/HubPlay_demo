// Well-known keys the playback surfaces store under
// `/me/preferences/{key}`. Pulled out of the components that consume
// them so the HeroSection, SeriesHero and Settings page share a single
// source of truth — a typo on either side would silently disconnect
// the toggle from the reading surface.

/**
 * Boolean. When true (default), the detail-page hero auto-plays the
 * trailer ~3s after the page settles. When false, the static backdrop
 * image stays put and no trailer iframe is ever loaded. Persisted per
 * user via `/me/preferences` so the choice follows the account across
 * devices, unlike the session-only "Skip" button on the trailer
 * itself which only suppresses for the current tab.
 */
export const TRAILERS_ENABLED_PREF_KEY = "playback.trailers_enabled";
