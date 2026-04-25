package iptv

// Active stream prober. Complements the passive health tracking
// performed by the proxy: the proxy only sees channels users actually
// open, so a never-tuned channel sits at consecutive_failures=0
// forever even if its upstream URL has been dead for months. The
// prober walks the channel list periodically (and after every M3U
// refresh) and records a probe outcome against each one via the
// existing ChannelHealthReporter seam — so the user-facing
// `ListHealthyByLibrary` query auto-hides anything that fails three
// consecutive probes, exactly like the proxy path.
//
// Design notes
//
//   - We do not run ffprobe here. Spawning a subprocess per channel
//     for hundreds of channels every few hours is too costly and the
//     marginal accuracy over an HTTP-only probe is small for the
//     common dead-channel modes (DNS NX, connection refused, 4xx,
//     manifest 404). Real playback failure is captured by the
//     hls.js beacon which feeds the same state.
//   - HEAD is unreliable on IPTV CDNs (many reject it or return
//     misleading status), so we use a ranged GET that pulls only the
//     first KB. For HLS we then look for the `#EXTM3U` magic on the
//     fetched bytes — a 200 with HTML "blocked" page or a soft-404
//     looks ok at the HTTP layer but fails the magic check.
//   - Concurrency is bounded by a semaphore so a 500-channel library
//     does not open 500 connections in parallel against a CDN that
//     would tarpit or rate-limit us.
//   - Non-HTTP schemes (rtmp, rtsp, udp) are reported as "ok" without
//     probing. We have no way to validate them cheaply and the proxy
//     will catch real breakage when a user tunes in.

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"hubplay/internal/db"
)

const (
	// proberDefaultTimeout caps each individual probe. 6 s gives
	// slow CDNs room without making a full library walk drag on.
	proberDefaultTimeout = 6 * time.Second

	// proberDefaultConcurrency caps how many probes run in parallel.
	// 8 keeps the open-fd count modest and avoids triggering CDN
	// rate-limits even on libraries with hundreds of channels.
	proberDefaultConcurrency = 8

	// proberMaxBody is the cap on bytes read from the manifest
	// before we declare the probe a success. 64 KB is enough to see
	// the `#EXTM3U` line on any well-formed playlist while bounding
	// memory if some upstream sends a huge HTML page.
	proberMaxBody = 64 << 10
)

// Prober runs active probes against channel stream URLs and reports
// the outcome through ChannelHealthReporter. Stateless across runs —
// the persistence layer owns the consecutive-failure counter.
type Prober struct {
	client      *http.Client
	reporter    ChannelHealthReporter
	concurrency int
	timeout     time.Duration
}

// NewProber builds a Prober. Reporter is required (a Prober that
// can't record outcomes is useless — fail loud at construction
// rather than silently in production).
func NewProber(client *http.Client, reporter ChannelHealthReporter) *Prober {
	if client == nil {
		client = &http.Client{Timeout: proberDefaultTimeout}
	}
	return &Prober{
		client:      client,
		reporter:    reporter,
		concurrency: proberDefaultConcurrency,
		timeout:     proberDefaultTimeout,
	}
}

// SetConcurrency overrides the parallel-probe cap. Mostly used by
// tests to force serial execution; production sticks to the default.
func (p *Prober) SetConcurrency(n int) {
	if n < 1 {
		n = 1
	}
	p.concurrency = n
}

// SetTimeout overrides the per-probe deadline. Tests use a short
// timeout (50 ms) to keep the suite fast; production sticks to the
// default.
func (p *Prober) SetTimeout(d time.Duration) {
	if d <= 0 {
		d = proberDefaultTimeout
	}
	p.timeout = d
}

// ProbeChannels runs one probe per channel, bounded by the
// configured concurrency. Returns a summary that callers can log.
// Honours ctx — when ctx is cancelled, in-flight probes finish but
// no new ones start, and the partial summary is returned.
func (p *Prober) ProbeChannels(ctx context.Context, channels []*db.Channel) ProbeSummary {
	summary := ProbeSummary{Total: len(channels)}
	if len(channels) == 0 || p.reporter == nil {
		return summary
	}

	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		sem = make(chan struct{}, p.concurrency)
	)

	scheduled := 0
schedule:
	for _, ch := range channels {
		// Guard before the select so an already-cancelled ctx wins
		// deterministically — without this, the runtime can pick the
		// `sem <- struct{}{}` branch even when ctx.Done is also ready.
		if ctx.Err() != nil {
			break schedule
		}
		select {
		case <-ctx.Done():
			break schedule
		case sem <- struct{}{}:
		}
		if ctx.Err() != nil {
			// We may have just acquired a slot in the same select tick
			// where ctx flipped — return it so we don't block the rest.
			select {
			case <-sem:
			default:
			}
			break schedule
		}
		scheduled++

		wg.Add(1)
		go func(ch *db.Channel) {
			defer wg.Done()
			defer func() { <-sem }()

			outcome := p.probeOne(ctx, ch.StreamURL)

			mu.Lock()
			switch outcome.kind {
			case probeKindOK:
				summary.OK++
				p.reporter.RecordProbeSuccess(ctx, ch.ID)
			case probeKindSkip:
				summary.Skipped++
				// Skipped (non-HTTP) channels are reported as
				// success — see file header comment.
				p.reporter.RecordProbeSuccess(ctx, ch.ID)
			case probeKindFail:
				summary.Failed++
				p.reporter.RecordProbeFailure(ctx, ch.ID, outcome.err)
			}
			mu.Unlock()
		}(ch)
	}

	// Drain in-flight probes BEFORE accounting for the unscheduled
	// tail — otherwise the read-then-add on summary.{OK,Failed,...}
	// races with the worker goroutines.
	wg.Wait()
	mu.Lock()
	if remaining := len(channels) - scheduled; remaining > 0 {
		summary.Skipped += remaining
	}
	mu.Unlock()
	return summary
}

// ProbeSummary is the per-run report from ProbeChannels.
type ProbeSummary struct {
	Total   int // channels in the input list
	OK      int // probe succeeded (HTTP 2xx + valid magic, or non-HTTP scheme)
	Failed  int // probe failed (HTTP error, bad magic, timeout)
	Skipped int // ctx cancelled before this channel was probed
}

type probeKind int

const (
	probeKindOK probeKind = iota
	probeKindFail
	probeKindSkip
)

type probeOutcome struct {
	kind probeKind
	err  error
}

// probeOne performs a single ranged GET against streamURL and
// classifies the result. The returned err on probeKindFail is
// the short, human-readable cause sanitiseProbeError will then
// store in the DB.
func (p *Prober) probeOne(parent context.Context, streamURL string) probeOutcome {
	streamURL = strings.TrimSpace(streamURL)
	if streamURL == "" {
		return probeOutcome{kind: probeKindFail, err: fmt.Errorf("empty stream url")}
	}

	// Non-HTTP schemes (rtmp://, rtsp://, udp://) cannot be probed
	// with net/http. Treat as "skip / pass" so we don't penalise
	// channels we can't validate cheaply.
	lower := strings.ToLower(streamURL)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return probeOutcome{kind: probeKindSkip}
	}

	ctx, cancel := context.WithTimeout(parent, p.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		return probeOutcome{kind: probeKindFail, err: fmt.Errorf("build request: %w", err)}
	}
	// Some CDNs only honour Range on Accept-Ranges-supporting
	// resources; if they ignore it we still cap the read body-side.
	req.Header.Set("Range", "bytes=0-65535")
	// Mimic a media client so endpoints that gate on User-Agent
	// (some Pluto / Samsung TV streams) don't 403.
	req.Header.Set("User-Agent", "VLC/3.0.20 LibVLC/3.0.20")
	req.Header.Set("Accept", "*/*")

	resp, err := p.client.Do(req)
	if err != nil {
		return probeOutcome{kind: probeKindFail, err: err}
	}
	defer resp.Body.Close() //nolint:errcheck

	// 2xx and 206 (Partial Content) are both wins. 3xx the http
	// client follows by default. 4xx/5xx are failures.
	if resp.StatusCode >= 400 {
		return probeOutcome{kind: probeKindFail, err: fmt.Errorf("HTTP %d", resp.StatusCode)}
	}

	// For HLS playlists, validate the magic. For TS segments and
	// other binary streams, a 2xx with non-empty body is enough —
	// we can't cheaply validate every container.
	body, err := io.ReadAll(io.LimitReader(resp.Body, proberMaxBody))
	if err != nil {
		return probeOutcome{kind: probeKindFail, err: fmt.Errorf("read body: %w", err)}
	}
	if len(body) == 0 {
		return probeOutcome{kind: probeKindFail, err: fmt.Errorf("empty response body")}
	}

	if looksLikeHLS(streamURL, resp.Header.Get("Content-Type")) {
		if !hasHLSMagic(body) {
			return probeOutcome{kind: probeKindFail, err: fmt.Errorf("invalid HLS manifest")}
		}
	}

	return probeOutcome{kind: probeKindOK}
}

// looksLikeHLS decides whether to apply the `#EXTM3U` magic check.
// Driven by URL suffix and Content-Type — both are fallible alone
// (CDN may serve `.m3u8` as `application/octet-stream`, or set the
// type without the suffix), so we OR them.
func looksLikeHLS(url, contentType string) bool {
	low := strings.ToLower(url)
	if i := strings.IndexByte(low, '?'); i >= 0 {
		low = low[:i]
	}
	if strings.HasSuffix(low, ".m3u8") || strings.HasSuffix(low, ".m3u") {
		return true
	}
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "mpegurl") || strings.Contains(ct, "x-mpegurl") {
		return true
	}
	return false
}

// hasHLSMagic verifies the first non-empty line of body starts with
// `#EXTM3U` — robust against leading BOM and whitespace.
func hasHLSMagic(body []byte) bool {
	// Strip UTF-8 BOM if present.
	if len(body) >= 3 && body[0] == 0xEF && body[1] == 0xBB && body[2] == 0xBF {
		body = body[3:]
	}
	for len(body) > 0 {
		nl := -1
		for i, b := range body {
			if b == '\n' {
				nl = i
				break
			}
		}
		var line []byte
		if nl < 0 {
			line = body
			body = nil
		} else {
			line = body[:nl]
			body = body[nl+1:]
		}
		trimmed := strings.TrimSpace(string(line))
		if trimmed == "" {
			continue
		}
		return strings.HasPrefix(trimmed, "#EXTM3U")
	}
	return false
}
