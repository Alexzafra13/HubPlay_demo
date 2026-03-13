package clock

import (
	"testing"
	"time"
)

func TestRealClock(t *testing.T) {
	c := New()
	before := time.Now()
	now := c.Now()
	after := time.Now()

	if now.Before(before) || now.After(after) {
		t.Error("RealClock.Now() should return current time")
	}
}

func TestMockClock(t *testing.T) {
	fixed := time.Date(2026, 3, 13, 10, 0, 0, 0, time.UTC)
	c := &Mock{CurrentTime: fixed}

	if !c.Now().Equal(fixed) {
		t.Error("MockClock should return the fixed time")
	}

	c.Advance(5 * time.Minute)

	expected := fixed.Add(5 * time.Minute)
	if !c.Now().Equal(expected) {
		t.Errorf("expected %v after Advance, got %v", expected, c.Now())
	}
}
