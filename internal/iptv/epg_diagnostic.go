package iptv

import "hubplay/internal/db"

// sampleTvgIDs returns up to `max` non-empty tvg-id strings from a
// channel list, for diagnostic logs. Pure helper so it's easy to
// unit-test (see epg_diagnostic_test.go).
func sampleTvgIDs(channels []*db.Channel, max int) []string {
	if max <= 0 {
		return nil
	}
	out := make([]string, 0, max)
	for _, ch := range channels {
		if ch == nil || ch.TvgID == "" {
			continue
		}
		out = append(out, ch.TvgID)
		if len(out) >= max {
			break
		}
	}
	return out
}

// countBlankTvgIDs reports how many channels lack a tvg-id. This is
// the most actionable signal for the "zero-match" warning: if the
// number is high, the operator's M3U is missing tvg-id attributes
// and no XMLTV match could ever succeed.
func countBlankTvgIDs(channels []*db.Channel) int {
	n := 0
	for _, ch := range channels {
		if ch == nil || ch.TvgID == "" {
			n++
		}
	}
	return n
}
