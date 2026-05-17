package clock

import "time"

type Clock interface {
	Now() time.Time
}

type Real struct{}

func New() *Real { return &Real{} }

func (Real) Now() time.Time { return time.Now() }

type Mock struct {
	CurrentTime time.Time
}

func (m *Mock) Now() time.Time { return m.CurrentTime }

func (m *Mock) Advance(d time.Duration) { m.CurrentTime = m.CurrentTime.Add(d) }
