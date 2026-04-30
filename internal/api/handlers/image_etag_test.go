package handlers

import "testing"

func TestStrongImageETag(t *testing.T) {
	tests := []struct {
		id, w, want string
	}{
		{"abc-123", "", `"abc-123"`},
		{"abc-123", "320", `"abc-123:w320"`},
		{"d4f8-uuid", "1280", `"d4f8-uuid:w1280"`},
	}
	for _, tt := range tests {
		got := strongImageETag(tt.id, tt.w)
		if got != tt.want {
			t.Errorf("strongImageETag(%q, %q) = %q, want %q", tt.id, tt.w, got, tt.want)
		}
	}
}

func TestETagMatches(t *testing.T) {
	tests := []struct {
		name          string
		ifNoneMatch   string
		etag          string
		wantMatch     bool
	}{
		{"exact match", `"abc-123"`, `"abc-123"`, true},
		{"different ETag", `"def-456"`, `"abc-123"`, false},
		{"wildcard matches anything", `*`, `"abc-123"`, true},
		{"weak prefix tolerated", `W/"abc-123"`, `"abc-123"`, true},
		{"comma list with match", `"x", "abc-123", "y"`, `"abc-123"`, true},
		{"comma list with no match", `"x", "y"`, `"abc-123"`, false},
		{"empty if-none-match", ``, `"abc-123"`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := etagMatches(tt.ifNoneMatch, tt.etag); got != tt.wantMatch {
				t.Errorf("etagMatches(%q, %q) = %v, want %v",
					tt.ifNoneMatch, tt.etag, got, tt.wantMatch)
			}
		})
	}
}
