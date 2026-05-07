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

## Resolution (2026-05-08, branch `claude/review-player-tasks-4guc9`)

Closed end-to-end. Five fixes across frontend and backend.

### Root causes (post-investigation)

The four user-visible symptoms had **two** root causes, not one. The doc above was right that #2/#3/#4 were a frontend feedback loop, but the cause was structural — not "another `useEffect` re-applies progress" but the controlled `<input type="range" value={currentTime}>` itself fighting `setCurrentTime(video.currentTime)` on every `timeupdate` while the seek (1-2 s ffmpeg restart) was in flight. Symptom #1 was a backend bug — the trickplay generator hardcoded `IntervalSec=10` and `Total=GridSide² (100)`, capping coverage at 1000 s = 16 m 40 s for every item.

### Fixes shipped

1. **`SeekBar` pointerup-commit pattern** (`web/src/components/player/PlayerControls.tsx`). The seek input now tracks a local `dragValue` while the pointer is down; `onSeek` only fires on `onPointerUp` / `onPointerCancel`. Mid-drag values are visual echo, not real seeks. Keyboard-arrow nav (no pointer in flight) still commits immediately on each press. This is the Plex / YouTube pattern and kills the multi-fire-during-drag cascade at the source.
2. **`isSeeking` gate in VideoPlayer** (`web/src/components/player/VideoPlayer.tsx`). Listeners on `seeking` / `seeked` flip a ref; `timeupdate` skips `setCurrentTime` while the ref is true. Without this React would re-render the slider with the pre-seek sample (the new buffer hasn't landed yet — `video.currentTime` briefly reports the old position) and the thumb would visibly jitter, reading as a "freeze".
3. **Progress reporter respects `video.seeking`** (`web/src/hooks/useProgressReporter.ts`). Periodic + unmount paths both bail when a seek is in flight. Persisting a mid-seek sample as "where the user is" used to corrupt resume on the next session.
4. **Defensive currentTime preservation across hls.js error/recover** (`web/src/hooks/useHls.ts`). A `lastGoodTimeRef` tracks the most recent settled position; on `MEDIA_ATTACHED` (the recovery path detaches and re-attaches media) we restore from it if `<video>.currentTime` zeroed out. `NETWORK_ERROR` recovery passes the resume time to `hls.startLoad(timeSec)`. The "Attempting to recover…" toast clears on the next `FRAG_LOADED`. This closes the doc'd "Play after frozen-paused state restarts from frame 0".
5. **Trickplay covers full duration** (`internal/imaging/trickplay.go`). New `DurationSeconds` param drives an adaptive `IntervalSec` and `GridSide`: cap thumbnail count at ~400 per sprite (interval scales up for very long content), grid sized via `ceil(sqrt(total))`, manifest reports the real `total` (not `GridSide²`). Manifest carries a `Version` stamp so the handler regenerates legacy v1 (1000-s-coverage) sprites on first hit instead of serving wrong thumbnails forever.
6. **Server-side restart rate limit** (`internal/stream/manager.go`). Sliding-window cap (20 / 60 s) per session as defense in depth. The pointerup-commit fix should keep healthy clients well under this; if a future regression triggers it, the manager refuses to spawn more ffmpegs and returns a 429 the handler maps to `Retry-After: 5`.

### Verification

- Backend: `go test ./... -count=1` → all packages green. New tests: `TestTrickplayParams_Adapt` (4 cases), `TestTrickplayParams_Adapt_NoDuration`, `TestTrickplayParams_Adapt_GridAlwaysFits`, `TestTrickplayManifestVersion_NonZero`, `TestManager_RestartSessionAt_RateLimited`, `TestManager_RestartSessionAt_RateLimitWindowResets`.
- Frontend: `vitest run` → 394 / 394 (was 392; +2 in `useProgressReporter.test.ts` for the seeking-skip gate). `tsc -b` clean. Lint errors in touched files are pre-existing (verified by stashing).

### What was out of scope (still open)

- 360p ABR prefetch from hls.js's master playlist — orthogonal, untouched.
- Cache cruft reconciler at startup — orthogonal, untouched.
- `trending query` parse error in home-handler — orthogonal, untouched.

### What this session did NOT touch

`internal/federation/`, `internal/iptv/`, `internal/auth/`, the live-TV player (`useLiveHls`). The fixes are scoped to the VOD path because that's where the bugs lived; live still uses its own lifecycle hook (the F4 split lives at `web/src/hooks/hlsLifecycle.ts` already, but the seek-loop bug doesn't apply to live anyway since live playlists don't expose far-future seeks).

---

## Addendum: the REAL root cause (2026-05-08 evening — `-copyts`)

The "Resolution" section above declared the bug closed prematurely. The user post-deployed and reproduced a NEW symptom: "queda sin ir y se pausa, al darle Play empieza de nuevo". Browser-side debug instrumentation captured the actual cascade for the first time:

```
t=36.21s   user click @ 29:42 → seeking @1782.2  ✓
t=36.21s   XHR seg 296                             ← correct
t=39.03s   XHR seg 593   (+297 segs)               ← spurious
t=41.94s   XHR seg 890   (+297 segs)               ← spurious
t=48.82s   XHR seg 1171  (end of file)             ← spurious
t=54.32s   durationchange  d=7026.6 → d=1786.5     ← timeline collapsed
t=54.39s   pause + ended                            ← player TERMINATED
```

**Root cause: ffmpeg without `-copyts`** resets the output's PTS to 0 on each restart. Synthesized VOD manifest claims segment 296 covers timeline `[1776, 1782]`, but the produced `segment00296.ts` has internal PTS `[0, 6]`. MSE places segments by their actual PTS (NOT the manifest's claim), so the player's `<video>.currentTime = 1782` lands in a buffer hole. hls.js's stream controller then fires fan-out probe requests at multiples of the seek target trying to find content for the requested time — visible as the +297-segment cadence in server logs. Eventually MediaSource latches onto the smaller buffered range as the new duration, the player reaches EOF, and on Play the user sees "back to the beginning".

Fix in commit `3f0ee55`: add `-copyts` unconditionally after `-i` so segment N always lands at timeline `N * hls_time` regardless of how many ffmpeg runs produced it. Plex and Jellyfin both apply this for the same reason.

### What the earlier "Resolution" actually achieved

The 6 fixes in the previous section were **all defensive layers**, not the root-cause fix. Specifically:

- **Pointerup-commit SeekBar**: pre-existing concern (multi-fire during drag) but not the cause of the cascade. Worth keeping — Plex/YouTube pattern.
- **`isSeeking` gate → `video.seeking` direct read**: defended against thumb jitter that was ITSELF caused by the timeline collapse. Once `-copyts` lands, jitter doesn't happen anyway. The simpler `video.seeking` direct read still wins because it self-recovers if a `seeked` event drops.
- **Progress reporter skip while seeking**: still useful (defends resume state from being corrupted by mid-seek samples). Keep.
- **`lastGoodTimeRef` recovery (both in useHls and VideoPlayer)**: defended against the "Play restarts from 0" symptom. With `-copyts` correct the timeline doesn't collapse anymore so currentTime never resets. The defensive layer is harmless and protects against unrelated future regressions (e.g., recoverMediaError edge cases). Keep but acknowledge as belt-and-suspenders.
- **Trickplay adaptive grid + version stamp**: independent bug. Real fix. Necessary and unrelated to `-copyts`.
- **Restart rate limit + AND-coalesce**: defends against future client-side seek loops that could re-emerge. Cheap defense. Keep.

### Verification (post-fix)

User reported back 2026-05-08: "el ffmpeg funciona perfecto!" ✓ Free seeking confirmed working in production after pulling the build with commit `3f0ee55`. Trickplay 504 timeouts also gone (commit `ac601bc` — async generation).

### Lessons

1. **Don't declare "closed" without prod verification.** The previous "Resolution" section was written before the user could test in their environment with a real long movie. The frontend fixes were defensible but the cascade reproduced — telling us the real cause was elsewhere.
2. **Server logs alone aren't enough.** The +366 / +231 / +297 segment cadences in server logs all looked algorithmic but didn't pin the cause. The user-side debug snippet (XHR + video events + duration changes) was what surfaced the timeline collapse — and from there `-copyts` was a 5-minute fix.
3. **MSE timeline integrity is fragile.** When manifest claims and segment PTS disagree, MSE silently builds a Frankenstein timeline; downstream symptoms (cascading fetches, duration collapse, playhead jumping) look like seek-handling bugs but aren't.
