package observability

// StreamSink adapts Metrics to the small sink interface the stream.Manager
// expects, without forcing the stream package to import observability. This
// keeps the dependency direction one-way (observability → stream) and lets
// tests for stream stay free of Prometheus.
type StreamSink struct {
	m *Metrics
}

// NewStreamSink builds a sink backed by the given Metrics. Passing a nil
// Metrics returns a nil sink; callers should guard or rely on the
// stream.Manager's own nil-safe fallback.
func NewStreamSink(m *Metrics) *StreamSink {
	if m == nil {
		return nil
	}
	return &StreamSink{m: m}
}

func (s *StreamSink) TranscodeStarted() {
	s.m.StreamTranscodeStarts.WithLabelValues("started").Inc()
}

func (s *StreamSink) TranscodeBusy() {
	s.m.StreamTranscodeStarts.WithLabelValues("busy").Inc()
}

func (s *StreamSink) TranscodeFailed() {
	s.m.StreamTranscodeStarts.WithLabelValues("failed").Inc()
}

func (s *StreamSink) SetActiveSessions(n int) {
	s.m.StreamActiveSessions.Set(float64(n))
}
