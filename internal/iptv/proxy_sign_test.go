package iptv

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"log/slog"
)

// TestProxySign_RoundTrip fija el cierre del olor A3: las URLs que el
// proxy reescribe en la playlist llevan una firma que VerifyProxySig
// acepta, usando exactamente el mismo valor `url` que el handler extrae
// (Go decodifica el query param una vez), y rechaza firmas ausentes,
// manipuladas o de otro canal.
func TestProxySign_RoundTrip(t *testing.T) {
	p := NewStreamProxy(slog.Default())

	const channelID = "chan-1"
	// Playlist con un segmento relativo + uno absoluto en otro CDN (caso
	// multi-CDN que un host-lock ingenuo rompería).
	body := []byte(strings.Join([]string{
		"#EXTM3U",
		"#EXTINF:6,",
		"seg-00001.ts",
		"#EXTINF:6,",
		"https://cdn2.example.net/path/seg-00002.ts?token=abc",
		"",
	}, "\n"))

	rr := httptest.NewRecorder()
	if err := p.serveRewrittenPlaylistBody(rr, body, channelID, "https://cdn1.example.com/live/master.m3u8"); err != nil {
		t.Fatalf("serveRewrittenPlaylistBody: %v", err)
	}
	out := rr.Body.String()

	// Cada línea proxiada debe llevar &sig= y verificar contra el valor
	// `url` ya decodificado (lo que ve el handler).
	var checked int
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "/proxy?url=") {
			continue
		}
		checked++
		q := line[strings.Index(line, "?")+1:]
		vals, err := url.ParseQuery(q)
		if err != nil {
			t.Fatalf("parse query %q: %v", q, err)
		}
		gotURL := vals.Get("url") // == lo que devuelve r.URL.Query().Get("url")
		sig := vals.Get("sig")
		if sig == "" {
			t.Errorf("línea sin sig: %s", line)
		}
		if !p.VerifyProxySig(channelID, gotURL, sig) {
			t.Errorf("VerifyProxySig falló para url=%q sig=%q", gotURL, sig)
		}
		// Firma de OTRO canal no debe valer.
		if p.VerifyProxySig("otro-canal", gotURL, sig) {
			t.Errorf("la firma no debería valer para otro canal: url=%q", gotURL)
		}
		// Sig manipulada no debe valer.
		if p.VerifyProxySig(channelID, gotURL, sig+"00") {
			t.Errorf("sig manipulada aceptada para url=%q", gotURL)
		}
	}
	if checked < 2 {
		t.Fatalf("esperaba al menos 2 URLs proxiadas firmadas, vi %d\n%s", checked, out)
	}
}

func TestVerifyProxySig_RejectsEmptyAndForged(t *testing.T) {
	p := NewStreamProxy(slog.Default())
	const ch = "c-1"
	raw := "https://cdn.example.com/seg.ts"

	if p.VerifyProxySig(ch, raw, "") {
		t.Error("sig vacía debería rechazarse")
	}
	if p.VerifyProxySig(ch, raw, "deadbeef") {
		t.Error("sig forjada debería rechazarse")
	}
	// La firma legítima sí pasa.
	good := p.signProxyURL(ch, raw)
	if !p.VerifyProxySig(ch, raw, good) {
		t.Error("la firma legítima debería pasar")
	}
	// Dos proxies distintos tienen claves distintas → no intercambiables.
	p2 := NewStreamProxy(slog.Default())
	if p2.VerifyProxySig(ch, raw, good) {
		t.Error("firma de otra instancia (otra clave) no debería valer")
	}
}
