package iptv

import iptvmodel "hubplay/internal/iptv/model"

// sampleTvgIDs devuelve hasta `max` tvg-ids no vacíos para logs diagnósticos.
func sampleTvgIDs(channels []*iptvmodel.Channel, max int) []string {
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

// countBlankTvgIDs cuenta canales sin tvg-id. Si el número es alto,
// el M3U no trae tvg-id y ningún match XMLTV puede funcionar.
func countBlankTvgIDs(channels []*iptvmodel.Channel) int {
	n := 0
	for _, ch := range channels {
		if ch == nil || ch.TvgID == "" {
			n++
		}
	}
	return n
}
