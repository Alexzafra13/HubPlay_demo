package iptv

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// ─── Fake reporter ──────────────────────────────────────────────────

type fakeHealthReporter struct {
	mu        sync.Mutex
	successes []string
	failures  []struct {
		ChannelID string
		Err       error
	}
}

func (f *fakeHealthReporter) RecordProbeSuccess(_ context.Context, channelID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.successes = append(f.successes, channelID)
}

func (f *fakeHealthReporter) RecordProbeFailure(_ context.Context, channelID string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures = append(f.failures, struct {
		ChannelID string
		Err       error
	}{channelID, err})
}

func (f *fakeHealthReporter) snapshot() (successes []string, failures int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.successes...), len(f.failures)
}

func newTestProxy(reporter ChannelHealthReporter) *StreamProxy {
	p := NewStreamProxy(slog.New(slog.NewTextHandler(new(discard), nil)))
	p.SetHealthReporter(reporter)
	return p
}

// ─── reportOutcome ──────────────────────────────────────────────────
//
// The outcome reporter is the only code that distinguishes upstream
// failures from client cancellations, so its tests are where we pin
// the semantics down.

func TestReportOutcome_NilReporterIsNoOp(t *testing.T) {
	p := NewStreamProxy(slog.New(slog.NewTextHandler(new(discard), nil)))
	// Should not panic with nil reporter.
	p.reportOutcome(context.Background(), context.Background(), "c-1", errors.New("boom"))
}

func TestReportOutcome_Success(t *testing.T) {
	rep := &fakeHealthReporter{}
	p := newTestProxy(rep)

	p.reportOutcome(context.Background(), context.Background(), "c-1", nil)

	got, _ := rep.snapshot()
	if len(got) != 1 || got[0] != "c-1" {
		t.Errorf("successes = %v, want [c-1]", got)
	}
}

func TestReportOutcome_Failure(t *testing.T) {
	rep := &fakeHealthReporter{}
	p := newTestProxy(rep)

	p.reportOutcome(context.Background(), context.Background(), "c-1",
		errors.New("no such host"))

	_, failures := rep.snapshot()
	if failures != 1 {
		t.Errorf("failures = %d, want 1", failures)
	}
}

// The user hitting stop / closing the tab must not count as a channel
// fault — otherwise every casual click would pile up failures on
// working channels.
func TestReportOutcome_ClientCancellation_NotRecorded(t *testing.T) {
	rep := &fakeHealthReporter{}
	p := newTestProxy(rep)

	// Simulate a request context that's already cancelled.
	fetchCtx, cancel := context.WithCancel(context.Background())
	cancel()

	p.reportOutcome(context.Background(), fetchCtx, "c-1",
		context.Canceled)

	got, failures := rep.snapshot()
	if len(got) != 0 || failures != 0 {
		t.Errorf("should not record anything; successes=%v failures=%d", got, failures)
	}
}

// context.DeadlineExceeded is a real problem (upstream too slow) and
// should count, unlike user cancellation.
func TestReportOutcome_DeadlineExceeded_Recorded(t *testing.T) {
	rep := &fakeHealthReporter{}
	p := newTestProxy(rep)

	p.reportOutcome(context.Background(), context.Background(), "c-1",
		context.DeadlineExceeded)

	_, failures := rep.snapshot()
	if failures != 1 {
		t.Errorf("DeadlineExceeded should be counted, failures = %d", failures)
	}
}

// ─── Integration: stream path reports ───────────────────────────────

// A real fetchUpstream against a working test server populates the
// success counter. End-to-end gate that the wiring is live.
func TestStreamOnceWithChannel_ReportsSuccess(t *testing.T) {
	unblockLoopback(t)
	rep := &fakeHealthReporter{}
	p := newTestProxy(rep)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "video/mp2t")
		_, _ = io.WriteString(w, "body")
	}))
	defer srv.Close()

	rr := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = p.streamOnceWithChannel(ctx, rr, "c-real", srv.URL)

	got, _ := rep.snapshot()
	if len(got) != 1 || got[0] != "c-real" {
		t.Errorf("expected success for c-real, got %v", got)
	}
}

// Upstream DNS failure gets reported as a proxy failure (not a
// client cancellation).
func TestStreamOnceWithChannel_ReportsFailure(t *testing.T) {
	rep := &fakeHealthReporter{}
	p := newTestProxy(rep)

	rr := httptest.NewRecorder()
	ctx := context.Background()
	err := p.streamOnceWithChannel(ctx, rr, "c-fail",
		"https://nonexistent.invalid.example/stream.m3u8")
	if err == nil {
		t.Fatal("expected fetch error on bogus host")
	}

	_, failures := rep.snapshot()
	if failures != 1 {
		t.Errorf("expected 1 failure recorded, got %d", failures)
	}
}

// When the request context is cancelled mid-fetch the error ends up
// as context.Canceled; we must not pollute the channel's counter.
func TestStreamOnceWithChannel_CancelledContext_NoFailure(t *testing.T) {
	unblockLoopback(t)
	rep := &fakeHealthReporter{}
	p := newTestProxy(rep)

	// Server that holds the connection open so cancellation happens
	// mid-flight.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately — fetch should fail with context.Canceled
	rr := httptest.NewRecorder()
	_ = p.streamOnceWithChannel(ctx, rr, "c-cancelled", srv.URL)

	got, failures := rep.snapshot()
	if len(got) != 0 || failures != 0 {
		t.Errorf("cancelled context should not touch health; successes=%v failures=%d",
			got, failures)
	}
}

// ─── Sanitisation ───────────────────────────────────────────────────

func TestSanitiseProbeError(t *testing.T) {
	cases := []struct {
		in   error
		want string
	}{
		{nil, ""},
		{errors.New("connect: Get \"...\": no such host"), "Get \"...\": no such host"},
		{errors.New("plain message"), "plain message"},
	}
	for _, tc := range cases {
		got := sanitiseProbeError(tc.in)
		if !strings.Contains(got, tc.want) {
			t.Errorf("sanitise(%v) = %q, want contains %q", tc.in, got, tc.want)
		}
	}
}
