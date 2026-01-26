//go:build !js || !wasm

package core

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/ua-parser/uap-go/uaparser"
	"gopkg.in/yaml.v3"
)

func (p *Parser) startUpdater() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Recovered from updater panic: %v", r)
			// Restart updater after some time if it crashed
			time.Sleep(time.Minute)
			go p.startUpdater()
		}
	}()

	interval := 24 * time.Hour
	if p.config.UpdateInterval != "" {
		if d, err := time.ParseDuration(p.config.UpdateInterval); err == nil {
			interval = d
		}
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.updateRegexes()
		}
	}
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

	resp, err := client.Get(url)
	if err != nil {
		log.Printf("Failed to download regexes: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Failed to download regexes: status code %d", resp.StatusCode)
		return
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Failed to read regexes response: %v", err)
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

	// Optionally clear cache when regexes change
	if p.cache != nil {
		p.cache.Purge()
	}

	log.Println("Regexes updated successfully")
}
