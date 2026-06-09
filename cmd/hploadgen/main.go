// Command hploadgen drives an authenticated read workload against a
// running HubPlay server so the catalogue / browse / search hot paths can
// be measured (throughput + latency) while a pprof profile is captured.
//
// It logs in once, then fans out N workers that hit a weighted mix of the
// endpoints a browsing client exercises. Pair it with a CPU/heap profile:
//
//	# terminal 1 — capture 30s CPU profile (needs observability.pprof_enabled + metrics_token)
//	curl -H "Authorization: Bearer $TOKEN" \
//	  "http://localhost:8096/debug/pprof/profile?seconds=30" -o cpu.prof
//	# terminal 2 — drive load for 32s
//	go run ./cmd/hploadgen -url http://localhost:8096 -user admin -pass hubplay123 -duration 32s
//	# then: go tool pprof -http=: ./hubplay cpu.prof
//
// No third-party dependencies, so it runs anywhere the Go toolchain does.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

type weightedPath struct {
	path   string
	weight int
}

func main() {
	base := flag.String("url", "http://localhost:8096", "server base URL")
	dur := flag.Duration("duration", 30*time.Second, "load duration")
	workers := flag.Int("workers", 50, "concurrent workers")
	user := flag.String("user", "admin", "username")
	pass := flag.String("pass", "hubplay123", "password")
	flag.Parse()

	token, err := login(*base, *user, *pass)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hploadgen: login failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("logged in, driving load...")

	// Weighted mix mirroring a browsing session: lots of grid/list
	// fetches, fewer searches.
	mix := []weightedPath{
		{"/api/v1/items?limit=50", 10},
		{"/api/v1/items?limit=50&offset=200", 4},
		{"/api/v1/items/latest?limit=20", 4},
		{"/api/v1/items/search?q=Movie&limit=50", 3},
		{"/api/v1/items/genres", 2},
		{"/api/v1/libraries", 2},
	}
	var plan []string
	for _, m := range mix {
		for i := 0; i < m.weight; i++ {
			plan = append(plan, m.path)
		}
	}

	var ok, errs, codeNon2xx int64
	deadline := time.Now().Add(*dur)
	client := &http.Client{Timeout: 10 * time.Second}
	var wg sync.WaitGroup
	for w := 0; w < *workers; w++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			i := seed
			for time.Now().Before(deadline) {
				p := plan[i%len(plan)]
				i++
				req, _ := http.NewRequest(http.MethodGet, *base+p, nil)
				req.Header.Set("Authorization", "Bearer "+token)
				resp, err := client.Do(req)
				if err != nil {
					atomic.AddInt64(&errs, 1)
					continue
				}
				io.Copy(io.Discard, resp.Body) //nolint:errcheck
				resp.Body.Close()
				if resp.StatusCode >= 200 && resp.StatusCode < 300 {
					atomic.AddInt64(&ok, 1)
				} else {
					atomic.AddInt64(&codeNon2xx, 1)
				}
			}
		}(w)
	}
	wg.Wait()

	total := ok + codeNon2xx
	fmt.Printf("ok=%d non2xx=%d errors=%d duration=%s rps=%.0f\n",
		ok, codeNon2xx, errs, *dur, float64(total)/dur.Seconds())
}

func login(base, user, pass string) (string, error) {
	body, _ := json.Marshal(map[string]string{"username": user, "password": pass})
	resp, err := http.Post(base+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, string(b))
	}
	var out struct {
		AccessToken string `json:"access_token"`
		Data        struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.AccessToken != "" {
		return out.AccessToken, nil
	}
	if out.Data.AccessToken != "" {
		return out.Data.AccessToken, nil
	}
	return "", fmt.Errorf("no access_token in login response")
}
