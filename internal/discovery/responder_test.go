package discovery

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"testing"
	"time"
)

func startTestResponder(t *testing.T, cfg Config) *Responder {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r, err := Start(ctx, cfg, slog.Default())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if r == nil {
		t.Fatal("Start returned nil responder with Enabled=true")
	}
	return r
}

func probe(t *testing.T, port int, payload string) ([]byte, bool) {
	t.Helper()
	conn, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(payload)); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	buf := make([]byte, 512)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, false
	}
	return buf[:n], true
}

func TestResponderAnswersProbeWithAdvertisedPort(t *testing.T) {
	r := startTestResponder(t, Config{
		Enabled: true, Port: freeUDPPort(t), HTTPPort: 8096, AdvertisePort: 8097,
		Name: "Salón", Version: "test",
	})

	body, ok := probe(t, r.Port(), Magic)
	if !ok {
		t.Fatal("no reply to a valid probe")
	}
	var reply Reply
	if err := json.Unmarshal(body, &reply); err != nil {
		t.Fatalf("reply is not JSON: %v (%q)", err, body)
	}
	if reply.Product != "hubplay" {
		t.Errorf("product = %q, want hubplay", reply.Product)
	}
	if reply.Port != 8097 {
		t.Errorf("port = %d, want advertised 8097 (not the internal 8096)", reply.Port)
	}
	if reply.Name != "Salón" || reply.Version != "test" {
		t.Errorf("name/version = %q/%q", reply.Name, reply.Version)
	}
}

func TestResponderFallsBackToHTTPPort(t *testing.T) {
	r := startTestResponder(t, Config{Enabled: true, Port: freeUDPPort(t), HTTPPort: 8096})
	body, ok := probe(t, r.Port(), Magic+" extra")
	if !ok {
		t.Fatal("no reply to a probe with trailing data")
	}
	var reply Reply
	if err := json.Unmarshal(body, &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Port != 8096 {
		t.Errorf("port = %d, want 8096", reply.Port)
	}
	if reply.Name != "HubPlay" {
		t.Errorf("default name = %q", reply.Name)
	}
}

func TestResponderIgnoresForeignTraffic(t *testing.T) {
	r := startTestResponder(t, Config{Enabled: true, Port: freeUDPPort(t), HTTPPort: 8096})
	if _, ok := probe(t, r.Port(), "M-SEARCH * HTTP/1.1"); ok {
		t.Fatal("responder answered a non-HubPlay datagram")
	}
}

func TestResponderDisabledAndInvalid(t *testing.T) {
	ctx := context.Background()
	if r, err := Start(ctx, Config{Enabled: false}, slog.Default()); r != nil || err != nil {
		t.Fatalf("disabled: got %v, %v", r, err)
	}
	if _, err := Start(ctx, Config{Enabled: true}, slog.Default()); err == nil {
		t.Fatal("expected error without any http port")
	}
	if _, err := Start(ctx, Config{Enabled: true, HTTPPort: 1, Port: 70000}, slog.Default()); err == nil {
		t.Fatal("expected error for out-of-range udp port")
	}
}

func TestResponderStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r, err := Start(ctx, Config{Enabled: true, Port: freeUDPPort(t), HTTPPort: 8096}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	port := r.Port()
	cancel()
	time.Sleep(50 * time.Millisecond)
	if _, ok := probe(t, port, Magic); ok {
		t.Fatal("responder still answering after cancel")
	}
}

func TestResponderAdvertisesURLWhenConfigured(t *testing.T) {
	r := startTestResponder(t, Config{
		Enabled: true, Port: freeUDPPort(t), HTTPPort: 8096, AdvertiseURL: "https://hubplay.example.org",
	})
	body, ok := probe(t, r.Port(), Magic)
	if !ok {
		t.Fatal("no reply")
	}
	var reply Reply
	if err := json.Unmarshal(body, &reply); err != nil {
		t.Fatal(err)
	}
	if reply.URL != "https://hubplay.example.org" {
		t.Errorf("url = %q", reply.URL)
	}
}

// freeUDPPort pide un puerto efímero al SO: el puerto por defecto (41860)
// puede estar ocupado por un servidor de desarrollo en la misma máquina.
func freeUDPPort(t *testing.T) int {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	port := conn.LocalAddr().(*net.UDPAddr).Port
	_ = conn.Close()
	return port
}
