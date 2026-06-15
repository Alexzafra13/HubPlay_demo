package torrentstream

import (
	"context"
	"sort"
	"strings"
)

// FilterOptions are the curation preferences applied to a raw result set
// before it reaches the UI. They come from server config and can be
// overridden per-request via the request context (WithFilterOptions).
type FilterOptions struct {
	// ExcludeResolutions drops results whose Resolution is listed (e.g.
	// ["2160p"] to hide 4K on bandwidth-limited setups).
	ExcludeResolutions []string
	// MaxSizeGB drops results larger than this many GB. 0 = no limit.
	// Results with an unknown size (0 bytes) are never dropped by size.
	MaxSizeGB float64
	// PreferredLanguage, when set, sorts results that carry this language
	// ahead of the rest within the same quality/seeders tier.
	PreferredLanguage string
	// MaxPerResolution caps how many results survive per resolution group
	// so the UI isn't flooded (e.g. top 5 of each). 0 = no cap.
	MaxPerResolution int
	// ExcludeCam drops cam/TS/screener releases. Default true.
	ExcludeCam bool
}

// DefaultFilterOptions is the sane baseline: drop cams, top 5 per
// resolution, no other restrictions.
func DefaultFilterOptions() FilterOptions {
	return FilterOptions{
		ExcludeCam:       true,
		MaxPerResolution: 5,
	}
}

// filterOptionsCtxKey is the unexported context key for per-request
// overrides.
type filterOptionsCtxKey struct{}

// WithFilterOptions attaches per-request curation preferences to the
// context (e.g. a user's preferred language/quality from their profile).
func WithFilterOptions(ctx context.Context, opts FilterOptions) context.Context {
	return context.WithValue(ctx, filterOptionsCtxKey{}, opts)
}

// filterOptionsFrom returns the context's overrides, falling back to the
// supplied default when none are present.
func filterOptionsFrom(ctx context.Context, fallback FilterOptions) FilterOptions {
	if opts, ok := ctx.Value(filterOptionsCtxKey{}).(FilterOptions); ok {
		return opts
	}
	return fallback
}

// Curate turns a raw result set into the clean, ordered list the frontend
// shows: it drops excluded/low-quality/oversized results, sorts them by a
// configurable cascade and caps the count per resolution group.
//
// Sort cascade (each level breaks ties of the previous):
//  1. Quality   — resolution rank (4K > 1080p > 720p > 480p > unknown)
//  2. Seeders   — most available first
//  3. Language  — preferred language first (when configured)
//  4. Size      — smaller first (cheaper to stream for equal quality)
func Curate(in []SearchResult, opts FilterOptions) []SearchResult {
	out := make([]SearchResult, 0, len(in))
	maxBytes := int64(opts.MaxSizeGB * 1e9)
	for _, r := range in {
		if opts.ExcludeCam && r.IsCam {
			continue
		}
		if isExcludedResolution(r.Resolution, opts.ExcludeResolutions) {
			continue
		}
		if maxBytes > 0 && r.SizeBytes > maxBytes {
			continue
		}
		out = append(out, r)
	}

	pref := strings.ToLower(strings.TrimSpace(opts.PreferredLanguage))
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := resolutionRank(out[i].Resolution), resolutionRank(out[j].Resolution)
		if ri != rj {
			return ri > rj
		}
		if out[i].Seeders != out[j].Seeders {
			return out[i].Seeders > out[j].Seeders
		}
		if pref != "" {
			pi, pj := hasLanguage(out[i], pref), hasLanguage(out[j], pref)
			if pi != pj {
				return pi // preferred-language result first
			}
		}
		// Smaller size wins the final tiebreak; unknown sizes (0) sink so
		// real, sized releases are preferred.
		si, sj := normalizedSize(out[i].SizeBytes), normalizedSize(out[j].SizeBytes)
		return si < sj
	})

	if opts.MaxPerResolution > 0 {
		out = capPerResolution(out, opts.MaxPerResolution)
	}
	return out
}

// capPerResolution keeps at most n results per resolution group, preserving
// the already-sorted order.
func capPerResolution(in []SearchResult, n int) []SearchResult {
	counts := make(map[string]int)
	out := in[:0]
	for _, r := range in {
		if counts[r.Resolution] >= n {
			continue
		}
		counts[r.Resolution]++
		out = append(out, r)
	}
	return out
}

func isExcludedResolution(res string, excluded []string) bool {
	if res == "" {
		return false
	}
	for _, e := range excluded {
		if strings.EqualFold(strings.TrimSpace(e), res) {
			return true
		}
	}
	return false
}

func hasLanguage(r SearchResult, pref string) bool {
	for _, l := range r.Languages {
		if strings.ToLower(l) == pref {
			return true
		}
	}
	return false
}

// resolutionRank orders resolutions for sorting (higher is better).
func resolutionRank(res string) int {
	switch res {
	case "2160p":
		return 5
	case "1440p":
		return 4
	case "1080p":
		return 3
	case "720p":
		return 2
	case "480p":
		return 1
	default:
		return 0
	}
}

// normalizedSize maps an unknown size (0) to a very large value so it sorts
// last in the "smaller first" tiebreak.
func normalizedSize(bytes int64) int64 {
	if bytes <= 0 {
		return 1<<63 - 1
	}
	return bytes
}
