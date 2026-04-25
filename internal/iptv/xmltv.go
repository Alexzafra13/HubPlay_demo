package iptv

// XMLTV parser. Two surfaces:
//
//   - ParseXMLTVStream — token-based streaming. Holds at most one
//     <channel> or <programme> in memory at a time. Use this when
//     the feed can grow large (davidmuma's "guiatv.xml.gz" is 200+
//     MB uncompressed; some upstreams ship 2 GB feeds). Memory
//     usage is bounded by the size of one element regardless of
//     how many programmes the feed contains.
//
//   - ParseXMLTV — convenience wrapper that materialises the whole
//     EPGData in memory. Kept for tests and short-feed callers.
//     Internally uses ParseXMLTVStream.
//
// The streaming path is the only one used at runtime by the EPG
// refresher (service_epg.go) — reading the feed should never push
// the binary's resident set above ~tens of MB even for the worst
// upstream.

import (
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"time"
)

// EPGData represents parsed EPG data from an XMLTV file. Returned by
// the eager ParseXMLTV; the streaming path produces these elements
// one at a time via the handler.
type EPGData struct {
	Channels []EPGChannel
	Programs []EPGProgram
}

// EPGChannel represents a channel definition from XMLTV.
//
// XMLTV files typically list multiple `<display-name>` entries per
// channel — e.g. "La 1", "La 1 HD", "La 1.TV" — so a single playlist
// can match against any of the aliases. We keep them all; the matcher
// in service.go tries each variant before giving up.
type EPGChannel struct {
	ID           string
	Name         string   // first display-name (kept for backwards-compat)
	DisplayNames []string // all display-names, in source order
	Icon         string
}

// EPGProgram represents a program entry from XMLTV.
type EPGProgram struct {
	ChannelID   string
	Title       string
	Description string
	Category    string
	IconURL     string
	Start       time.Time
	Stop        time.Time
}

// EPGStreamHandler receives parsed elements from ParseXMLTVStream as
// the decoder sees them. Channel definitions arrive first (assuming
// the conventional XMLTV ordering); programmes follow. Returning a
// non-nil error from either method aborts the parse with that error
// surfaced.
//
// The same EPGProgram value MUST NOT be retained beyond the call —
// the parser may reuse internal buffers between invocations. Copy
// any field you need to keep.
type EPGStreamHandler interface {
	OnChannel(EPGChannel) error
	OnProgramme(EPGProgram) error
}

// xmlTVChannel / xmlTVProgramme / xmlTVText / xmlTVIcon describe the
// individual elements DecodeElement parses one-at-a-time. There is
// intentionally no `xmlTV` root struct — that would invite eager
// decoding of the whole document.

type xmlTVChannel struct {
	ID          string      `xml:"id,attr"`
	DisplayName []xmlTVText `xml:"display-name"`
	Icon        []xmlTVIcon `xml:"icon"`
}

type xmlTVProgramme struct {
	Start    string      `xml:"start,attr"`
	Stop     string      `xml:"stop,attr"`
	Channel  string      `xml:"channel,attr"`
	Title    []xmlTVText `xml:"title"`
	Desc     []xmlTVText `xml:"desc"`
	Category []xmlTVText `xml:"category"`
	Icon     []xmlTVIcon `xml:"icon"`
}

type xmlTVText struct {
	Lang string `xml:"lang,attr"`
	Text string `xml:",chardata"`
}

type xmlTVIcon struct {
	Src string `xml:"src,attr"`
}

// ParseXMLTVStream walks the XMLTV document token-by-token. Memory
// usage is bounded by the size of one <channel> or <programme>
// element regardless of how many programmes the feed contains.
//
// Programmes with unparseable start/stop attributes are dropped with
// a warning log and reflected in the returned skippedPrograms count
// — same semantics as the eager parser.
//
// Aborts on the first hard XML error (malformed structure, mismatched
// tags). Does NOT abort on element-level decode failures — those are
// reported via the handler-style log + skip path.
func ParseXMLTVStream(r io.Reader, h EPGStreamHandler) (skippedPrograms int, err error) {
	dec := xml.NewDecoder(r)
	dec.CharsetReader = func(_ string, in io.Reader) (io.Reader, error) {
		// Accept any charset declaration. Real-world XMLTV feeds
		// occasionally claim "ISO-8859-1" but ship UTF-8; the Go
		// stdlib chokes on the unknown charset before getting to
		// the bytes, so we pass through unchanged.
		return in, nil
	}

	for {
		tok, tokErr := dec.Token()
		if tokErr == io.EOF {
			return skippedPrograms, nil
		}
		if tokErr != nil {
			return skippedPrograms, fmt.Errorf("parse XMLTV: %w", tokErr)
		}

		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}

		switch se.Name.Local {
		case "channel":
			var raw xmlTVChannel
			if dErr := dec.DecodeElement(&raw, &se); dErr != nil {
				return skippedPrograms, fmt.Errorf("decode channel: %w", dErr)
			}
			if hErr := h.OnChannel(toEPGChannel(raw)); hErr != nil {
				return skippedPrograms, hErr
			}

		case "programme":
			var raw xmlTVProgramme
			if dErr := dec.DecodeElement(&raw, &se); dErr != nil {
				return skippedPrograms, fmt.Errorf("decode programme: %w", dErr)
			}
			prog, dropReason := toEPGProgram(raw)
			if dropReason != "" {
				skippedPrograms++
				slog.Warn("skipping XMLTV programme",
					"channel", raw.Channel,
					"reason", dropReason,
					"start", raw.Start,
					"stop", raw.Stop,
				)
				continue
			}
			if hErr := h.OnProgramme(prog); hErr != nil {
				return skippedPrograms, hErr
			}
		}
	}
}

// ParseXMLTV parses an XMLTV document from a reader and returns the
// full data set in memory. Convenience wrapper around the streaming
// parser kept for short-feed callers and tests; runtime EPG refresh
// uses the streaming path directly to avoid the materialisation cost.
func ParseXMLTV(r io.Reader) (*EPGData, error) {
	c := &collectingHandler{}
	skipped, err := ParseXMLTVStream(r, c)
	if err != nil {
		return nil, err
	}
	if skipped > 0 {
		slog.Warn("XMLTV parsing completed with skipped programmes",
			"skipped", skipped, "parsed", len(c.programs))
	}
	return &EPGData{Channels: c.channels, Programs: c.programs}, nil
}

// collectingHandler accumulates channels + programmes for the eager
// ParseXMLTV path. Lives in the parser file so the test suite for
// the eager API stays untouched.
type collectingHandler struct {
	channels []EPGChannel
	programs []EPGProgram
}

func (c *collectingHandler) OnChannel(ch EPGChannel) error {
	c.channels = append(c.channels, ch)
	return nil
}

func (c *collectingHandler) OnProgramme(p EPGProgram) error {
	c.programs = append(c.programs, p)
	return nil
}

// toEPGChannel projects the XML-decoded element into the public
// shape. Skips empty display-names; the matcher already tolerates
// missing aliases.
func toEPGChannel(raw xmlTVChannel) EPGChannel {
	out := EPGChannel{ID: raw.ID}
	if len(raw.DisplayName) > 0 {
		out.Name = raw.DisplayName[0].Text
		out.DisplayNames = make([]string, 0, len(raw.DisplayName))
		for _, dn := range raw.DisplayName {
			if dn.Text != "" {
				out.DisplayNames = append(out.DisplayNames, dn.Text)
			}
		}
	}
	if len(raw.Icon) > 0 {
		out.Icon = raw.Icon[0].Src
	}
	return out
}

// toEPGProgram projects the XML-decoded programme into the public
// shape, parsing time attributes. Returns a non-empty dropReason when
// the programme should be skipped — the caller logs + counts.
func toEPGProgram(raw xmlTVProgramme) (EPGProgram, string) {
	start, err := parseXMLTVTime(raw.Start)
	if err != nil {
		return EPGProgram{}, "unparseable start time"
	}
	stop, err := parseXMLTVTime(raw.Stop)
	if err != nil {
		return EPGProgram{}, "unparseable stop time"
	}
	prog := EPGProgram{
		ChannelID: raw.Channel,
		Start:     start,
		Stop:      stop,
	}
	if len(raw.Title) > 0 {
		prog.Title = raw.Title[0].Text
	}
	if len(raw.Desc) > 0 {
		prog.Description = raw.Desc[0].Text
	}
	if len(raw.Category) > 0 {
		prog.Category = raw.Category[0].Text
	}
	if len(raw.Icon) > 0 {
		prog.IconURL = raw.Icon[0].Src
	}
	return prog, ""
}

// parseXMLTVTime parses XMLTV time format: "20060102150405 -0700" or "20060102150405".
func parseXMLTVTime(s string) (time.Time, error) {
	// Try with timezone offset
	if t, err := time.Parse("20060102150405 -0700", s); err == nil {
		return t, nil
	}
	// Try without timezone
	if t, err := time.Parse("20060102150405", s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid XMLTV time: %q", s)
}
