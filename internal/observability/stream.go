package observability

// StreamSink: adapta Metrics al sink que stream.Manager espera, sin forzar a
// stream a importar observability. Arrow one-way: observability → stream;
// tests de stream quedan libres de Prometheus.
type StreamSink struct {
	m *Metrics
}

// NewStreamSink: nil Metrics → nil sink. El caller guarda o se apoya en el
// fallback nil-safe del stream.Manager.
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
