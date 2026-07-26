//go:build !wasm

package core

import (
	"encoding/json"
	"fmt"
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

// defaultCorrectionsURL is this repository's own hot-update channel for the
// correction layer (docs/correction-layer.md).
const defaultCorrectionsURL = "https://raw.githubusercontent.com/Octanium91/ua-parser/main/pkg/core/resources/corrections.yaml"

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

	// Initial one-shot correction fetch so native deployments get fresh rules
	// at startup (parity with the WASM host-push clients), instead of waiting
	// a full interval. Regexes keep their embedded-until-first-tick behavior
	// (the regex download is large; corrections are a few KB).
	if !p.config.DisableCorrectionsUpdate {
		p.updateCorrections()
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
					if !p.config.DisableCorrectionsUpdate {
						p.updateCorrections()
					}
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

// fetchResource downloads one updatable resource with ETag revalidation and a
// size cap. notModified is true on a 304; err covers transport, status, and
// size failures. Bound to the parser context so Close() cancels in-flight
// downloads instead of blocking shutdown for up to the client timeout.
func (p *Parser) fetchResource(url, lastETag string, maxSize int64) (data []byte, etag string, notModified bool, err error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
		// Both resources are single static files; a redirect is never
		// expected and following one to an arbitrary host is an SSRF pivot
		// (e.g. a 3xx to a cloud metadata endpoint). Refuse and surface it.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return fmt.Errorf("refusing redirect to %s (set the final URL directly)", req.URL.Host)
		},
	}

	req, err := http.NewRequestWithContext(p.ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", false, err
	}
	if lastETag != "" {
		req.Header.Set("If-None-Match", lastETag)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return nil, lastETag, true, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", false, fmt.Errorf("status code %d", resp.StatusCode)
	}

	data, err = io.ReadAll(io.LimitReader(resp.Body, maxSize+1))
	if err != nil {
		return nil, "", false, err
	}
	if int64(len(data)) > maxSize {
		return nil, "", false, fmt.Errorf("response exceeds %d bytes", maxSize)
	}
	return data, resp.Header.Get("ETag"), false, nil
}

func (p *Parser) updateRegexes() {
	url := p.config.UpdateURL
	if url == "" {
		url = "https://raw.githubusercontent.com/ua-parser/uap-core/master/regexes.yaml"
	}

	log.Printf("Checking for regex updates from %s", url)

	data, etag, notModified, err := p.fetchResource(url, p.lastETag, maxRegexesSize)
	if err != nil {
		log.Printf("Failed to download regexes: %v", err)
		return
	}
	if notModified {
		log.Println("Regexes unchanged (304)")
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

	p.lastETag = etag

	log.Println("Regexes updated successfully")
}

// updateCorrections fetches corrections.yaml from this repository (or the
// configured override) and hot-swaps the correction rule set. Validation,
// inline self-tests, keep-last-good semantics, gen bump, and cache purge all
// live in ApplyCorrectionsYAML.
func (p *Parser) updateCorrections() {
	url := p.config.CorrectionsURL
	if url == "" {
		url = defaultCorrectionsURL
	}

	log.Printf("Checking for correction updates from %s", url)

	data, etag, notModified, err := p.fetchResource(url, p.lastCorrectionsETag, maxCorrectionsBytes)
	if err != nil {
		log.Printf("Failed to download corrections: %v", err)
		return
	}
	if notModified {
		log.Println("Corrections unchanged (304)")
		return
	}

	if err := p.ApplyCorrectionsYAML(data); err != nil {
		log.Printf("Rejected downloaded corrections (keeping last good): %v", err)
		return
	}

	p.lastCorrectionsETag = etag
}
