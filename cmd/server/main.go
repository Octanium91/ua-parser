package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/Octanium91/ua-parser/pkg/core"
)

type ParseRequest struct {
	UA      string            `json:"ua"`
	Headers map[string]string `json:"headers"`
}

func main() {
	port := os.Getenv("UA_PORT")
	if port == "" {
		port = "8080"
	}

	routePath := os.Getenv("UA_ROUTE_PATH")
	if routePath == "" {
		routePath = "/"
	}

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

	cfg := core.Config{
		Ctx:               ctx,
		DisableAutoUpdate: disableUpdate,
		LRUCacheSize:      cacheSize,
		UpdateURL:         os.Getenv("UA_UPDATE_URL"),
		UpdateInterval:    os.Getenv("UA_UPDATE_INTERVAL"),
	}

	parser, err := core.New(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize parser: %v", err)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	mux.HandleFunc(routePath, func(w http.ResponseWriter, r *http.Request) {
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

		result := parser.Parse(req.UA, req.Headers)

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(result); err != nil {
			log.Printf("Error encoding response: %v", err)
		}
	})

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		log.Println("Shutting down server...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("Server shutdown error: %v", err)
		}
		parser.Close()
	}()

	log.Printf("Starting server on port %s, path %s (DisableUpdate: %v)", port, routePath, disableUpdate)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server failed: %v", err)
	}
}
