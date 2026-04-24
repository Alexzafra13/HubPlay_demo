package iptv

import (
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

func TestParseXMLTV_Basic(t *testing.T) {
	input := `<?xml version="1.0" encoding="UTF-8"?>
<tv>
  <channel id="bbc1.uk">
    <display-name>BBC One</display-name>
    <icon src="http://logo.com/bbc1.png"/>
  </channel>
  <channel id="bbc2.uk">
    <display-name>BBC Two</display-name>
  </channel>
  <programme start="20260314180000 +0000" stop="20260314190000 +0000" channel="bbc1.uk">
    <title>News at Six</title>
    <desc>Evening news bulletin</desc>
    <category>News</category>
    <icon src="http://img.com/news.png"/>
  </programme>
  <programme start="20260314190000 +0000" stop="20260314200000 +0000" channel="bbc1.uk">
    <title>EastEnders</title>
    <desc>Drama series</desc>
    <category>Drama</category>
  </programme>
</tv>`

	data, err := ParseXMLTV(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	if len(data.Channels) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(data.Channels))
	}

	if data.Channels[0].ID != "bbc1.uk" {
		t.Errorf("channel ID = %q", data.Channels[0].ID)
	}
	if data.Channels[0].Name != "BBC One" {
		t.Errorf("channel name = %q", data.Channels[0].Name)
	}
	if data.Channels[0].Icon != "http://logo.com/bbc1.png" {
		t.Errorf("channel icon = %q", data.Channels[0].Icon)
	}

	if len(data.Programs) != 2 {
		t.Fatalf("expected 2 programs, got %d", len(data.Programs))
	}

	prog := data.Programs[0]
	if prog.ChannelID != "bbc1.uk" {
		t.Errorf("program channel = %q", prog.ChannelID)
	}
	if prog.Title != "News at Six" {
		t.Errorf("program title = %q", prog.Title)
	}
	if prog.Description != "Evening news bulletin" {
		t.Errorf("program desc = %q", prog.Description)
	}
	if prog.Category != "News" {
		t.Errorf("program category = %q", prog.Category)
	}
	if prog.IconURL != "http://img.com/news.png" {
		t.Errorf("program icon = %q", prog.IconURL)
	}
	if prog.Start.Year() != 2026 || prog.Start.Month() != 3 || prog.Start.Day() != 14 {
		t.Errorf("program start = %v", prog.Start)
	}
	if prog.Stop.Hour() != 19 {
		t.Errorf("program stop hour = %d", prog.Stop.Hour())
	}
}

func TestParseXMLTV_Empty(t *testing.T) {
	input := `<?xml version="1.0"?><tv></tv>`
	data, err := ParseXMLTV(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Channels) != 0 || len(data.Programs) != 0 {
		t.Error("expected empty data")
	}
}

func TestParseXMLTV_SkipsInvalidTimes(t *testing.T) {
	input := `<?xml version="1.0"?>
<tv>
  <programme start="invalid" stop="20260314190000" channel="ch1">
    <title>Bad Start</title>
  </programme>
  <programme start="20260314190000" stop="20260314200000" channel="ch1">
    <title>Good One</title>
  </programme>
</tv>`

	data, err := ParseXMLTV(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Programs) != 1 {
		t.Fatalf("expected 1 program (skipping bad time), got %d", len(data.Programs))
	}
	if data.Programs[0].Title != "Good One" {
		t.Errorf("wrong program kept: %q", data.Programs[0].Title)
	}
}

// ── Streaming parser ──────────────────────────────────────────────

// recordingHandler is a test helper that captures every callback the
// streaming parser fires, in order, so the test can assert both
// counts and ordering.
type recordingHandler struct {
	channels   []EPGChannel
	programmes []EPGProgram
	channelErr error
	progErr    error
}

func (h *recordingHandler) OnChannel(ch EPGChannel) error {
	h.channels = append(h.channels, ch)
	return h.channelErr
}
func (h *recordingHandler) OnProgramme(p EPGProgram) error {
	h.programmes = append(h.programmes, p)
	return h.progErr
}

func TestParseXMLTVStream_DispatchesElementsInOrder(t *testing.T) {
	input := `<?xml version="1.0"?>
<tv>
  <channel id="bbc1.uk"><display-name>BBC One</display-name></channel>
  <channel id="bbc2.uk"><display-name>BBC Two</display-name></channel>
  <programme start="20260314180000 +0000" stop="20260314190000 +0000" channel="bbc1.uk">
    <title>News</title>
  </programme>
  <programme start="20260314190000 +0000" stop="20260314200000 +0000" channel="bbc2.uk">
    <title>Drama</title>
  </programme>
</tv>`

	h := &recordingHandler{}
	skipped, err := ParseXMLTVStream(strings.NewReader(input), h)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if skipped != 0 {
		t.Errorf("unexpected skips: %d", skipped)
	}
	if len(h.channels) != 2 {
		t.Fatalf("channels: got %d, want 2", len(h.channels))
	}
	if h.channels[0].ID != "bbc1.uk" || h.channels[1].ID != "bbc2.uk" {
		t.Errorf("channel order: %v", h.channels)
	}
	if len(h.programmes) != 2 {
		t.Fatalf("programmes: got %d, want 2", len(h.programmes))
	}
	if h.programmes[0].Title != "News" || h.programmes[1].Title != "Drama" {
		t.Errorf("programme order: %v", h.programmes)
	}
}

func TestParseXMLTVStream_SkipsBadTimesAndCounts(t *testing.T) {
	input := `<?xml version="1.0"?>
<tv>
  <programme start="bogus" stop="20260314190000" channel="ch1"><title>Bad</title></programme>
  <programme start="20260314190000" stop="bogus" channel="ch1"><title>Bad2</title></programme>
  <programme start="20260314190000" stop="20260314200000" channel="ch1"><title>Good</title></programme>
</tv>`

	h := &recordingHandler{}
	skipped, err := ParseXMLTVStream(strings.NewReader(input), h)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 2 {
		t.Errorf("skipped count: got %d, want 2", skipped)
	}
	if len(h.programmes) != 1 || h.programmes[0].Title != "Good" {
		t.Errorf("kept wrong programmes: %v", h.programmes)
	}
}

func TestParseXMLTVStream_HandlerErrorAborts(t *testing.T) {
	input := `<?xml version="1.0"?>
<tv>
  <channel id="ch1"><display-name>One</display-name></channel>
  <channel id="ch2"><display-name>Two</display-name></channel>
</tv>`

	h := &recordingHandler{
		channelErr: fmt.Errorf("stop"),
	}
	_, err := ParseXMLTVStream(strings.NewReader(input), h)
	if err == nil {
		t.Fatal("expected handler error to surface")
	}
	// Aborts on the first channel — the second one must NOT be
	// dispatched. Cheap signal that we don't exhaust the document
	// before bailing.
	if len(h.channels) != 1 {
		t.Errorf("expected abort after 1 channel, got %d", len(h.channels))
	}
}

func TestParseXMLTVStream_RejectsMalformedXML(t *testing.T) {
	// Unclosed <tv> root → the decoder's Token() will return an
	// "unexpected EOF" or similar. We surface it as a parse error.
	input := `<?xml version="1.0"?><tv><channel id="x">`
	h := &recordingHandler{}
	_, err := ParseXMLTVStream(strings.NewReader(input), h)
	if err == nil {
		t.Fatal("expected parse error on malformed XML")
	}
}

// boundedAllocReader counts the bytes read so the streaming-memory
// test can sanity-check that the parser doesn't slurp the whole
// document before dispatching the first element.
type boundedAllocReader struct {
	src     io.Reader
	maxRead int
}

func (b *boundedAllocReader) Read(p []byte) (int, error) {
	if len(p) > b.maxRead {
		p = p[:b.maxRead]
	}
	return b.src.Read(p)
}

func TestParseXMLTVStream_StreamsLargeFeedWithoutBuffering(t *testing.T) {
	// Synthesise a feed with 50k programmes. The eager parser would
	// allocate 50k xmlTVProgramme structs in a slice up front. The
	// streaming path must dispatch each one and let it go before
	// reading the next, so the handler observes the count without
	// the test process ballooning.
	//
	// We assert the dispatch shape — counting + last-programme check.
	// Memory bound is exercised implicitly by the fact that this
	// test doesn't OOM under -race with 50k entries.
	const n = 50_000
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><tv>`)
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, `<programme start="20260314180000 +0000" stop="20260314190000 +0000" channel="ch%d"><title>p%d</title></programme>`, i, i)
	}
	b.WriteString(`</tv>`)

	count := 0
	lastTitle := ""
	h := &countingHandler{
		onProg: func(p EPGProgram) {
			count++
			lastTitle = p.Title
		},
	}
	skipped, err := ParseXMLTVStream(&boundedAllocReader{
		src: strings.NewReader(b.String()), maxRead: 4096,
	}, h)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if skipped != 0 {
		t.Errorf("unexpected skips: %d", skipped)
	}
	if count != n {
		t.Errorf("dispatched %d programmes, want %d", count, n)
	}
	if lastTitle != fmt.Sprintf("p%d", n-1) {
		t.Errorf("last programme: %q", lastTitle)
	}
}

// countingHandler is intentionally lightweight — no slice growth, no
// retention — so the streaming-large-feed test isolates the parser's
// memory behaviour, not the handler's.
type countingHandler struct {
	onCh   func(EPGChannel)
	onProg func(EPGProgram)
}

func (c *countingHandler) OnChannel(ch EPGChannel) error {
	if c.onCh != nil {
		c.onCh(ch)
	}
	return nil
}
func (c *countingHandler) OnProgramme(p EPGProgram) error {
	if c.onProg != nil {
		c.onProg(p)
	}
	return nil
}

func TestParseXMLTVStream_AcceptsAnyCharsetDeclaration(t *testing.T) {
	// Real XMLTV feeds occasionally claim charsets the Go stdlib
	// doesn't know (e.g. "ISO-8859-1") while shipping UTF-8 bytes.
	// The CharsetReader wired into the decoder must pass them through
	// unchanged so the parse doesn't bail.
	input := `<?xml version="1.0" encoding="ISO-8859-1"?>
<tv><channel id="x"><display-name>Test</display-name></channel></tv>`
	h := &recordingHandler{}
	if _, err := ParseXMLTVStream(strings.NewReader(input), h); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(h.channels) != 1 {
		t.Errorf("expected 1 channel, got %d", len(h.channels))
	}
}

func TestParseXMLTVStream_DropsEmptyDisplayNames(t *testing.T) {
	// Some feeds emit empty <display-name/> elements to pad. The
	// matcher copes with extras but a clean drop here keeps the
	// candidate list tight, which makes fuzzy matching cheaper.
	input := `<?xml version="1.0"?>
<tv><channel id="x">
  <display-name>Real</display-name>
  <display-name></display-name>
  <display-name>Alt</display-name>
</channel></tv>`
	h := &recordingHandler{}
	if _, err := ParseXMLTVStream(strings.NewReader(input), h); err != nil {
		t.Fatal(err)
	}
	if len(h.channels) != 1 {
		t.Fatalf("channels: %d", len(h.channels))
	}
	got := h.channels[0].DisplayNames
	if len(got) != 2 || got[0] != "Real" || got[1] != "Alt" {
		t.Errorf("display-names: %v", got)
	}
}

// Sanity: the eager ParseXMLTV result must match what the streaming
// parser produces when fed through a collector. Pin this so the two
// surfaces can't drift.
func TestParseXMLTV_MatchesStreamingThroughCollector(t *testing.T) {
	input := `<?xml version="1.0"?>
<tv>
  <channel id="ch1"><display-name>One</display-name></channel>
  <programme start="20260314180000" stop="20260314190000" channel="ch1"><title>T1</title></programme>
  <programme start="20260314190000" stop="20260314200000" channel="ch1"><title>T2</title></programme>
</tv>`

	eager, err := ParseXMLTV(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	h := &recordingHandler{}
	if _, err := ParseXMLTVStream(strings.NewReader(input), h); err != nil {
		t.Fatal(err)
	}

	if len(eager.Channels) != len(h.channels) || len(eager.Programs) != len(h.programmes) {
		t.Fatalf("count mismatch: eager=(%d,%d) stream=(%d,%d)",
			len(eager.Channels), len(eager.Programs),
			len(h.channels), len(h.programmes))
	}
	for i := range eager.Channels {
		if eager.Channels[i].ID != h.channels[i].ID {
			t.Errorf("channel[%d] mismatch", i)
		}
	}
	for i := range eager.Programs {
		if !eager.Programs[i].Start.Equal(h.programmes[i].Start) {
			t.Errorf("programme[%d] start mismatch", i)
		}
		if eager.Programs[i].Title != h.programmes[i].Title {
			t.Errorf("programme[%d] title mismatch", i)
		}
	}
}

// avoid unused-time-import warning if the file is ever trimmed.
var _ = time.Time{}

func TestParseXMLTVTime(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"20260314180000 +0000", false},
		{"20260314180000 +0100", false},
		{"20260314180000", false},
		{"invalid", true},
		{"", true},
	}

	for _, tc := range tests {
		_, err := parseXMLTVTime(tc.input)
		if (err != nil) != tc.wantErr {
			t.Errorf("parseXMLTVTime(%q): err=%v, wantErr=%v", tc.input, err, tc.wantErr)
		}
	}
}
