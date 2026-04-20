package iptv

import (
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"time"
)

// EPGData represents parsed EPG data from an XMLTV file.
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

// xmlTV is the root XMLTV element.
type xmlTV struct {
	XMLName    xml.Name         `xml:"tv"`
	Channels   []xmlTVChannel  `xml:"channel"`
	Programmes []xmlTVProgramme `xml:"programme"`
}

type xmlTVChannel struct {
	ID          string         `xml:"id,attr"`
	DisplayName []xmlTVText    `xml:"display-name"`
	Icon        []xmlTVIcon    `xml:"icon"`
}

type xmlTVProgramme struct {
	Start   string         `xml:"start,attr"`
	Stop    string         `xml:"stop,attr"`
	Channel string         `xml:"channel,attr"`
	Title   []xmlTVText    `xml:"title"`
	Desc    []xmlTVText    `xml:"desc"`
	Category []xmlTVText   `xml:"category"`
	Icon    []xmlTVIcon    `xml:"icon"`
}

type xmlTVText struct {
	Lang string `xml:"lang,attr"`
	Text string `xml:",chardata"`
}

type xmlTVIcon struct {
	Src string `xml:"src,attr"`
}

// ParseXMLTV parses an XMLTV document from a reader.
func ParseXMLTV(r io.Reader) (*EPGData, error) {
	var raw xmlTV
	decoder := xml.NewDecoder(r)
	decoder.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) {
		return input, nil // Accept any charset
	}

	if err := decoder.Decode(&raw); err != nil {
		return nil, fmt.Errorf("parse XMLTV: %w", err)
	}

	data := &EPGData{}

	// Parse channels
	for _, ch := range raw.Channels {
		epgCh := EPGChannel{ID: ch.ID}
		if len(ch.DisplayName) > 0 {
			epgCh.Name = ch.DisplayName[0].Text
			epgCh.DisplayNames = make([]string, 0, len(ch.DisplayName))
			for _, dn := range ch.DisplayName {
				if dn.Text != "" {
					epgCh.DisplayNames = append(epgCh.DisplayNames, dn.Text)
				}
			}
		}
		if len(ch.Icon) > 0 {
			epgCh.Icon = ch.Icon[0].Src
		}
		data.Channels = append(data.Channels, epgCh)
	}

	// Parse programmes
	skippedPrograms := 0
	for _, p := range raw.Programmes {
		start, err := parseXMLTVTime(p.Start)
		if err != nil {
			skippedPrograms++
			title := ""
			if len(p.Title) > 0 {
				title = p.Title[0].Text
			}
			slog.Warn("skipping XMLTV programme with unparseable start time",
				"channel", p.Channel,
				"title", title,
				"start", p.Start,
				"error", err,
			)
			continue
		}
		stop, err := parseXMLTVTime(p.Stop)
		if err != nil {
			skippedPrograms++
			title := ""
			if len(p.Title) > 0 {
				title = p.Title[0].Text
			}
			slog.Warn("skipping XMLTV programme with unparseable stop time",
				"channel", p.Channel,
				"title", title,
				"stop", p.Stop,
				"error", err,
			)
			continue
		}

		prog := EPGProgram{
			ChannelID: p.Channel,
			Start:     start,
			Stop:      stop,
		}
		if len(p.Title) > 0 {
			prog.Title = p.Title[0].Text
		}
		if len(p.Desc) > 0 {
			prog.Description = p.Desc[0].Text
		}
		if len(p.Category) > 0 {
			prog.Category = p.Category[0].Text
		}
		if len(p.Icon) > 0 {
			prog.IconURL = p.Icon[0].Src
		}

		data.Programs = append(data.Programs, prog)
	}

	if skippedPrograms > 0 {
		slog.Warn("XMLTV parsing completed with skipped programmes",
			"total", len(raw.Programmes),
			"skipped", skippedPrograms,
			"parsed", len(data.Programs),
		)
	}

	return data, nil
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
