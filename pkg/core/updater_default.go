//go:build !wasm

package core

import (
	"encoding/json"
	"io"
	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/ua-parser/uap-go/uaparser"
	"gopkg.in/yaml.v3"
)

// maxRegexesSize caps the update download; the upstream regexes.yaml is well
// under 1 MB, so 16 MB is generous while preventing an OOM from a
// misconfigured or compromised endpoint.
const maxRegexesSize = 16 << 20

func (p *Parser) startUpdater() {
	interval := 24 * time.Hour
	if p.config.UpdateInterval != "" {
		if d, err := time.ParseDuration(p.config.UpdateInterval); err != nil {
			log.Printf("Invalid update_interval %q (%v); using default 24h", p.config.UpdateInterval, err)
		} else if d <= 0 {
			// time.NewTicker panics on non-positive intervals.
			log.Printf("Non-positive update_interval %q; using default 24h", p.config.UpdateInterval)
		} else {
			interval = d
		}
	}

	backoff := time.Duration(0)
	maxBackoff := 30 * time.Minute

	for {
		if backoff > 0 {
			select {
			case <-p.ctx.Done():
				return
			case <-time.After(backoff):
			}
		}

		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("Recovered from updater panic: %v", r)
					if backoff == 0 {
						backoff = time.Minute
					} else {
						backoff *= 2
						if backoff > maxBackoff {
							backoff = maxBackoff
						}
					}
				}
			}()

			ticker := time.NewTicker(jitter(interval))
			defer ticker.Stop()

			for {
				select {
				case <-p.ctx.Done():
					return
				case <-ticker.C:
					p.updateRegexes()
					backoff = 0
					// Re-jitter so fleets started together do not converge
					// into a synchronized stampede on the update URL.
					ticker.Reset(jitter(interval))
				}
			}
		}()

		// Check if we should stop after panic recovery
		select {
		case <-p.ctx.Done():
			return
		default:
		}
	}
}

// jitter spreads an interval by ±10% to avoid synchronized fleet-wide fetches.
func jitter(d time.Duration) time.Duration {
	spread := int64(d) / 10
	if spread <= 0 {
		return d
	}
	return d - time.Duration(spread/2) + time.Duration(rand.Int63n(spread))
}

func (p *Parser) updateRegexes() {
	url := p.config.UpdateURL
	if url == "" {
		url = "https://raw.githubusercontent.com/ua-parser/uap-core/master/regexes.yaml"
	}

	log.Printf("Checking for regex updates from %s", url)

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Bind the request to the parser context so Close() cancels an in-flight
	// download instead of blocking shutdown for up to the client timeout.
	req, err := http.NewRequestWithContext(p.ctx, http.MethodGet, url, nil)
	if err != nil {
		log.Printf("Failed to build regexes request: %v", err)
		return
	}
	if p.lastETag != "" {
		req.Header.Set("If-None-Match", p.lastETag)
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Failed to download regexes: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		log.Println("Regexes unchanged (304)")
		return
	}
	if resp.StatusCode != http.StatusOK {
		log.Printf("Failed to download regexes: status code %d", resp.StatusCode)
		return
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxRegexesSize+1))
	if err != nil {
		log.Printf("Failed to read regexes response: %v", err)
		return
	}
	if len(data) > maxRegexesSize {
		log.Printf("Regexes response exceeds %d bytes; refusing to load", maxRegexesSize)
		return
	}

	// Validate the new regexes
	def := uaparser.RegexDefinitions{}
	if err := yaml.Unmarshal(data, &def); err != nil {
		log.Printf("Failed to parse new regexes (YAML): %v", err)
		// Try JSON as fallback
		if errJSON := json.Unmarshal(data, &def); errJSON != nil {
			log.Printf("Failed to parse new regexes (JSON): %v", errJSON)
			return
		}
	}

	newUap, err := uaparser.New(uaparser.WithRegexDefinitions(def))
	if err != nil {
		log.Printf("Failed to create new parser: %v", err)
		return
	}

	p.mu.Lock()
	p.uap = newUap
	p.mu.Unlock()

	// Bump the generation BEFORE purging: any Parse that started against the
	// old database will see a changed generation and skip caching its result,
	// so a stale entry cannot be re-added after the purge.
	p.gen.Add(1)

	// Clear cache when regexes change
	if p.cache != nil {
		p.cache.Purge()
	}

	p.lastETag = resp.Header.Get("ETag")

	log.Println("Regexes updated successfully")
}
