//go:build !wasm

package core

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const minimalRegexes = `
user_agent_parsers:
  - regex: '(TestBrowser)/(\d+)\.(\d+)'
os_parsers:
  - regex: '(TestOS) (\d+)'
device_parsers:
  - regex: '(TestDevice)'
    device_replacement: 'TestDevice'
`

// A successful update must swap the DB, purge the cache, and bump the
// generation so in-flight parses cannot re-cache stale results.
func TestUpdaterSwapAndPurge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		w.Write([]byte(minimalRegexes))
	}))
	defer srv.Close()

	p, err := New(Config{DisableAutoUpdate: true, LRUCacheSize: 10, UpdateURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Close()

	// Warm the cache with the embedded DB.
	ua := "TestBrowser/1.2 something"
	before := p.Parse(ua, nil)
	if before.Browser.Name == "TestBrowser" {
		t.Fatalf("Embedded DB unexpectedly knows TestBrowser")
	}

	genBefore := p.gen.Load()
	p.updateRegexes()

	if p.gen.Load() != genBefore+1 {
		t.Errorf("Generation not bumped on swap: %d -> %d", genBefore, p.gen.Load())
	}
	if p.lastETag != `"v1"` {
		t.Errorf("ETag not recorded, got %q", p.lastETag)
	}

	after := p.Parse(ua, nil)
	if after.Browser.Name != "TestBrowser" {
		t.Errorf("Cache/DB not refreshed after update: browser %q (stale cache entry?)", after.Browser.Name)
	}
}

// A 304 response must be a no-op.
func TestUpdaterNotModified(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Write([]byte(minimalRegexes))
	}))
	defer srv.Close()

	p, err := New(Config{DisableAutoUpdate: true, UpdateURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Close()

	p.updateRegexes() // 200, stores ETag
	gen := p.gen.Load()
	p.updateRegexes() // 304, no swap
	if p.gen.Load() != gen {
		t.Errorf("304 response must not bump the generation")
	}
	if calls != 2 {
		t.Errorf("Expected 2 fetches, got %d", calls)
	}
}

// Malformed payloads and error statuses must leave the parser untouched.
func TestUpdaterRejectsBadPayloads(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"garbage", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("!!! not yaml [ or json"))
		}},
		{"http500", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			p, err := New(Config{DisableAutoUpdate: true, UpdateURL: srv.URL})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			defer p.Close()

			gen := p.gen.Load()
			p.updateRegexes()
			if p.gen.Load() != gen {
				t.Errorf("Bad payload must not swap the DB")
			}
			// The parser must still work.
			if res := p.Parse("Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/126.0.0.0", nil); res == nil {
				t.Error("Parser broken after rejected update")
			}
		})
	}
}

// Oversized responses must be refused (OOM guard).
func TestUpdaterSizeCap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(strings.Repeat("a", maxRegexesSize+2)))
	}))
	defer srv.Close()

	p, err := New(Config{DisableAutoUpdate: true, UpdateURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Close()

	gen := p.gen.Load()
	p.updateRegexes()
	if p.gen.Load() != gen {
		t.Errorf("Oversized payload must be refused")
	}
}

// A non-positive interval must fall back to the default instead of feeding
// time.NewTicker a value it panics on (previously an infinite panic loop).
func TestUpdaterZeroIntervalDoesNotPanicLoop(t *testing.T) {
	p, err := New(Config{UpdateInterval: "0s", UpdateURL: "http://127.0.0.1:0/never"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Give the updater goroutine a moment to start (and previously: to panic).
	time.Sleep(50 * time.Millisecond)
	p.Close()
}

// jitter must stay within ±10% of the interval and never be non-positive.
func TestJitterBounds(t *testing.T) {
	d := 10 * time.Hour
	for i := 0; i < 1000; i++ {
		j := jitter(d)
		if j < d-d/10 || j > d+d/10 {
			t.Fatalf("jitter out of bounds: %v for %v", j, d)
		}
		if j <= 0 {
			t.Fatalf("non-positive jitter: %v", j)
		}
	}
}
