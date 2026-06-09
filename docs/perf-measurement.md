# Measuring HubPlay performance

This is the runbook for measuring what the app actually does and delivers
end-to-end. **Run it on the hardware you care about** — performance is
hardware-specific, so to know "does it run well on my Pi / NAS / server",
measure on that box, not on a dev laptop. A laptop is fine for *relative*
before/after comparisons of a change.

Measuring "everything" needs three complementary pillars:

| Pillar | Answers | Tool |
|---|---|---|
| **Profiles** | *Where* does CPU / memory / time go? leaks? lock contention? | `pprof` (`/debug/pprof`) |
| **Load** | *How much* can it take? throughput, latency under concurrency | `cmd/hploadgen` (or k6/vegeta) |
| **Metrics** | Rates & latencies over time, in production | Prometheus `/metrics` |

> A profile with no load on the server tells you almost nothing — always
> profile **while** driving load (or under real traffic).

---

## 0. Prerequisites

```bash
# Build the server + dev tools
make build                      # or: go build -o hubplay ./cmd/hubplay
go build -o hpseed   ./cmd/hpseed
go build -o hploadgen ./cmd/hploadgen

# FFmpeg/FFprobe are required for streaming. Fetch a static build:
./scripts/fetch-ffmpeg.sh       # drops ffmpeg/ffprobe next to the binary
```

Enable profiling + metrics in `hubplay.yaml` (token-gated — pprof is OFF
and fails closed without a token):

```yaml
observability:
  metrics_enabled: true
  metrics_token: "CHANGE_ME"     # required to expose /metrics and pprof
  pprof_enabled: true            # turn OFF again once you're done measuring
```

Export the token for the commands below:

```bash
export TOK="CHANGE_ME"
export BASE="http://localhost:8096"
```

---

## 1. API + catalogue (throughput, queries, allocations)

Seed a realistic catalogue so the query hot paths have data (reuses the
production repositories, so rows match a real scan):

```bash
# stop the server first; seed writes directly to the SQLite file
./hpseed -db ./data/hubplay.db -items 5000 -channels 5000
# prints an admin login: admin / hubplay123
```

Start the server, then drive load while capturing a 30 s CPU profile:

```bash
# terminal 1 — capture (token via Authorization header)
curl -H "Authorization: Bearer $TOK" "$BASE/debug/pprof/profile?seconds=30" -o cpu.prof

# terminal 2 — authenticated browse/search/list mix
./hploadgen -url "$BASE" -user admin -pass hubplay123 -duration 32s -workers 50
# prints: ok=... rps=...

# analyse
go tool pprof -http=: ./hubplay cpu.prof        # interactive flame graph
go tool pprof -top -cum ./hubplay cpu.prof       # text, by cumulative
```

Heap / allocation profile (find what churns the GC):

```bash
curl -H "Authorization: Bearer $TOK" "$BASE/debug/pprof/allocs" -o allocs.prof
go tool pprof -top -sample_index=alloc_space ./hubplay allocs.prof
```

Other useful profiles: `/debug/pprof/heap` (live heap), `/debug/pprof/goroutine`
(leaks), `/debug/pprof/mutex` and `/debug/pprof/block` (contention — set
`runtime.SetMutexProfileFraction`/`SetBlockProfileRate` first if you need them).

---

## 2. Library scanning (ffprobe + thumbnails + metadata)

This is CPU + I/O bound on *real files* — seeding can't measure it. Point a
library at a folder and time the scan:

```bash
# create a library via the admin UI (or API) pointing at /media/movies,
# then trigger a scan and watch the timing + progress events:
time curl -X POST -H "Authorization: Bearer $USER_TOKEN" "$BASE/api/v1/libraries/$LIB_ID/scan"
# follow progress (SSE):
curl -N -H "Authorization: Bearer $USER_TOKEN" "$BASE/api/v1/me/events"
```

Watch the host while it runs: `htop` / `docker stats` for CPU + RAM, and the
server log for `library scan completed ... elapsed_ms=...`. Scan throughput
is items/second; note it scales with file count, not library size in TB.

---

## 3. Transcoding (the heaviest path — needs real media + FFmpeg)

Start several concurrent playbacks that force a transcode (a client whose
codecs don't match the source), then watch resource use:

```bash
# kick off N HLS transcode sessions in parallel (adjust itemIDs):
for id in $ITEM1 $ITEM2 $ITEM3; do
  curl -s -H "Authorization: Bearer $USER_TOKEN" \
    "$BASE/api/v1/stream/$id/master.m3u8?audio=-1" >/dev/null &
done

# observe:
docker stats hubplay              # CPU% + MEM of the container
pgrep -a ffmpeg                   # how many ffmpeg children, and their args
nvidia-smi / intel_gpu_top        # hwaccel utilisation, if configured
```

Check the auto-tune the server logged at boot: `streaming auto-tune applied
hw_accel=... cpu_count=... max_sessions=...`. On a hwaccel box you should
see NVENC/VAAPI sessions; on CPU-only, the libx264 preset scales down with
core count. Confirm ffmpeg processes die when you `DELETE
/api/v1/stream/{id}/session` or stop the client (no orphans — see the
process-group teardown).

---

## 4. Behaviour under a memory / CPU limit (constrained hardware)

Simulate a small box without owning one — the runtime now honours cgroup
limits (GOMAXPROCS from the CPU quota, GOMEMLIMIT from the memory limit):

```bash
docker run --rm --cpus=2 --memory=1g -p 8096:8096 \
  -v $PWD/data:/data ghcr.io/alexzafra13/hubplay:latest
```

In the boot log you should see, when a limit is set:

```
GOMAXPROCS tuned from cgroup CPU quota   cpu_quota=2 gomaxprocs=2 host_cpus=16
GOMEMLIMIT tuned from cgroup memory limit limit_bytes=1073741824 ...
streaming auto-tune applied ... cpu_count=2 ...   # sized to the quota, not the host
```

Then run §1–§3 against it and watch that RSS stays under the limit (the GC
should reclaim before the OOM-killer fires) and that it doesn't over-schedule.

---

## 5. Frontend (browser-side)

```bash
cd web && pnpm build
npx lighthouse "$BASE" --view            # TTI, LCP, bundle weight
npx vite-bundle-visualizer               # what's in each chunk
```

For real playback, open the player in Chrome DevTools → Performance, record a
play + seek, and check for long tasks / dropped frames; the Network tab shows
HLS segment fetch cadence.

---

## What can't be measured in a generic CI sandbox

No FFmpeg, no GPU, no real media, no browser, and no representative hardware
— so §2–§5 (scanning, transcoding, hwaccel, frontend) must run on a real
machine. §1 (API/DB) can be profiled anywhere the Go toolchain runs.
