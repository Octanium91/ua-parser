package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Octanium91/ua-parser/pkg/core"
)

type ParseRequest struct {
	UA      string            `json:"ua"`
	Headers map[string]string `json:"headers"`
	// Signals carries optional browser-side evidence collected on the page
	// (max_touch_points, webgl_renderer, ...) — see the root README's
	// "Forwarding headers from your backend" section.
	Signals *core.Signals `json:"signals"`
}

// envOr returns the value of the environment variable key, or def if unset/empty.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// normalizePath ensures a route pattern is a valid absolute path (leading slash),
// so a value like "status" is accepted the same as "/status".
func normalizePath(p string) string {
	if p == "" {
		return "/"
	}
	if p[0] != '/' {
		return "/" + p
	}
	return p
}

// joinPath mounts sub under an optional base prefix. base "" (unset) leaves sub
// at the root, preserving legacy behavior; base "/api" shifts everything, e.g.
// joinPath("/api", "/health") -> "/api/health" and joinPath("/api", "/") -> "/api".
// Every endpoint is derived through this, so future endpoints shift with the base.
func joinPath(base, sub string) string {
	base = strings.TrimRight(normalizePath(base), "/") // "" (root) or "/api"
	full := base + normalizePath(sub)                  // "/", "/health", "/api/", "/api/health"
	if full != "/" {
		full = strings.TrimRight(full, "/") // "/api/" -> "/api"; leaves "/health" as-is
	}
	if full == "" {
		full = "/"
	}
	return full
}

func main() {
	port := os.Getenv("UA_PORT")
	if port == "" {
		port = "8080"
	}

	// UA_BASE_PATH is a single prefix under which every endpoint is mounted
	// (handy behind a reverse proxy). UA_ROUTE_PATH / UA_HEALTH_PATH set each
	// endpoint's sub-path relative to that base; unset UA_BASE_PATH keeps the
	// legacy root behavior.
	basePath := envOr("UA_BASE_PATH", "")
	routePath := joinPath(basePath, envOr("UA_ROUTE_PATH", "/"))
	healthPath := joinPath(basePath, envOr("UA_HEALTH_PATH", "/health"))

	disableUpdateStr := os.Getenv("UA_DISABLE_UPDATE")
	disableUpdate, _ := strconv.ParseBool(disableUpdateStr)

	cacheSize := 1000
	if cs := os.Getenv("UA_CACHE_SIZE"); cs != "" {
		if val, err := strconv.Atoi(cs); err == nil {
			cacheSize = val
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	disableCorrections, _ := strconv.ParseBool(os.Getenv("UA_DISABLE_CORRECTIONS_UPDATE"))

	cfg := core.Config{
		Ctx:                      ctx,
		DisableAutoUpdate:        disableUpdate,
		LRUCacheSize:             cacheSize,
		UpdateURL:                os.Getenv("UA_UPDATE_URL"),
		UpdateInterval:           os.Getenv("UA_UPDATE_INTERVAL"),
		CorrectionsURL:           os.Getenv("UA_CORRECTIONS_URL"),
		DisableCorrectionsUpdate: disableCorrections,
	}

	parser, err := core.New(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize parser: %v", err)
	}

	healthHandler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		version, rules := parser.CorrectionsInfo()
		w.Header().Set("Content-Type", "application/json")
		// A struct (not a map) so key order is stable and matches the docs:
		// {"status":"ok","corrections":{"version":...,"rules":...}}.
		resp := struct {
			Status      string `json:"status"`
			Corrections struct {
				Version string `json:"version"`
				Rules   int    `json:"rules"`
			} `json:"corrections"`
		}{Status: "ok"}
		resp.Corrections.Version = version
		resp.Corrections.Rules = rules
		json.NewEncoder(w).Encode(resp)
	}

	parseHandler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB limit

		var req ParseRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		result := parser.ParseFull(req.UA, req.Headers, req.Signals)

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(result); err != nil {
			log.Printf("Error encoding response: %v", err)
		}
	}

	mux := http.NewServeMux()
	if healthPath == routePath {
		// Both endpoints were configured onto the same path. Register a single
		// handler that dispatches by method so ServeMux never panics on a
		// duplicate-pattern registration.
		mux.HandleFunc(routePath, func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				healthHandler(w, r)
			case http.MethodPost:
				parseHandler(w, r)
			default:
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}
		})
		log.Printf("Health and parse endpoints share path %q (GET=health, POST=parse)", routePath)
	} else {
		mux.HandleFunc(healthPath, healthHandler)
		mux.HandleFunc(routePath, parseHandler)
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// done is closed once shutdown has fully completed (in-flight requests
	// drained and the parser closed), so main can wait for it before exiting.
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-ctx.Done()
		log.Println("Shutting down server...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("Server shutdown error: %v", err)
		}
		parser.Close()
	}()

	log.Printf("Starting server on port %s (parse=%s, health=%s, DisableUpdate: %v)", port, routePath, healthPath, disableUpdate)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server failed: %v", err)
	}
	// ListenAndServe returns as soon as Shutdown is initiated; wait until the
	// shutdown goroutine finishes draining requests and closing the parser.
	<-done
}
