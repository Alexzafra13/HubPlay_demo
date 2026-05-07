# Player seek + trick-play bugs (2026-05-07)

Reported by the user during the verification of PR #183 ([streaming race fix](https://github.com/Alexzafra13/HubPlay_demo/pull/183)). The server-side singleflight fix landed cleanly — these are **frontend** issues to attack in a separate session.

## Symptoms (Ballerina (2025) [Bluray 1080p], browser playback)

1. **Scrubbing preview thumbnail is always the same image, regardless of timeline position.** Hovering at minute 40:21 shows a frame that's clearly not from 40:21. Hovering elsewhere shows the same frame again.
2. **Click on the progress bar freezes the player** instead of seeking. The player gets stuck without navigating to the clicked position.
3. **Pause after the freeze shows a frame that doesn't match the click position.** Whatever frame is on screen looks unrelated to where the user clicked.
4. **Pressing Play after the frozen-paused state restarts from the beginning** instead of resuming from the apparent paused position.

## Server-side evidence corroborating a frontend seek loop

During the post-deploy verification, server logs showed **four `RestartSessionAt` events in 42 seconds** with the user reporting only one user-initiated seek:

| time | profile | start_segment | start_time | delta |
|------|---------|---------------|------------|-------|
| 03:37:08 | 1080p | 363 | 2178 s | (initial seek) |
| 03:37:22 | 1080p | 729 | 4374 s | +366 segments / +14 s wallclock |
| 03:37:37 | 1080p | 1095 | 6570 s | +366 segments / +15 s wallclock |
| 03:37:50 | 1080p | 1249 | 7494 s | +154 segments / +13 s wallclock — past EOF |

The 360p variant mirrored the same pattern 2-3 s behind. The strict +366-segment cadence (= 36 m 36 s of video) at ~14 s wallclock intervals is **algorithmic, not human scrubbing** — strong evidence the frontend is firing `seeking` events the user did not request.

The `singleflight` fix in PR #183 collapsed the racing **StartSession** burst correctly. The cascading `RestartSessionAt` events here are unrelated — they are seek-restarts the player *believed* were legitimate.

## Likely causes

- **Trick-play VTT manifest missing**: HubPlay does not appear to publish a WebVTT image-cue track for scrubbing previews. Without it the player overlay falls back to a single still (poster?), which matches symptom #1.
- **Seek event feedback loop in the frontend**: a click on the progress bar fires `video.currentTime = X`, which triggers `seeking` → request manifest/segment → response handler updates progress state → a downstream `useEffect` reads progress and re-applies `video.currentTime`, causing another `seeking`. The `+366 segment` jumps suggest some divisor-based snapping (chapters? fixed offset?) is interfering.
- **Lost-state on pause**: symptoms #3 and #4 together point to the player losing track of the intended `currentTime` when the seek + pause race concludes — the resume button reads stale state and starts from 0.

## Diagnosis path for the next session

1. Open the player in DevTools, attach listeners for `seeking`, `seeked`, `waiting`, `play`, `pause`, `timeupdate`. Confirm whether one user click produces a single `seeking` event or many.
2. Inspect the `.m3u8` master + variant manifests for `EXT-X-IMAGE-STREAM-INF` (HLS image variants) or `EXT-X-MAP` references to a VTT track. If absent, trick-play previews need a backend implementation pass.
3. Hover the timeline with Network → Img filter open. If no thumbnail XHRs fire, the overlay is rendering a static fallback (poster). If they fire but always to the same URL, the time-to-image mapping is broken on the frontend.
4. Reproduce the +366-segment cascade locally — open the player at any movie, do a single click on the progress bar, watch the network log. The cascade should reproduce in dev too if our hypothesis is right.
5. Check the resume / progress flow: `web/src/api/progress.ts` and the player's effect that reads it. The "Play after pause restarts from 0" symptom points there.

## Out of scope for this followup

- The 360p variant being preloaded by hls.js (separate concern: master-manifest emission and frontend ABR config).
- The cache cruft accumulation from old session directories (separate concern: startup-time cache reconciler).
- The `home-handler` `trending query` parse error logged on every detail-page load (separate concern: `time.Time.String()` round-trip with monotonic clock suffix).

## Definition of done

When the next session declares this closed, the following should hold:

- A single user click on the progress bar produces **exactly one** `RestartSessionAt` event in server logs (and only when the click target falls outside the cached-segment window).
- Hover thumbnails reflect the actual frame at the hovered timestamp.
- Pause after a successful seek shows a frame from the seeked-to position.
- Play after pause resumes from the paused position, never from the start.
