package main

import (
	"context"
	"errors"
	"testing"

	"hubplay/internal/testutil"
)

type fakeSettings struct {
	values map[string]string
	setErr error
	sets   int
}

func (f *fakeSettings) GetOr(_ context.Context, key, def string) (string, error) {
	if v, ok := f.values[key]; ok {
		return v, nil
	}
	return def, nil
}

func (f *fakeSettings) Set(_ context.Context, key, value string) error {
	f.sets++
	if f.setErr != nil {
		return f.setErr
	}
	f.values[key] = value
	return nil
}

func TestLoadOrCreateServerID_GeneratesOnceAndPersists(t *testing.T) {
	store := &fakeSettings{values: map[string]string{}}
	first := loadOrCreateServerID(context.Background(), store, testutil.NopLogger())
	if len(first) != 16 {
		t.Fatalf("id = %q, want 16 hex chars", first)
	}
	second := loadOrCreateServerID(context.Background(), store, testutil.NopLogger())
	if second != first {
		t.Fatalf("second call = %q, want the persisted %q", second, first)
	}
	if store.sets != 1 {
		t.Fatalf("Set called %d times, want 1", store.sets)
	}
}

func TestLoadOrCreateServerID_EmptyWhenPersistFails(t *testing.T) {
	store := &fakeSettings{values: map[string]string{}, setErr: errors.New("disk full")}
	if got := loadOrCreateServerID(context.Background(), store, testutil.NopLogger()); got != "" {
		t.Fatalf("id = %q, want empty when the id cannot be persisted", got)
	}
}
