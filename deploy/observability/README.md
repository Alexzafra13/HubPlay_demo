# HubPlay observability stack (Prometheus + Grafana)

A turnkey, right-sized monitoring stack for a self-hosted media server. It
scrapes the metrics HubPlay already emits and renders them in a
ready-made Grafana dashboard — no app changes needed.

> For a Plex-style app this dashboard + `pprof` (see
> `docs/perf-measurement.md`) are the core you'll actually use day to day.
> The defining bottleneck — transcoding capacity — is measured by watching
> CPU/GPU under real playback, not by load-testing the API.

## Run it

1. Enable metrics in `hubplay.yaml` and pick a token:

   ```yaml
   observability:
     metrics_enabled: true
     metrics_token: "your-token"
   ```

2. Put that token in `prometheus.yml` (the `credentials:` field).

3. Start the stack (separate from the app, so you run it only when you
   want to look):

   ```bash
   docker compose -f deploy/observability/docker-compose.observability.yml up -d
   ```

4. Open Grafana at <http://localhost:3000> (admin / admin) → the
   **HubPlay** dashboard is auto-provisioned.

If HubPlay isn't on the Docker host, change the target in `prometheus.yml`
(e.g. `hubplay:8096` if you put it on the same compose network).

## What the dashboard shows

- **RED** for HTTP: request rate, 5xx error rate, and p50/p95/p99 latency
  per route — derived from `hubplay_http_request_duration_seconds`.
- **Media**: active streams, transcode starts by outcome, IPTV transmux
  starts + decode mode.
- **Runtime/host**: goroutines, heap, process CPU and resident memory.

## Load testing the API

Use the k6 script to drive load (and gate on latency thresholds) while you
watch the dashboard:

```bash
go run ./cmd/hpseed -db ./data/hubplay.db -items 5000 -channels 5000
k6 run -e BASE=http://localhost:8096 -e USER=admin -e PASS=hubplay123 \
  scripts/perf/k6-api.js
```

No k6? `cmd/hploadgen` is a dependency-free fallback (rps + counts only,
no percentiles).
