package clock

import "time"

// Clock abstracts time for testability.
type Clock interface {
	Now() time.Time
}

// Real returns system time.
type Real struct{}

func New() *Real { return &Real{} }

func (Real) Now() time.Time { return time.Now() }

// Mock is a controllable clock for tests.
type Mock struct {
	CurrentTime time.Time
}

func (m *Mock) Now() time.Time { return m.CurrentTime }

func (m *Mock) Advance(d time.Duration) { m.CurrentTime = m.CurrentTime.Add(d) }
